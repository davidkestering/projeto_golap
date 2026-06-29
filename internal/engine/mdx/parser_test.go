package mdx

import "testing"

func TestParseMemberRefIsSingleId(t *testing.T) {
	e, err := ParseExpression(`[Measures].[Unit Sales]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	id, ok := e.(*Id)
	if !ok {
		t.Fatalf("esperava *Id, veio %T", e)
	}
	if len(id.Segments) != 2 || id.Segments[0].Name != "Measures" || id.Segments[1].Name != "Unit Sales" {
		t.Errorf("segmentos inesperados: %+v", id.Segments)
	}
}

func TestParseMethodVsProperty(t *testing.T) {
	// .Children sem parênteses => segmento de Id (membro composto).
	e, _ := ParseExpression(`[Store].[USA].Children`)
	if id, ok := e.(*Id); !ok || len(id.Segments) != 3 {
		t.Fatalf("esperava Id de 3 segmentos, veio %T %v", e, e)
	}
	// .Lag(1) com parênteses => método (FunCall).
	e2, _ := ParseExpression(`[Time].[1997].Lag(1)`)
	fc, ok := e2.(*FunCall)
	if !ok || fc.Name != "Lag" || fc.Syntax != SyntaxMethod || len(fc.Args) != 2 {
		t.Fatalf("esperava método Lag, veio %T %+v", e2, e2)
	}
}

func TestParsePrecedence(t *testing.T) {
	// 1 + 2 * 3  =>  +(1, *(2,3))
	e, _ := ParseExpression(`1 + 2 * 3`)
	add, ok := e.(*FunCall)
	if !ok || add.Name != "+" {
		t.Fatalf("topo deveria ser +, veio %v", e)
	}
	mul, ok := add.Args[1].(*FunCall)
	if !ok || mul.Name != "*" {
		t.Fatalf("RHS deveria ser *, veio %v", add.Args[1])
	}
}

func TestParseFunctionAndTuple(t *testing.T) {
	e, err := ParseExpression(`CrossJoin([Store].Children, ([Time].[1997], [Gender].[F]))`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cj, ok := e.(*FunCall)
	if !ok || cj.Name != "CrossJoin" || cj.Syntax != SyntaxFunction || len(cj.Args) != 2 {
		t.Fatalf("CrossJoin inesperado: %T %+v", e, e)
	}
	tup, ok := cj.Args[1].(*FunCall)
	if !ok || tup.Syntax != SyntaxParentheses || len(tup.Args) != 2 {
		t.Fatalf("tupla inesperada: %+v", cj.Args[1])
	}
}

func TestParseFullQuery(t *testing.T) {
	mdx := `SELECT
  NON EMPTY {[Measures].[Unit Sales], [Measures].[Store Sales]} ON COLUMNS,
  {[Store].[USA].Children} ON ROWS
FROM [Sales]
WHERE ([Time].[1997])`
	q, err := ParseQuery(mdx)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if q.Cube != "Sales" {
		t.Errorf("cube = %q", q.Cube)
	}
	if len(q.Axes) != 2 {
		t.Fatalf("eixos = %d", len(q.Axes))
	}
	if !q.Axes[0].NonEmpty || q.Axes[0].Ordinal != 0 {
		t.Errorf("eixo 0 inesperado: %+v", q.Axes[0])
	}
	if q.Axes[1].Ordinal != 1 {
		t.Errorf("eixo 1 ordinal = %d", q.Axes[1].Ordinal)
	}
	if q.Slicer == nil {
		t.Error("slicer (WHERE) nulo")
	}
}

func TestParseWithMember(t *testing.T) {
	mdx := `WITH MEMBER [Measures].[Profit] AS [Measures].[Store Sales] - [Measures].[Store Cost]
SELECT {[Measures].[Profit]} ON 0 FROM [Sales]`
	q, err := ParseQuery(mdx)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(q.Formulas) != 1 || !q.Formulas[0].IsMember {
		t.Fatalf("fórmula WITH MEMBER ausente: %+v", q.Formulas)
	}
	if q.Formulas[0].Name.String() != "[Measures].[Profit]" {
		t.Errorf("nome do membro = %q", q.Formulas[0].Name.String())
	}
	if len(q.Axes) != 1 || q.Axes[0].Ordinal != 0 {
		t.Errorf("eixo inesperado: %+v", q.Axes)
	}
}

func TestRoundTripStable(t *testing.T) {
	mdx := `SELECT {[Measures].[Unit Sales]} ON COLUMNS, [Store].[Store Country].Members ON ROWS FROM [Sales] WHERE [Time].[1997]`
	q1, err := ParseQuery(mdx)
	if err != nil {
		t.Fatalf("parse 1: %v", err)
	}
	q2, err := ParseQuery(q1.String())
	if err != nil {
		t.Fatalf("parse 2 (de %q): %v", q1.String(), err)
	}
	if q1.String() != q2.String() {
		t.Errorf("round-trip instável:\n1: %s\n2: %s", q1.String(), q2.String())
	}
}

func TestParseErrorMissingFrom(t *testing.T) {
	if _, err := ParseQuery(`SELECT {[Measures].[Unit Sales]} ON COLUMNS`); err == nil {
		t.Fatal("esperava erro por falta de FROM")
	}
}
