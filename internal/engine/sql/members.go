package sql

import (
	"fmt"
	"strings"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
)

// BuildLevelMembers gera SELECT DISTINCT sobre a TABELA DE DIMENSÃO para
// enumerar todos os membros de um nível (independente de existirem fatos), com
// filtros de ancestrais/restrição aplicados sobre colunas da mesma tabela.
//
// É a base da semântica "mostrar todos os membros" (vs. NON EMPTY, que enumera
// pelo fato). Não suporta níveis em snowflake (tabela do nível != tabela da
// hierarquia).
func BuildLevelMembers(d Dialect, cube *metadata.Cube, ref query.LevelRef, filters []query.Filter) (*Statement, error) {
	dim, hier, lvl, err := resolveLevel(cube, ref)
	if err != nil {
		return nil, err
	}
	if hier.Table.Table == "" {
		return nil, fmt.Errorf("dimensão %q sem tabela (snowflake?) — enumeração via dimensão não suportada", dim.Name)
	}
	if lvl.Table != "" && lvl.Table != hier.Table.Table {
		return nil, fmt.Errorf("nível %q em tabela %q (snowflake) — enumeração via dimensão não suportada", lvl.Name, lvl.Table)
	}
	alias := dim.Name
	colExpr := d.QuoteIdent(alias) + "." + d.QuoteIdent(lvl.Column)

	st := &Statement{
		Columns: []query.Column{{Name: lvl.Name, UniqueName: lvl.UniqueName(dim, hier), Kind: "level"}},
	}

	var where []string
	for _, f := range filters {
		_, fh, fl, err := resolveLevel(cube, query.LevelRef{Dimension: f.Dimension, Hierarchy: f.Hierarchy, Level: f.Level})
		if err != nil {
			return nil, err
		}
		// Só filtros cujas colunas estão na MESMA tabela da dimensão.
		if fh.Table.Table != hier.Table.Table {
			continue
		}
		st.Args = append(st.Args, f.Members)
		where = append(where, fmt.Sprintf("(%s)::text = ANY($%d)",
			d.QuoteIdent(alias)+"."+d.QuoteIdent(fl.Column), len(st.Args)))
	}

	var b strings.Builder
	b.WriteString("SELECT DISTINCT ")
	b.WriteString(colExpr)
	b.WriteString("\nFROM ")
	b.WriteString(relationSQL(d, hier.Table))
	b.WriteString(" AS ")
	b.WriteString(d.QuoteIdent(alias))
	if len(where) > 0 {
		b.WriteString("\nWHERE ")
		b.WriteString(strings.Join(where, " AND "))
	}
	b.WriteString("\nORDER BY 1")

	st.SQL = b.String()
	return st, nil
}
