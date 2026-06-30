package sql

import (
	"fmt"
	"strconv"
	"strings"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
)

// factAlias é o alias da tabela fato na SQL gerada.
const factAlias = "f"

// Statement é a SQL gerada com seus argumentos e o plano de colunas (ordem dos
// níveis de eixo seguidos das medidas).
type Statement struct {
	SQL     string
	Args    []any
	Columns []query.Column
}

// Build gera o SELECT/GROUP BY para a query sobre o cubo, no dialeto dado.
func Build(d Dialect, cube *metadata.Cube, q query.Query) (*Statement, error) {
	axis := q.AxisLevels()
	if len(axis) == 0 && len(q.Measures) == 0 {
		return nil, fmt.Errorf("query vazia: informe ao menos uma medida ou nível")
	}

	st := &Statement{}
	var (
		selectExprs []string
		groupExprs  []string
		joins       = newJoinSet(d, cube)
	)

	// Níveis de eixo (linhas + colunas) → colunas de agrupamento.
	for _, ref := range axis {
		dim, hier, lvl, err := resolveLevel(cube, ref)
		if err != nil {
			return nil, err
		}
		expr, err := joins.levelExpr(dim, hier, lvl)
		if err != nil {
			return nil, err
		}
		selectExprs = append(selectExprs, expr)
		groupExprs = append(groupExprs, expr)
		st.Columns = append(st.Columns, query.Column{
			Name:       lvl.Name,
			UniqueName: lvl.UniqueName(dim, hier),
			Kind:       "level",
		})
	}

	// Medidas → expressões agregadas.
	for _, mName := range q.Measures {
		m, ok := cube.FindMeasure(mName)
		if !ok {
			return nil, fmt.Errorf("medida %q não existe no cubo %q", mName, cube.Name)
		}
		expr, err := measureExpr(d, m)
		if err != nil {
			return nil, err
		}
		selectExprs = append(selectExprs, expr)
		st.Columns = append(st.Columns, query.Column{
			Name:         m.Name,
			UniqueName:   m.UniqueName(),
			Kind:         "measure",
			FormatString: m.FormatString,
		})
	}

	// Filtros → predicados WHERE (comparados como texto).
	var where []string
	for _, f := range q.Filters {
		dim, hier, lvl, err := resolveLevel(cube, query.LevelRef{Dimension: f.Dimension, Hierarchy: f.Hierarchy, Level: f.Level})
		if err != nil {
			return nil, err
		}
		expr, err := joins.levelExpr(dim, hier, lvl)
		if err != nil {
			return nil, err
		}
		st.Args = append(st.Args, f.Members)
		where = append(where, fmt.Sprintf("(%s)::text = ANY($%d)", expr, len(st.Args)))
	}

	// Montagem da SQL.
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
	if len(groupExprs) > 0 {
		b.WriteString("\nGROUP BY ")
		b.WriteString(strings.Join(groupExprs, ", "))
		b.WriteString("\nORDER BY ")
		b.WriteString(orderByPositions(len(groupExprs)))
	}

	st.SQL = b.String()
	return st, nil
}

// resolveLevel localiza dimensão, hierarquia e nível no cubo.
func resolveLevel(cube *metadata.Cube, ref query.LevelRef) (*metadata.Dimension, *metadata.Hierarchy, *metadata.Level, error) {
	dim, ok := cube.FindDimension(ref.Dimension)
	if !ok {
		return nil, nil, nil, fmt.Errorf("dimensão %q não existe no cubo %q", ref.Dimension, cube.Name)
	}
	if len(dim.Hierarchies) == 0 {
		return nil, nil, nil, fmt.Errorf("dimensão %q sem hierarquias", ref.Dimension)
	}
	var hier *metadata.Hierarchy
	if ref.Hierarchy == "" {
		hier = dim.Hierarchies[0]
	} else {
		for _, h := range dim.Hierarchies {
			if h.EffectiveName(dim) == ref.Hierarchy {
				hier = h
				break
			}
		}
		if hier == nil {
			return nil, nil, nil, fmt.Errorf("hierarquia %q não existe na dimensão %q", ref.Hierarchy, ref.Dimension)
		}
	}
	for _, l := range hier.Levels {
		if l.Name == ref.Level {
			return dim, hier, l, nil
		}
	}
	return nil, nil, nil, fmt.Errorf("nível %q não existe na dimensão %q", ref.Level, ref.Dimension)
}

// measureExpr monta a expressão agregada de uma medida.
func measureExpr(d Dialect, m *metadata.Measure) (string, error) {
	var src string
	switch {
	case m.Expression != "":
		src = "(" + m.Expression + ")"
	case m.Column != "":
		src = d.QuoteIdent(factAlias) + "." + d.QuoteIdent(m.Column)
	default:
		return "", fmt.Errorf("medida %q sem column nem expression", m.Name)
	}
	// Counts retornam bigint (inteiro limpo); somas/médias/min/max numéricos são
	// convertidos para float8 para serializar como número JSON simples. A
	// exatidão decimal (dinheiro) será tratada numa fase posterior.
	switch normalizeAgg(m.Aggregator) {
	case "distinct-count":
		return "count(distinct " + src + ")", nil
	case "count":
		return "count(" + src + ")", nil
	case "min":
		return "min(" + src + ")::float8", nil
	case "max":
		return "max(" + src + ")::float8", nil
	case "avg":
		return "avg(" + src + ")::float8", nil
	default: // sum
		return "sum(" + src + ")::float8", nil
	}
}

func normalizeAgg(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "distinct-count", "distinct_count", "distinctcount":
		return "distinct-count"
	case "count":
		return "count"
	case "min":
		return "min"
	case "max":
		return "max"
	case "avg", "average":
		return "avg"
	default:
		return "sum"
	}
}

func relationSQL(d Dialect, r metadata.Relation) string {
	if r.Schema != "" {
		return d.QuoteIdent(r.Schema) + "." + d.QuoteIdent(r.Table)
	}
	return d.QuoteIdent(r.Table)
}

func orderByPositions(n int) string {
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = strconv.Itoa(i + 1)
	}
	return strings.Join(parts, ", ")
}
