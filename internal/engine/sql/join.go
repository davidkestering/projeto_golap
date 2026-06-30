package sql

import (
	"fmt"

	"cubodw/internal/engine/metadata"
)

// joinSet acumula os JOINs necessários (dedup por dimensão) e resolve a
// expressão SQL da coluna de um nível.
type joinSet struct {
	d     Dialect
	cube  *metadata.Cube
	seen  map[string]bool
	order []string
}

func newJoinSet(d Dialect, cube *metadata.Cube) *joinSet {
	return &joinSet{d: d, cube: cube, seen: map[string]bool{}}
}

// levelExpr devolve a expressão SQL (alias.coluna) do nível, registrando o JOIN
// da dimensão quando necessário.
func (js *joinSet) levelExpr(dim *metadata.Dimension, hier *metadata.Hierarchy, lvl *metadata.Level) (string, error) {
	if lvl.Column == "" {
		return "", fmt.Errorf("nível %q sem coluna", lvl.Name)
	}
	tableName := hier.Table.Table

	// Snowflake dentro da hierarquia (nível em tabela diferente) — não suportado.
	if lvl.Table != "" && lvl.Table != tableName {
		return "", fmt.Errorf("nível %q em tabela %q (snowflake) ainda não suportado", lvl.Name, lvl.Table)
	}

	// Sem <Table>: dimensão degenerada (coluna na fato) só se não houver FK;
	// caso contrário é uma hierarquia com <Join> (snowflake), ainda não suportada.
	if tableName == "" {
		if dim.ForeignKey != "" {
			return "", fmt.Errorf("dimensão %q usa Join/snowflake, ainda não suportado na geração de SQL", dim.Name)
		}
		return js.d.QuoteIdent(factAlias) + "." + js.d.QuoteIdent(lvl.DisplayColumn()), nil
	}

	// Dimensão degenerada cuja tabela é a própria fato: sem JOIN.
	if tableName == js.cube.Fact.Table {
		return js.d.QuoteIdent(factAlias) + "." + js.d.QuoteIdent(lvl.DisplayColumn()), nil
	}

	// Dimensão normal: JOIN fato.FK = dim.PK.
	if dim.ForeignKey == "" || hier.PrimaryKey == "" {
		return "", fmt.Errorf("dimensão %q sem foreignKey/primaryKey para JOIN", dim.Name)
	}
	alias := dim.Name
	if !js.seen[alias] {
		js.seen[alias] = true
		js.order = append(js.order, fmt.Sprintf(
			"JOIN %s AS %s ON %s.%s = %s.%s",
			relationSQL(js.d, hier.Table),
			js.d.QuoteIdent(alias),
			js.d.QuoteIdent(factAlias), js.d.QuoteIdent(dim.ForeignKey),
			js.d.QuoteIdent(alias), js.d.QuoteIdent(hier.PrimaryKey),
		))
	}
	return js.d.QuoteIdent(alias) + "." + js.d.QuoteIdent(lvl.DisplayColumn()), nil
}

func (js *joinSet) ordered() []string { return js.order }
