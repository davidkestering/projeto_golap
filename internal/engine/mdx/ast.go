package mdx

import (
	"strconv"
	"strings"
)

// Syntax descreve como uma FunCall é escrita/aplicada (espelha mondrian.olap.Syntax).
type Syntax int

const (
	SyntaxFunction    Syntax = iota // Foo(a, b)
	SyntaxMethod                    // exp.Meth(a)
	SyntaxProperty                  // exp.Prop
	SyntaxInfix                     // a + b
	SyntaxPrefix                    // - a / NOT a
	SyntaxPostfix                   // a IS NULL
	SyntaxBraces                    // {a, b}
	SyntaxParentheses               // (a, b)  (tupla/parênteses)
	SyntaxCase                      // CASE ... END
	SyntaxCast                      // CAST(a AS t)
)

// Exp é qualquer expressão MDX.
type Exp interface {
	String() string
	exp()
}

// FunCall é a aplicação de uma função/operador (não resolvida; o significado é
// atribuído na fase de avaliação). Espelha mondrian UnresolvedFunCall.
type FunCall struct {
	Name   string
	Syntax Syntax
	Args   []Exp
}

func (*FunCall) exp() {}

// Id é um identificador composto: segmentos separados por ponto, ex.:
// [Measures].[Unit Sales] ou [Time].[1997].&[Q1].
type Id struct {
	Segments []Segment
}

func (*Id) exp() {}

// Segment é um segmento de um Id.
type Segment struct {
	Name   string
	Quoted bool // veio entre colchetes [..]
	Key    bool // veio como &chave
}

// NumericLiteral é um literal numérico.
type NumericLiteral struct {
	Value float64
	Raw   string
}

func (*NumericLiteral) exp() {}

// StringLiteral é um literal string.
type StringLiteral struct{ Value string }

func (*StringLiteral) exp() {}

// NullLiteral é o literal NULL.
type NullLiteral struct{}

func (*NullLiteral) exp() {}

// --- Query ---------------------------------------------------------------

// Query é uma instrução SELECT MDX.
type Query struct {
	Formulas []*Formula // cláusula WITH
	Axes     []*Axis
	Cube     string
	Slicer   Exp // expressão do WHERE (pode ser nil)
}

// Formula é uma definição WITH MEMBER ou WITH SET.
type Formula struct {
	IsMember bool
	Name     *Id
	Exp      Exp
}

// Axis é um eixo do SELECT: [NON EMPTY] <set> ON <ordinal>.
type Axis struct {
	NonEmpty bool
	Exp      Exp
	Ordinal  int // 0=COLUMNS,1=ROWS,2=PAGES,3=CHAPTERS,4=SECTIONS
}

// AxisName devolve o nome canônico do eixo pelo ordinal.
func AxisName(ordinal int) string {
	switch ordinal {
	case 0:
		return "COLUMNS"
	case 1:
		return "ROWS"
	case 2:
		return "PAGES"
	case 3:
		return "CHAPTERS"
	case 4:
		return "SECTIONS"
	default:
		return "AXIS(" + strconv.Itoa(ordinal) + ")"
	}
}

// --- String() ------------------------------------------------------------

func (s Segment) String() string {
	switch {
	case s.Key:
		return "&[" + strings.ReplaceAll(s.Name, "]", "]]") + "]"
	case s.Quoted:
		return "[" + strings.ReplaceAll(s.Name, "]", "]]") + "]"
	default:
		return s.Name
	}
}

func (id *Id) String() string {
	parts := make([]string, len(id.Segments))
	for i, s := range id.Segments {
		parts[i] = s.String()
	}
	return strings.Join(parts, ".")
}

func (n *NumericLiteral) String() string { return n.Raw }
func (s *StringLiteral) String() string {
	return "'" + strings.ReplaceAll(s.Value, "'", "''") + "'"
}
func (*NullLiteral) String() string { return "NULL" }

func (f *FunCall) String() string {
	switch f.Syntax {
	case SyntaxBraces:
		return "{" + joinArgs(f.Args, ", ") + "}"
	case SyntaxParentheses:
		return "(" + joinArgs(f.Args, ", ") + ")"
	case SyntaxInfix:
		return f.Args[0].String() + " " + f.Name + " " + f.Args[1].String()
	case SyntaxPrefix:
		return f.Name + " " + f.Args[0].String()
	case SyntaxPostfix:
		return f.Args[0].String() + " " + f.Name
	case SyntaxProperty:
		return f.Args[0].String() + "." + f.Name
	case SyntaxMethod:
		return f.Args[0].String() + "." + f.Name + "(" + joinArgs(f.Args[1:], ", ") + ")"
	case SyntaxCast:
		return "CAST(" + f.Args[0].String() + " AS " + f.Args[1].String() + ")"
	case SyntaxCase:
		return caseString(f)
	default: // SyntaxFunction
		return f.Name + "(" + joinArgs(f.Args, ", ") + ")"
	}
}

func caseString(f *FunCall) string {
	var b strings.Builder
	b.WriteString("CASE")
	for _, a := range f.Args {
		b.WriteString(" ")
		b.WriteString(a.String())
	}
	b.WriteString(" END")
	return b.String()
}

func joinArgs(args []Exp, sep string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = a.String()
	}
	return strings.Join(parts, sep)
}

// String reconstrói uma forma canônica (não necessariamente idêntica à entrada)
// da query — útil para depuração e para gerar MDX a partir de queries thin.
func (q *Query) String() string {
	var b strings.Builder
	if len(q.Formulas) > 0 {
		b.WriteString("WITH")
		for _, f := range q.Formulas {
			if f.IsMember {
				b.WriteString("\nMEMBER ")
			} else {
				b.WriteString("\nSET ")
			}
			b.WriteString(f.Name.String())
			b.WriteString(" AS ")
			b.WriteString(f.Exp.String())
		}
		b.WriteString("\n")
	}
	b.WriteString("SELECT")
	for i, ax := range q.Axes {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n")
		if ax.NonEmpty {
			b.WriteString("NON EMPTY ")
		}
		b.WriteString(ax.Exp.String())
		b.WriteString(" ON ")
		b.WriteString(AxisName(ax.Ordinal))
	}
	b.WriteString("\nFROM [")
	b.WriteString(strings.ReplaceAll(q.Cube, "]", "]]"))
	b.WriteString("]")
	if q.Slicer != nil {
		b.WriteString("\nWHERE ")
		b.WriteString(q.Slicer.String())
	}
	return b.String()
}
