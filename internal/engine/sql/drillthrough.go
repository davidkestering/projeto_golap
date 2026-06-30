package sql

import (
	"fmt"
	"strconv"
	"strings"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
)

// BuildDrillthrough gera SQL que devolve as LINHAS DE FATO cruas por trás de um
// contexto (os filtros que descrevem uma célula): seleciona as colunas de nível
// dos filtros (contexto) + as colunas de medida não agregadas da fato, com
// LIMIT. É o "drill-through" clássico.
func BuildDrillthrough(d Dialect, cube *metadata.Cube, filters []query.Filter, maxrows int) (*Statement, error) {
	if maxrows <= 0 {
		maxrows = 100
	}
	if maxrows > 10000 {
		maxrows = 10000
	}

	joins := newJoinSet(d, cube)
	st := &Statement{}
	var selectExprs []string
	var where []string
	seenLevel := map[string]bool{}

	// Colunas de contexto (níveis dos filtros) + predicados WHERE.
	for _, f := range filters {
		dim, hier, lvl, err := resolveLevel(cube, query.LevelRef{Dimension: f.Dimension, Hierarchy: f.Hierarchy, Level: f.Level})
		if err != nil {
			return nil, err
		}
		expr, err := joins.levelExpr(dim, hier, lvl)
		if err != nil {
			return nil, err
		}
		key := dim.Name + "." + lvl.Name
		if !seenLevel[key] {
			seenLevel[key] = true
			selectExprs = append(selectExprs, expr)
			st.Columns = append(st.Columns, query.Column{Name: lvl.Name, UniqueName: lvl.UniqueName(dim, hier), Kind: "level"})
		}
		st.Args = append(st.Args, f.Members)
		where = append(where, fmt.Sprintf("(%s)::text = ANY($%d)", expr, len(st.Args)))
	}

	// Colunas de medida cruas (valores da própria linha de fato). Cast para
	// float8 para serializar como número JSON simples (colunas DECIMAL/Numeric).
	for _, m := range cube.Measures {
		if m.Column == "" {
			continue // medidas só com expressão são puladas no drill-through
		}
		selectExprs = append(selectExprs, d.QuoteIdent(factAlias)+"."+d.QuoteIdent(m.Column)+"::float8")
		st.Columns = append(st.Columns, query.Column{Name: m.Name, UniqueName: m.UniqueName(), Kind: "measure", FormatString: m.FormatString})
	}
	if len(selectExprs) == 0 {
		return nil, fmt.Errorf("drill-through: nada para selecionar")
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(strings.Join(selectExprs, ", "))
	b.WriteString("\nFROM ")
	b.WriteString(relationSQL(d, cube.Fact))
	b.WriteString(" AS ")
	b.WriteString(d.QuoteIdent(factAlias))
	for _, j := range joins.ordered() {
		b.WriteString("\n")
		b.WriteString(j)
	}
	if len(where) > 0 {
		b.WriteString("\nWHERE ")
		b.WriteString(strings.Join(where, " AND "))
	}
	b.WriteString("\nLIMIT ")
	b.WriteString(strconv.Itoa(maxrows))

	st.SQL = b.String()
	return st, nil
}
