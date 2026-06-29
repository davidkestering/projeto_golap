package mdx

import (
	"fmt"
	"strconv"
)

// parser é um parser recursivo-descendente seguindo a precedência do Mondrian.
type parser struct {
	toks []Token
	pos  int
}

// ParseQuery faz o parse de uma instrução SELECT MDX completa.
func ParseQuery(input string) (*Query, error) {
	p, err := newParser(input)
	if err != nil {
		return nil, err
	}
	q, err := p.selectStatement()
	if err != nil {
		return nil, err
	}
	if p.cur().Type != EOF {
		return nil, p.errf("texto extra após a query")
	}
	return q, nil
}

// ParseExpression faz o parse de uma expressão MDX isolada (útil para testes e
// para fórmulas WITH).
func ParseExpression(input string) (Exp, error) {
	p, err := newParser(input)
	if err != nil {
		return nil, err
	}
	e, err := p.expression()
	if err != nil {
		return nil, err
	}
	if p.cur().Type != EOF {
		return nil, p.errf("texto extra após a expressão")
	}
	return e, nil
}

func newParser(input string) (*parser, error) {
	toks, err := Tokenize(input)
	if err != nil {
		return nil, err
	}
	return &parser{toks: toks}, nil
}

// --- utilitários ---------------------------------------------------------

