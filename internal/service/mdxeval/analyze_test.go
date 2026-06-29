package mdxeval

import (
	"testing"

	"cubodw/internal/demo"
	"cubodw/internal/engine/mdx"
	"cubodw/internal/engine/metadata"
)

func salesCube(t *testing.T) *metadata.Cube {
	t.Helper()
	s, err := demo.Schema()
	if err != nil {
		t.Fatalf("demo.Schema: %v", err)
	}
	c, ok := s.FindCube("Sales")
	if !ok {
		t.Fatal("cubo Sales ausente")
	}
	return c
}

func analyze(t *testing.T, cube *metadata.Cube, expr string) axisPlan {
	t.Helper()
	e, err := mdx.ParseExpression(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	p, err := analyzeAxis(cube, e)
	if err != nil {
		t.Fatalf("analyze %q: %v", expr, err)
	}
	return p
}

func TestAnalyzeMeasures(t *testing.T) {
	c := salesCube(t)
	p := analyze(t, c, `{[Measures].[Unit Sales], [Measures].[Store Sales]}`)
	if !p.isMeasures || len(p.measures) != 2 {
		t.Fatalf("plano de medidas inesperado: %+v", p)
	}
}

func TestAnalyzeLevelMembers(t *testing.T) {
	c := salesCube(t)
	p := analyze(t, c, `[Store].[Store Country].Members`)
	if p.isMeasures || len(p.bindings) != 1 {
		t.Fatalf("plano inesperado: %+v", p)
	}
	if p.bindings[0].ref.Level != "Store Country" || len(p.bindings[0].filters) != 0 {
		t.Errorf("binding inesperado: %+v", p.bindings[0])
	}
}

func TestAnalyzeCrossJoin(t *testing.T) {
	c := salesCube(t)
	p := analyze(t, c, `CrossJoin([Gender].[Gender].Members, [Marital Status].[Marital Status].Members)`)
	if len(p.bindings) != 2 {
		t.Fatalf("crossjoin deveria dar 2 bindings: %+v", p.bindings)
	}
}

func TestResolveMemberWithParent(t *testing.T) {
	c := salesCube(t)
	e, _ := mdx.ParseExpression(`[Store].[USA].[CA]`)
	ref, err := resolveMemberId(c, e.(*mdx.Id))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref.levelIndex != 1 {
		t.Errorf("levelIndex = %d, quero 1 (Store State)", ref.levelIndex)
	}
	if ref.values[0] != "USA" || ref.values[1] != "CA" {
		t.Errorf("values = %v", ref.values)
	}
}

func TestAnalyzeChildren(t *testing.T) {
	c := salesCube(t)
	p := analyze(t, c, `[Store].[USA].Children`)
	if len(p.bindings) != 1 {
		t.Fatalf("children deveria dar 1 binding: %+v", p.bindings)
	}
	b := p.bindings[0]
	if b.ref.Level != "Store State" {
		t.Errorf("nível-filho = %q, quero Store State", b.ref.Level)
	}
	// Deve filtrar Store Country = USA.
	var hasParent bool
	for _, f := range b.filters {
		if f.Level == "Store Country" && len(f.Members) == 1 && f.Members[0] == "USA" {
			hasParent = true
		}
	}
	if !hasParent {
		t.Errorf("filtro de ancestral ausente: %+v", b.filters)
	}
}

func TestAnalyzeSlicer(t *testing.T) {
	c := salesCube(t)
	e, _ := mdx.ParseExpression(`([Time].[1997], [Measures].[Store Sales])`)
	filters, measure, err := analyzeSlicer(c, e)
	if err != nil {
		t.Fatalf("slicer: %v", err)
	}
	if measure == nil || measure.Name != "Store Sales" {
		t.Errorf("medida do slicer = %v", measure)
	}
	if len(filters) != 1 || filters[0].Level != "Year" || filters[0].Members[0] != "1997" {
		t.Errorf("filtros do slicer inesperados: %+v", filters)
	}
}

func TestUnsupportedFunctionErrors(t *testing.T) {
	c := salesCube(t)
	e, _ := mdx.ParseExpression(`Order([Store].[Store Country].Members, [Measures].[Unit Sales], BDESC)`)
	if _, err := analyzeAxis(c, e); err == nil {
		t.Fatal("esperava erro para Order (ainda não suportado)")
	}
}
