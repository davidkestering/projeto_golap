// Package sql gera SQL relacional a partir de uma query sobre um cubo (star
// schema). O comportamento específico de cada banco fica num Dialect.
package sql

import (
	"fmt"
	"strconv"
	"strings"
)

// Dialect abstrai as diferenças de SQL entre bancos (quoting, placeholders,
// casts, predicado IN e limite de linhas).
type Dialect interface {
	Name() string
	// QuoteIdent quota um identificador (tabela/coluna/alias).
	QuoteIdent(name string) string
	// Placeholder devolve o marcador do i-ésimo parâmetro (1-based).
	Placeholder(i int) string
	// CastText / CastFloat envolvem uma expressão com cast para texto / ponto flutuante.
	CastText(expr string) string
	CastFloat(expr string) string
	// InClause monta o predicado de pertinência (comparando como texto) e os
	// argumentos correspondentes, a partir de startArg (nº de args já usados).
	InClause(colExpr string, members []string, startArg int) (sql string, args []any)
	// SelectTop devolve o prefixo após SELECT (ex.: "TOP n " no SQL Server), ou "".
	SelectTop(n int) string
	// LimitSuffix devolve a cláusula de limite no fim (ex.: "LIMIT n"), ou "".
	LimitSuffix(n int) string
}

// quoteWith quota um identificador com o par de delimitadores dado, escapando o
// delimitador de fechamento.
func quoteWith(name, open, close string) string {
	return open + strings.ReplaceAll(name, close, close+close) + close
}

func membersAsArgs(members []string) []any {
	args := make([]any, len(members))
	for i, m := range members {
		args[i] = m
	}
	return args
}

// inWithPlaceholders monta "<castText> IN (ph, ph, ...)" usando placeholders
// individuais (um por membro) — usado por MySQL/DuckDB/SQL Server.
func inWithPlaceholders(d Dialect, colExpr string, members []string, startArg int) (string, []any) {
	if len(members) == 0 {
		return "1=0", nil // conjunto vazio: nada casa
	}
	phs := make([]string, len(members))
	for i := range members {
		phs[i] = d.Placeholder(startArg + 1 + i)
	}
	return d.CastText(colExpr) + " IN (" + strings.Join(phs, ", ") + ")", membersAsArgs(members)
}

// ---- PostgreSQL -----------------------------------------------------------

// Postgres implementa o dialeto PostgreSQL (também base do DuckDB).
type Postgres struct{}

func (Postgres) Name() string                  { return "postgres" }
func (Postgres) QuoteIdent(name string) string { return quoteWith(name, `"`, `"`) }
func (Postgres) Placeholder(i int) string      { return "$" + strconv.Itoa(i) }
func (Postgres) CastText(expr string) string   { return "(" + expr + ")::text" }
func (Postgres) CastFloat(expr string) string  { return expr + "::float8" }
func (Postgres) SelectTop(int) string          { return "" }
func (Postgres) LimitSuffix(n int) string      { return "LIMIT " + strconv.Itoa(n) }

// InClause no Postgres usa um único parâmetro de array: col::text = ANY($n).
func (p Postgres) InClause(colExpr string, members []string, startArg int) (string, []any) {
	return p.CastText(colExpr) + " = ANY(" + p.Placeholder(startArg+1) + ")", []any{members}
}

// ---- MySQL / MariaDB ------------------------------------------------------

// MySQL implementa o dialeto MySQL/MariaDB (crases, placeholders ?, IN).
type MySQL struct{}

func (MySQL) Name() string                  { return "mysql" }
func (MySQL) QuoteIdent(name string) string { return quoteWith(name, "`", "`") }
func (MySQL) Placeholder(int) string        { return "?" }
func (MySQL) CastText(expr string) string   { return "CAST(" + expr + " AS CHAR)" }
func (MySQL) CastFloat(expr string) string  { return "CAST(" + expr + " AS DOUBLE)" }
func (MySQL) SelectTop(int) string          { return "" }
func (MySQL) LimitSuffix(n int) string      { return "LIMIT " + strconv.Itoa(n) }
func (m MySQL) InClause(colExpr string, members []string, startArg int) (string, []any) {
	return inWithPlaceholders(m, colExpr, members, startArg)
}

// ---- DuckDB ---------------------------------------------------------------

// DuckDB implementa o dialeto DuckDB (aspas duplas como Postgres, placeholders ?,
// casts AS DOUBLE/VARCHAR, IN por placeholders).
type DuckDB struct{}

func (DuckDB) Name() string                  { return "duckdb" }
func (DuckDB) QuoteIdent(name string) string { return quoteWith(name, `"`, `"`) }
func (DuckDB) Placeholder(int) string        { return "?" }
func (DuckDB) CastText(expr string) string   { return "CAST(" + expr + " AS VARCHAR)" }
func (DuckDB) CastFloat(expr string) string  { return "CAST(" + expr + " AS DOUBLE)" }
func (DuckDB) SelectTop(int) string          { return "" }
func (DuckDB) LimitSuffix(n int) string      { return "LIMIT " + strconv.Itoa(n) }
func (d DuckDB) InClause(colExpr string, members []string, startArg int) (string, []any) {
	return inWithPlaceholders(d, colExpr, members, startArg)
}

// ---- SQL Server / T-SQL ---------------------------------------------------

// SQLServer implementa o dialeto Microsoft SQL Server (colchetes, @pN, TOP,
// CAST AS FLOAT/NVARCHAR).
type SQLServer struct{}

func (SQLServer) Name() string                  { return "sqlserver" }
func (SQLServer) QuoteIdent(name string) string { return quoteWith(name, "[", "]") }
func (SQLServer) Placeholder(i int) string      { return "@p" + strconv.Itoa(i) }
func (SQLServer) CastText(expr string) string   { return "CAST(" + expr + " AS NVARCHAR(4000))" }
func (SQLServer) CastFloat(expr string) string  { return "CAST(" + expr + " AS FLOAT)" }
func (SQLServer) SelectTop(n int) string        { return "TOP " + strconv.Itoa(n) + " " }
func (SQLServer) LimitSuffix(int) string        { return "" }
func (s SQLServer) InClause(colExpr string, members []string, startArg int) (string, []any) {
	return inWithPlaceholders(s, colExpr, members, startArg)
}

// DialectByName devolve um dialeto pelo nome (default: Postgres).
func DialectByName(name string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "postgres", "postgresql", "pg":
		return Postgres{}, nil
	case "mysql", "mariadb":
		return MySQL{}, nil
	case "duckdb":
		return DuckDB{}, nil
	case "sqlserver", "mssql", "tsql":
		return SQLServer{}, nil
	default:
		return nil, fmt.Errorf("dialeto desconhecido: %q", name)
	}
}
