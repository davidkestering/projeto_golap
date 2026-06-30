// Package queryexec planeja (gera SQL) e executa queries de cubo contra o
// Postgres, devolvendo um Result tipado (records).
package queryexec

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	enginesql "cubodw/internal/engine/sql"
)

// Service executa queries usando um pool de Postgres e um dialeto.
type Service struct {
	pool    *pgxpool.Pool
	dialect enginesql.Dialect
	cache   *resultCache
}

// New cria o serviço. pool pode ser nil (apenas planejamento/preview funciona).
// cacheSize > 0 habilita o cache de resultados (FIFO, em memória).
func New(pool *pgxpool.Pool, cacheSize int) *Service {
	return &Service{pool: pool, dialect: enginesql.Postgres{}, cache: newResultCache(cacheSize)}
}

// CacheStats devolve hits, misses e tamanho do cache de resultados.
func (s *Service) CacheStats() (hits, misses, size int64) {
	if s.cache == nil {
		return 0, 0, 0
	}
	return s.cache.stats()
}

// CacheEnabled indica se o cache está ativo.
func (s *Service) CacheEnabled() bool { return s.cache != nil }

// CacheClear esvazia o cache de resultados.
func (s *Service) CacheClear() {
	if s.cache != nil {
		s.cache.clear()
	}
}

// Plan gera a SQL para a query (validação de nomes inclusa). Não toca no banco.
func (s *Service) Plan(cube *metadata.Cube, q query.Query) (*enginesql.Statement, error) {
	return enginesql.Build(s.dialect, cube, q)
}

// HasDB indica se há conexão de banco configurada.
func (s *Service) HasDB() bool { return s.pool != nil }

// EnumerateLevel devolve todos os membros de um nível a partir da tabela de
// dimensão (não do fato), aplicando os filtros de ancestrais/restrição.
func (s *Service) EnumerateLevel(ctx context.Context, cube *metadata.Cube, ref query.LevelRef, filters []query.Filter) ([]string, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("sem conexão de banco")
	}
	st, err := enginesql.BuildLevelMembers(s.dialect, cube, ref, filters)
	if err != nil {
		return nil, err
	}
	res, err := s.Run(ctx, cube, st)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Rows))
	for _, row := range res.Rows {
		out = append(out, fmt.Sprint(row[0].Value))
	}
	return out, nil
}

// Drillthrough devolve as linhas de fato cruas por trás de um contexto (filtros),
// com limite de linhas.
func (s *Service) Drillthrough(ctx context.Context, cube *metadata.Cube, filters []query.Filter, maxrows int) (*query.Result, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("sem conexão de banco")
	}
	st, err := enginesql.BuildDrillthrough(s.dialect, cube, filters, maxrows)
	if err != nil {
		return nil, err
	}
	return s.Run(ctx, cube, st)
}

// Run executa a SQL planejada e materializa o Result.
func (s *Service) Run(ctx context.Context, cube *metadata.Cube, st *enginesql.Statement) (*query.Result, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("sem conexão de banco")
	}

	key := st.SQL + "|" + fmt.Sprintf("%v", st.Args)
	if s.cache != nil {
		if cached, ok := s.cache.get(key); ok {
			return cloneResult(cached), nil
		}
	}

	rows, err := s.pool.Query(ctx, st.SQL, st.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &query.Result{Cube: cube.Name, SQL: st.SQL, Columns: st.Columns}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		cells := make([]query.Cell, len(vals))
		for i, v := range vals {
			formatted := formatValue(v)
			if i < len(st.Columns) {
				if c := st.Columns[i]; c.Kind == "measure" && c.FormatString != "" {
					if fv, ok := asFloat(v); ok {
						formatted = query.Format(fv, c.FormatString)
					}
				}
			}
			cells[i] = query.Cell{Value: v, Formatted: formatted}
		}
		res.Rows = append(res.Rows, cells)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if res.Rows == nil {
		res.Rows = [][]query.Cell{}
	}
	if s.cache != nil {
		s.cache.put(key, cloneResult(res))
	}
	return res, nil
}
