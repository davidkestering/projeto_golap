// Package sql gera SQL relacional a partir de uma query sobre um cubo (star
// schema). Por ora há um único dialeto (PostgreSQL); outros entram conforme a
// necessidade.
package sql

import "strings"

// Dialect abstrai as diferenças de SQL entre bancos.
type Dialect interface {
	// QuoteIdent quota um identificador simples (tabela/coluna/alias).
	QuoteIdent(name string) string
	// Name é o nome do dialeto.
	Name() string
}

// Postgres implementa o dialeto PostgreSQL.
type Postgres struct{}

func (Postgres) Name() string { return "postgres" }

func (Postgres) QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
