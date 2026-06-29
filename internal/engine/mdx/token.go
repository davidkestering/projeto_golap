// Package mdx implementa o lexer, AST e parser da linguagem MDX, espelhando a
// gramática do Mondrian (src/main/javacc/mondrian/parser/MdxParser.jj). Nesta
// fase faz apenas o parsing (texto → AST); a avaliação vem na fase seguinte.
package mdx

// TokenType enumera os tipos de token do MDX.
type TokenType int

const (
	EOF TokenType = iota

	// Identificadores e literais.
	IDENT    // identificador não-quotado: CrossJoin, Members, ...
	QUOTEDID // [identificador quotado]
	KEYID    // &[chave] ou &chave
	NUMBER   // literal numérico
	STRING   // literal string

	// Pontuação e operadores.
	DOT
	COMMA
	COLON
	BANG
	LPAREN
	RPAREN
	LBRACE
	RBRACE
	PLUS
	MINUS
	STAR
	SLASH
	CONCAT // ||
	EQ
	NE // <>
	LT
	GT
	LE
	GE

	// Palavras-chave.
	SELECT
	FROM
	WHERE
	WITH
	MEMBER
	SET
	AS
	ON
	COLUMNS
	ROWS
	PAGES
	CHAPTERS
	SECTIONS
	AXIS
	NON
	EMPTY
	AND
	OR
	XOR
	NOT
	IS
	NULL
	CASE
	WHEN
	THEN
	ELSE
	END
	CAST
	IN
	MATCHES
	DIMENSION
	PROPERTIES
	CELL
)

// Token é um token léxico com seu texto já normalizado (sem colchetes/aspas,
// com escapes resolvidos) e a posição (offset em runes) onde começa.
type Token struct {
	Type TokenType
	Text string
	Pos  int
}

// keywords mapeia a forma maiúscula → tipo de palavra-chave reservada.
var keywords = map[string]TokenType{
	"SELECT": SELECT, "FROM": FROM, "WHERE": WHERE, "WITH": WITH,
	"MEMBER": MEMBER, "SET": SET, "AS": AS, "ON": ON,
	"COLUMNS": COLUMNS, "ROWS": ROWS, "PAGES": PAGES,
	"CHAPTERS": CHAPTERS, "SECTIONS": SECTIONS, "AXIS": AXIS,
	"NON": NON, "EMPTY": EMPTY, "AND": AND, "OR": OR, "XOR": XOR,
	"NOT": NOT, "IS": IS, "NULL": NULL, "CASE": CASE, "WHEN": WHEN,
	"THEN": THEN, "ELSE": ELSE, "END": END, "CAST": CAST, "IN": IN,
	"MATCHES": MATCHES, "DIMENSION": DIMENSION, "PROPERTIES": PROPERTIES,
	"CELL": CELL,
}

// tokenNames serve para mensagens de erro/depuração.
var tokenNames = map[TokenType]string{
	EOF: "EOF", IDENT: "IDENT", QUOTEDID: "QUOTEDID", KEYID: "KEYID",
	NUMBER: "NUMBER", STRING: "STRING", DOT: ".", COMMA: ",", COLON: ":",
	BANG: "!", LPAREN: "(", RPAREN: ")", LBRACE: "{", RBRACE: "}",
	PLUS: "+", MINUS: "-", STAR: "*", SLASH: "/", CONCAT: "||",
	EQ: "=", NE: "<>", LT: "<", GT: ">", LE: "<=", GE: ">=",
	SELECT: "SELECT", FROM: "FROM", WHERE: "WHERE", WITH: "WITH",
	MEMBER: "MEMBER", SET: "SET", AS: "AS", ON: "ON", COLUMNS: "COLUMNS",
	ROWS: "ROWS", PAGES: "PAGES", CHAPTERS: "CHAPTERS", SECTIONS: "SECTIONS",
	AXIS: "AXIS", NON: "NON", EMPTY: "EMPTY", AND: "AND", OR: "OR",
	XOR: "XOR", NOT: "NOT", IS: "IS", NULL: "NULL", CASE: "CASE",
	WHEN: "WHEN", THEN: "THEN", ELSE: "ELSE", END: "END", CAST: "CAST",
	IN: "IN", MATCHES: "MATCHES", DIMENSION: "DIMENSION",
	PROPERTIES: "PROPERTIES", CELL: "CELL",
}

func (t TokenType) String() string {
	if n, ok := tokenNames[t]; ok {
		return n
	}
	return "?"
}