func (p *parser) cur() Token  { return p.toks[p.pos] }
func (p *parser) peek() Token {
	if p.pos+1 < len(p.toks) {
		return p.toks[p.pos+1]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) at(tt TokenType) bool { return p.cur().Type == tt }

func (p *parser) accept(tt TokenType) bool {
	if p.at(tt) {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expect(tt TokenType) error {
	if !p.at(tt) {
		return p.errf("esperava %s, encontrei %s", tt, p.cur().Type)
	}
	p.pos++
	return nil
}

func (p *parser) errf(format string, args ...any) error {
	t := p.cur()
	return fmt.Errorf("MDX@%d (%q): %s", t.Pos, t.Text, fmt.Sprintf(format, args...))
}

// --- statement -----------------------------------------------------------

func (p *parser) selectStatement() (*Query, error) {
	q := &Query{}

	if p.accept(WITH) {
		for p.at(MEMBER) || p.at(SET) {
			f, err := p.formula()
			if err != nil {
				return nil, err
			}
			q.Formulas = append(q.Formulas, f)
		}
	}

	if err := p.expect(SELECT); err != nil {
		return nil, err
	}

	if !p.at(FROM) {
		for {
			ax, err := p.axisSpecification()
			if err != nil {
				return nil, err
			}
			q.Axes = append(q.Axes, ax)
			if !p.accept(COMMA) {
				break
			}
		}
	}

	if err := p.expect(FROM); err != nil {
		return nil, err
	}
	cube, err := p.compoundId()
	if err != nil {
		return nil, err
	}
	if len(cube.Segments) == 0 {
		return nil, p.errf("cubo ausente no FROM")
	}
	q.Cube = cube.Segments[0].Name

	if p.accept(WHERE) {
		w, err := p.expression()
		if err != nil {
			return nil, err
		}
		q.Slicer = w
	}

	// Cell properties (ignoradas nesta fase).
	p.accept(CELL)
	if p.accept(PROPERTIES) {
		for {
			if _, err := p.compoundId(); err != nil {
				return nil, err
			}
			if !p.accept(COMMA) {
				break
			}
		}
	}
	return q, nil
}

func (p *parser) formula() (*Formula, error) {
	isMember := p.at(MEMBER)
	p.pos++ // MEMBER ou SET
	name, err := p.compoundId()
	if err != nil {
		return nil, err
	}
	if err := p.expect(AS); err != nil {
		return nil, err
	}
	e, err := p.formulaExpression()
	if err != nil {
		return nil, err
	}
	// Propriedades de membro (SOLVE_ORDER=..., etc.) — consumidas e ignoradas.
	for isMember && p.accept(COMMA) {
		if _, err := p.parseNameSegment(); err != nil {
			return nil, err
		}
		if err := p.expect(EQ); err != nil {
			return nil, err
		}
		if _, err := p.expression(); err != nil {
			return nil, err
		}
	}
	return &Formula{IsMember: isMember, Name: name, Exp: e}, nil
}

// formulaExpression aceita a sintaxe arcaica AS '<expr>' além de AS <expr>.
func (p *parser) formulaExpression() (Exp, error) {
	if p.at(STRING) {
		s := p.cur().Text
		p.pos++
		return ParseExpression(s)
	}
	return p.unaliasedExpression()
}

func (p *parser) axisSpecification() (*Axis, error) {
	ax := &Axis{}
	if p.at(NON) && p.peek().Type == EMPTY {
		p.pos += 2
		ax.NonEmpty = true
	}
	e, err := p.expression()
	if err != nil {
		return nil, err
	}
	ax.Exp = e

	// (DIMENSION)? PROPERTIES ... — ignoradas.
	p.accept(DIMENSION)
	if p.accept(PROPERTIES) {
		for {
			if _, err := p.compoundId(); err != nil {
				return nil, err
			}
			if !p.accept(COMMA) {
				break
			}
		}
	}

	if err := p.expect(ON); err != nil {
		return nil, err
	}
	ord, err := p.axisOrdinal()
	if err != nil {
		return nil, err
	}
	ax.Ordinal = ord
	return ax, nil
}

func (p *parser) axisOrdinal() (int, error) {
	switch p.cur().Type {
	case COLUMNS:
		p.pos++
		return 0, nil
	case ROWS:
		p.pos++
		return 1, nil
	case PAGES:
		p.pos++
		return 2, nil
	case CHAPTERS:
		p.pos++
		return 3, nil
	case SECTIONS:
		p.pos++
		return 4, nil
	case AXIS:
		p.pos++
		if err := p.expect(LPAREN); err != nil {
			return 0, err
		}
		n, err := p.axisNumber()
		if err != nil {
			return 0, err
		}
		if err := p.expect(RPAREN); err != nil {
			return 0, err
		}
		return n, nil
	case NUMBER:
		return p.axisNumber()
	default:
		return 0, p.errf("eixo inválido: esperava COLUMNS/ROWS/AXIS(n)/número")
	}
}

func (p *parser) axisNumber() (int, error) {
	t := p.cur()
	if t.Type != NUMBER {
		return 0, p.errf("esperava número de eixo")
	}
	p.pos++
	n, err := strconv.Atoi(t.Text)
	if err != nil || n < 0 {
		return 0, p.errf("ordinal de eixo inválido: %q", t.Text)
	}
	return n, nil
}

// --- expressões (precedência do Mondrian) --------------------------------

// expression: unaliasedExpression ( AS identifier )*
func (p *parser) expression() (Exp, error) {
	x, err := p.unaliasedExpression()
	if err != nil {
		return nil, err
	}
	for p.accept(AS) {
		seg, err := p.parseIdentifierSegment()
		if err != nil {
			return nil, err
		}
		x = &FunCall{Name: "AS", Syntax: SyntaxInfix, Args: []Exp{x, &Id{Segments: []Segment{seg}}}}
	}
	return x, nil
}

// unaliasedExpression: term5 ( (OR|XOR|':') term5 )*
func (p *parser) unaliasedExpression() (Exp, error) {
	x, err := p.term5()
	if err != nil {
		return nil, err
	}
	for {
		var name string
		switch p.cur().Type {
		case OR:
			name = "OR"
		case XOR:
			name = "XOR"
		case COLON:
			name = ":"
		default:
			return x, nil
		}
		p.pos++
		y, err := p.term5()
		if err != nil {
			return nil, err
		}
		x = infix(name, x, y)
	}
}

// term5: term4 ( AND term4 )*
func (p *parser) term5() (Exp, error) {
	x, err := p.term4()
	if err != nil {
		return nil, err
	}
	for p.accept(AND) {
		y, err := p.term4()
		if err != nil {
			return nil, err
		}
		x = infix("AND", x, y)
	}
	return x, nil
}

// term4: NOT term4 | term3
func (p *parser) term4() (Exp, error) {
	if p.accept(NOT) {
		x, err := p.term4()
		if err != nil {
			return nil, err
		}
		return &FunCall{Name: "NOT", Syntax: SyntaxPrefix, Args: []Exp{x}}, nil
	}
	return p.term3()
}

// term3: term2 ( comparações / IS / IN / MATCHES / NOT IN|MATCHES )*
func (p *parser) term3() (Exp, error) {
	x, err := p.term2()
	if err != nil {
		return nil, err
	}
	for {
		switch p.cur().Type {
		case EQ, NE, LT, GT, LE, GE:
			op := p.cur().Text
			p.pos++
			y, err := p.term2()
			if err != nil {
				return nil, err
			}
			x = infix(op, x, y)
		case IS:
			p.pos++
			switch p.cur().Type {
			case NULL:
				p.pos++
				x = &FunCall{Name: "IS NULL", Syntax: SyntaxPostfix, Args: []Exp{x}}
			case EMPTY:
				p.pos++
				x = &FunCall{Name: "IS EMPTY", Syntax: SyntaxPostfix, Args: []Exp{x}}
			default:
				y, err := p.term2()
				if err != nil {
					return nil, err
				}
				x = infix("IS", x, y)
			}
		case IN:
			p.pos++
			y, err := p.term2()
			if err != nil {
				return nil, err
			}
			x = infix("IN", x, y)
		case MATCHES:
			p.pos++
			y, err := p.term2()
			if err != nil {
				return nil, err
			}
			x = infix("MATCHES", x, y)
		case NOT:
			if nt := p.peek().Type; nt == IN || nt == MATCHES {
				p.pos += 2
				y, err := p.term2()
				if err != nil {
					return nil, err
				}
				name := "IN"
				if nt == MATCHES {
					name = "MATCHES"
				}
				x = &FunCall{Name: "NOT", Syntax: SyntaxPrefix, Args: []Exp{infix(name, x, y)}}
			} else {
				return x, nil
			}
		default:
			return x, nil
		}
	}
}

// term2: term ( (+|-|||) term )*
func (p *parser) term2() (Exp, error) {
	x, err := p.term()
	if err != nil {
		return nil, err
	}
	for {
		var name string
		switch p.cur().Type {
		case PLUS:
			name = "+"
		case MINUS:
			name = "-"
		case CONCAT:
			name = "||"
		default:
			return x, nil
		}
		p.pos++
		y, err := p.term()
		if err != nil {
			return nil, err
		}
		x = infix(name, x, y)
	}
}

// term: factor ( (*|/) factor )*
func (p *parser) term() (Exp, error) {
	x, err := p.factor()
	if err != nil {
		return nil, err
	}
	for {
		var name string
		switch p.cur().Type {
		case STAR:
			name = "*"
		case SLASH:
			name = "/"
		default:
			return x, nil
		}
		p.pos++
		y, err := p.factor()
		if err != nil {
			return nil, err
		}
		x = infix(name, x, y)
	}
}

// factor: (+) primary | (-) primary | primary
func (p *parser) factor() (Exp, error) {
	if p.accept(PLUS) {
		return p.primary()
	}
	if p.accept(MINUS) {
		x, err := p.primary()
		if err != nil {
			return nil, err
		}
		return &FunCall{Name: "-", Syntax: SyntaxPrefix, Args: []Exp{x}}, nil
	}
	return p.primary()
}

// primary: atom ( DOT segmentOrFuncall )*
func (p *parser) primary() (Exp, error) {
	e, err := p.atom()
	if err != nil {
		return nil, err
	}
	for p.accept(DOT) {
		seg, err := p.parseIdentifierSegment()
		if err != nil {
			return nil, err
		}
		args, hasParens, err := p.maybeArgs()
		if err != nil {
			return nil, err
		}
		e = createCall(e, seg, args, hasParens)
	}
	return e, nil
}

// atom: literais | NULL | CAST | (tupla) | {set} | CASE | nome[(args)]
func (p *parser) atom() (Exp, error) {
	switch p.cur().Type {
	case STRING:
		s := p.cur().Text
		p.pos++
		return &StringLiteral{Value: s}, nil
	case NUMBER:
		return p.numericLiteral()
	case NULL:
		p.pos++
		return &NullLiteral{}, nil
	case CAST:
		return p.castExpression()
	case LPAREN:
		p.pos++
		lis, err := p.expList()
		if err != nil {
			return nil, err
		}
		if err := p.expect(RPAREN); err != nil {
			return nil, err
		}
		return &FunCall{Name: "()", Syntax: SyntaxParentheses, Args: lis}, nil
	case LBRACE:
		p.pos++
		var lis []Exp
		if !p.at(RBRACE) {
			var err error
			lis, err = p.expList()
			if err != nil {
				return nil, err
			}
		}
		if err := p.expect(RBRACE); err != nil {
			return nil, err
		}
		return &FunCall{Name: "{}", Syntax: SyntaxBraces, Args: lis}, nil
	case CASE:
		return p.caseExpression()
	default:
		seg, err := p.parseNameSegment()
		if err != nil {
			return nil, err
		}
		// Qualificadores foo!bar!baz — mantém o último (como o Mondrian).
		for p.accept(BANG) {
			seg, err = p.parseNameSegment()
			if err != nil {
				return nil, err
			}
		}
		args, hasParens, err := p.maybeArgs()
		if err != nil {
			return nil, err
		}
		return createCall(nil, seg, args, hasParens), nil
	}
}

func (p *parser) castExpression() (Exp, error) {
	p.pos++ // CAST
	if err := p.expect(LPAREN); err != nil {
		return nil, err
	}
	e, err := p.unaliasedExpression()
	if err != nil {
		return nil, err
	}
	if err := p.expect(AS); err != nil {
		return nil, err
	}
	typeSeg, err := p.parseNameSegment()
	if err != nil {
		return nil, err
	}
	if err := p.expect(RPAREN); err != nil {
		return nil, err
	}
	return &FunCall{Name: "CAST", Syntax: SyntaxCast, Args: []Exp{e, &Id{Segments: []Segment{typeSeg}}}}, nil
}

func (p *parser) caseExpression() (Exp, error) {
	p.pos++ // CASE
	var args []Exp
	match := false
	if !p.at(WHEN) && !p.at(ELSE) && !p.at(END) {
		e, err := p.expression()
		if err != nil {
			return nil, err
		}
		match = true
		args = append(args, e)
	}
	for p.accept(WHEN) {
		w, err := p.expression()
		if err != nil {
			return nil, err
		}
		if err := p.expect(THEN); err != nil {
			return nil, err
		}
		t, err := p.expression()
		if err != nil {
			return nil, err
		}
		args = append(args, w, t)
	}
	if p.accept(ELSE) {
		e, err := p.expression()
		if err != nil {
			return nil, err
		}
		args = append(args, e)
	}
	if err := p.expect(END); err != nil {
		return nil, err
	}
	name := "_CaseTest"
	if match {
		name = "_CaseMatch"
	}
	return &FunCall{Name: name, Syntax: SyntaxCase, Args: args}, nil
}

func (p *parser) numericLiteral() (Exp, error) {
	t := p.cur()
	p.pos++
	v, err := strconv.ParseFloat(t.Text, 64)
	if err != nil {
		return nil, p.errf("número inválido: %q", t.Text)
	}
	return &NumericLiteral{Value: v, Raw: t.Text}, nil
}

// maybeArgs lê uma lista de argumentos entre parênteses, se presente.
func (p *parser) maybeArgs() (args []Exp, hasParens bool, err error) {
	if !p.accept(LPAREN) {
		return nil, false, nil
	}
	if p.at(RPAREN) {
		p.pos++
		return []Exp{}, true, nil
	}
	args, err = p.expList()
	if err != nil {
		return nil, true, err
	}
	if err := p.expect(RPAREN); err != nil {
		return nil, true, err
	}
	return args, true, nil
}

// expList: expression ( COMMA expression )*
func (p *parser) expList() ([]Exp, error) {
	var list []Exp
	e, err := p.expression()
	if err != nil {
		return nil, err
	}
	list = append(list, e)
	for p.accept(COMMA) {
		e, err := p.expression()
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, nil
}

// --- identificadores -----------------------------------------------------

func (p *parser) compoundId() (*Id, error) {
	seg, err := p.parseIdentifierSegment()
	if err != nil {
		return nil, err
	}
	id := &Id{Segments: []Segment{seg}}
	for p.at(DOT) {
		p.pos++
		seg, err := p.parseIdentifierSegment()
		if err != nil {
			return nil, err
		}
		id.Segments = append(id.Segments, seg)
	}
	return id, nil
}

// parseNameSegment aceita IDENT, [quotado] ou as soft-keywords Dimension/Properties.
func (p *parser) parseNameSegment() (Segment, error) {
	switch p.cur().Type {
	case IDENT:
		s := p.cur().Text
		p.pos++
		return Segment{Name: s}, nil
	case QUOTEDID:
		s := p.cur().Text
		p.pos++
		return Segment{Name: s, Quoted: true}, nil
	case DIMENSION:
		p.pos++
		return Segment{Name: "Dimension"}, nil
	case PROPERTIES:
		p.pos++
		return Segment{Name: "Properties"}, nil
	default:
		return Segment{}, p.errf("esperava identificador")
	}
}

// parseIdentifierSegment aceita também chaves &[..].
func (p *parser) parseIdentifierSegment() (Segment, error) {
	if p.at(KEYID) {
		s := p.cur().Text
		p.pos++
		return Segment{Name: s, Key: true}, nil
	}
	return p.parseNameSegment()
}

// --- helpers de construção ----------------------------------------------

func infix(name string, x, y Exp) *FunCall {
	return &FunCall{Name: name, Syntax: SyntaxInfix, Args: []Exp{x, y}}
}

// createCall espelha a lógica do Mondrian: sem parênteses, segmentos estendem um
// Id (membros compostos / propriedades como segmentos); com parênteses, viram
// função (sem left) ou método (com left).
func createCall(left Exp, seg Segment, args []Exp, hasParens bool) Exp {
	if !hasParens {
		if left == nil {
			return &Id{Segments: []Segment{seg}}
		}
		if id, ok := left.(*Id); ok {
			id.Segments = append(id.Segments, seg)
			return id
		}
		return &FunCall{Name: seg.Name, Syntax: SyntaxProperty, Args: []Exp{left}}
	}
	if left == nil {
		return &FunCall{Name: seg.Name, Syntax: SyntaxFunction, Args: args}
	}
	return &FunCall{Name: seg.Name, Syntax: SyntaxMethod, Args: append([]Exp{left}, args...)}
}
