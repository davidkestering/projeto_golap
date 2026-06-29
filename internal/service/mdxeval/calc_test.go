package mdxeval

import (
	"testing"

	"cubodw/internal/engine/mdx"
)

func TestEvalNumeric(t *testing.T) {
	env := map[string]float64{"store sales": 100, "store cost": 30}
	e, _ := mdx.ParseExpression(`[Measures].[Store Sales] - [Measures].[Store Cost]`)
	v, ok := evalNumeric(e, env, nil)
	if !ok || v != 70 {
		t.Fatalf("got %v,%v quero 70", v, ok)
	}

	e2, _ := mdx.ParseExpression(`([Measures].[Store Sales] + 50) * 2`)
	if v, ok := evalNumeric(e2, env, nil); !ok || v != 300 {
		t.Errorf("got %v,%v quero 300", v, ok)
	}

	// Divisão por zero => indefinido.
	envz := map[string]float64{"a": 1, "b": 0}
	ez, _ := mdx.ParseExpression(`[Measures].[A] / [Measures].[B]`)
	if _, ok := evalNumeric(ez, envz, nil); ok {
		t.Error("divisão por zero deveria ser indefinida")
	}
}

func TestEvalNumericWithCalcRegistry(t *testing.T) {
	q, err := mdx.ParseQuery(`WITH MEMBER [Measures].[Profit] AS [Measures].[Store Sales] - [Measures].[Store Cost] SELECT {[Measures].[Profit]} ON 0 FROM [Sales]`)
	if err != nil {
		t.Fatal(err)
	}
	reg := buildCalcRegistry(q)
	if !reg.has("Profit") {
		t.Fatalf("registry sem Profit: %v", reg)
	}
	env := map[string]float64{"store sales": 565238.13, "store cost": 225627.23}
	ref, _ := mdx.ParseExpression(`[Measures].[Profit]`)
	v, ok := evalNumeric(ref, env, reg)
	if !ok {
		t.Fatal("Profit não avaliou")
	}
	if d := v - 339610.90; d < -0.01 || d > 0.01 {
		t.Errorf("Profit = %v, quero ~339610.90", v)
	}
}

func TestEvalBool(t *testing.T) {
	env := map[string]float64{"unit sales": 2000}
	gt, _ := mdx.ParseExpression(`[Measures].[Unit Sales] > 1000`)
	if !evalBool(gt, env, nil) {
		t.Error("2000 > 1000 deveria ser true")
	}
	lt, _ := mdx.ParseExpression(`[Measures].[Unit Sales] < 1000`)
	if evalBool(lt, env, nil) {
		t.Error("2000 < 1000 deveria ser false")
	}
	andE, _ := mdx.ParseExpression(`[Measures].[Unit Sales] > 1000 AND [Measures].[Unit Sales] < 3000`)
	if !evalBool(andE, env, nil) {
		t.Error("AND deveria ser true")
	}
}

func TestIsMeasuresExp(t *testing.T) {
	m, _ := mdx.ParseExpression(`{[Measures].[Unit Sales], [Measures].[Store Sales]}`)
	if !isMeasuresExp(m) {
		t.Error("conjunto de medidas não reconhecido")
	}
	mem, _ := mdx.ParseExpression(`[Store].[Store Country].Members`)
	if isMeasuresExp(mem) {
		t.Error("conjunto de membros não deveria ser medidas")
	}
}
