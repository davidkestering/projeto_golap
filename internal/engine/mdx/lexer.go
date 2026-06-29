package mdx

import (
	"fmt"
	"strings"
	"unicode"
)

// Lexer transforma texto MDX numa sequência de tokens.
type Lexer struct {
	src []rune
	pos int
}

// Tokenize devolve todos os tokens da entrada (terminando em EOF).
func Tokenize(input string) ([]Token, error) {
	l := &Lexer{src: []rune(input)}
	var toks []Token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.Type == EOF {
			return toks, nil
		}
	}
}

func (l *Lexer) peek() rune {
	if l.pos < len(l.src) {
		return l.src[l.pos]
	}
	return 0
}

func (l *Lexer) peekAt(n int) rune {
	if l.pos+n < len(l.src) {
		return l.src[l.pos+n]
	}
	return 0
}

func (l *Lexer) next() (Token, error) {
	l.skipTrivia()
	start := l.pos
	if l.pos >= len(l.src) {
		return Token{Type: EOF, Pos: start}, nil
	}
	c := l.peek()

	switch {
	case c == '[':
		return l.lexQuotedID(start)
	case c == '&':
		return l.lexKeyID(start)
	case c == '\'':
		return l.lexString(start, '\'')
	case c == '"':
		return l.lexString(start, '"')
	case isDigit(c):
		return l.lexNumber(start)
	case c == '.' && isDigit(l.peekAt(1)):
		return l.lexNumber(start)
	case isLetter(c):
		return l.lexIdentOrKeyword(start)
	}
	return l.lexOperator(start)
}

// skipTrivia consome espaços e comentários (//, --, /* */).
func (l *Lexer) skipTrivia() {
	for l.pos < len(l.src) {
		c := l.peek()
		switch {
		case unicode.IsSpace(c):
			l.pos++
		case c == '/' && l.peekAt(1) == '/':
			l.skipLine()
		case c == '-' && l.peekAt(1) == '-':
			l.skipLine()
		case c == '/' && l.peekAt(1) == '*':
			l.pos += 2
			for l.pos < len(l.src) && !(l.peek() == '*' && l.peekAt(1) == '/') {
				l.pos++
			}
			l.pos += 2 // consome */
		default:
			return
		}
	}
}

func (l *Lexer) skipLine() {
	for l.pos < len(l.src) && l.peek() != '\n' {
		l.pos++
	}
}

func (l *Lexer) lexQuotedID(start int) (Token, error) {
	l.pos++ // [
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.peek()
		if c == ']' {
			if l.peekAt(1) == ']' { // ]] = ] literal
				sb.WriteRune(']')
				l.pos += 2
				continue
			}
			l.pos++ // fecha
			return Token{Type: QUOTEDID, Text: sb.String(), Pos: start}, nil
		}
		if c == '\n' || c == '\r' {
			break
		}
		sb.WriteRune(c)
		l.pos++
	}
	return Token{}, fmt.Errorf("identificador [..] não fechado na posição %d", start)
}

func (l *Lexer) lexKeyID(start int) (Token, error) {
	l.pos++ // &
	if l.peek() == '[' {
		t, err := l.lexQuotedID(l.pos)
		if err != nil {
			return Token{}, err
		}
		return Token{Type: KEYID, Text: t.Text, Pos: start}, nil
	}
	if !isLetter(l.peek()) {
		return Token{}, fmt.Errorf("chave & malformada na posição %d", start)
	}
	s := l.pos
	for l.pos < len(l.src) && (isLetter(l.peek()) || isDigit(l.peek())) {
		l.pos++
	}
	return Token{Type: KEYID, Text: string(l.src[s:l.pos]), Pos: start}, nil
}

func (l *Lexer) lexString(start int, q rune) (Token, error) {
	l.pos++ // aspa inicial
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.peek()
		if c == q {
			if l.peekAt(1) == q { // aspa duplicada = literal
				sb.WriteRune(q)
				l.pos += 2
				continue
			}
			l.pos++
			return Token{Type: STRING, Text: sb.String(), Pos: start}, nil
		}
		sb.WriteRune(c)
		l.pos++
	}
	return Token{}, fmt.Errorf("string não terminada na posição %d", start)
}

func (l *Lexer) lexNumber(start int) (Token, error) {
	for isDigit(l.peek()) {
		l.pos++
	}
	// Parte decimal: só consome o '.' se seguido de dígito (preserva '5.Members').
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		l.pos++
		for isDigit(l.peek()) {
			l.pos++
		}
	} else if l.peek() == '.' && start == l.pos {
		// caso ".5": já garantido pelo chamador que peekAt(1) é dígito
		l.pos++
		for isDigit(l.peek()) {
			l.pos++
		}
	}
	// Expoente.
	if c := l.peek(); c == 'e' || c == 'E' {
		save := l.pos
		l.pos++
		if l.peek() == '+' || l.peek() == '-' {
			l.pos++
		}
		if isDigit(l.peek()) {
			for isDigit(l.peek()) {
				l.pos++
			}
		} else {
			l.pos = save // não era expoente
		}
	}
	return Token{Type: NUMBER, Text: string(l.src[start:l.pos]), Pos: start}, nil
}

func (l *Lexer) lexIdentOrKeyword(start int) (Token, error) {
	for l.pos < len(l.src) && (isLetter(l.peek()) || isDigit(l.peek())) {
		l.pos++
	}
	text := string(l.src[start:l.pos])
	if kw, ok := keywords[strings.ToUpper(text)]; ok {
		return Token{Type: kw, Text: text, Pos: start}, nil
	}
	return Token{Type: IDENT, Text: text, Pos: start}, nil
}

func (l *Lexer) lexOperator(start int) (Token, error) {
	c := l.peek()
	two := string([]rune{c, l.peekAt(1)})
	switch two {
	case "||":
		l.pos += 2
		return Token{Type: CONCAT, Text: two, Pos: start}, nil
	case "<=":
		l.pos += 2
		return Token{Type: LE, Text: two, Pos: start}, nil
	case ">=":
		l.pos += 2
		return Token{Type: GE, Text: two, Pos: start}, nil
	case "<>":
		l.pos += 2
		return Token{Type: NE, Text: two, Pos: start}, nil
	}
	single := map[rune]TokenType{
		'.': DOT, ',': COMMA, ':': COLON, '!': BANG,
		'(': LPAREN, ')': RPAREN, '{': LBRACE, '}': RBRACE,
		'+': PLUS, '-': MINUS, '*': STAR, '/': SLASH,
		'=': EQ, '<': LT, '>': GT,
	}
	if tt, ok := single[c]; ok {
		l.pos++
		return Token{Type: tt, Text: string(c), Pos: start}, nil
	}
	return Token{}, fmt.Errorf("caractere inesperado %q na posição %d", string(c), start)
}

func isDigit(c rune) bool  { return c >= '0' && c <= '9' }
func isLetter(c rune) bool { return c == '_' || c == '$' || unicode.IsLetter(c) }
