package mdxeval

import (
	"strconv"
	"strings"

	"cubodw/internal/engine/mdx"
	"cubodw/internal/engine/metadata"
)

// calcRegistry guarda os membros calculados (WITH MEMBER) por nome (minúsculo).
// Apenas membros calculados na dimensão [Measures] são suportados nesta fase.
type calcRegistry map[string]mdx.Exp

func buildCalcRegistry(q *mdx.Query) calcRegistry {
	reg := calcRegistry{}
	for _, f := range q.Formulas {
		if !f.IsMember || f.Name == nil {
			continue
		}
		if len(f.Name.Segments) >= 1 && strings.EqualFold(f.Name.Segments[0].Name, metadata.MeasuresDimension) {
			reg[strings.ToLower(lastSeg(f.Name))] = f.Exp
		}
	}
	return reg
}

func (r calcRegistry) has(name string) bool {
	_, ok := r[strings.ToLower(name)]
	return ok
}

// measureSlot é uma posição do eixo de medidas: ou uma medida-base, ou um membro
// calculado (expressão sobre medidas).
type measureSlot struct {
	name       string
	caption    string
	uniqueName string
	isCalc     bool
	exp        mdx.Exp           // se calc
	base       *metadata.Measure // se base
}

// isMeasuresExp indica se a expressão de eixo é (um conjunto de) medida(s).
func isMeasuresExp(exp mdx.Exp) bool {
	switch e := exp.(type) {
	case *mdx.Id:
		return isMeasureId(e)
	case *mdx.FunCall:
		if e.Syntax != mdx.SyntaxBraces || len(e.Args) == 0 {
			return false
		}
		for _, a := range e.Args {
			id, ok := a.(*mdx.Id)
			if !ok || !isMeasureId(id) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// extractMeasureIds achata os Ids de medida de uma expressão de medidas.
func extractMeasureIds(exp mdx.Exp) []*mdx.Id {
	switch e := exp.(type) {
	case *mdx.Id:
		return []*mdx.Id{e}
	case *mdx.FunCall:
		var ids []*mdx.Id
		for _, a := range e.Args {
			if id, ok := a.(*mdx.Id); ok {
				ids = append(ids, id)
			}
		}
		return ids
	}
	return nil
}

// resolveMeasureSlot resolve um Id de medida para um slot (base ou calc).
func resolveMeasureSlot(cube *metadata.Cube, id *mdx.Id, reg calcRegistry) (measureSlot, error) {
	name := lastSeg(id)
	if reg.has(name) {
		return measureSlot{
			name:       name,
			caption:    name,
			uniqueName: metadata.Bracket(metadata.MeasuresDimension) + "." + metadata.Bracket(name),
			isCalc:     true,
			exp:        reg[strings.ToLower(name)],
		}, nil
	}
	m, err := resolveMeasure(cube, id)
	if err != nil {
		return measureSlot{}, err
	}
	return measureSlot{name: m.Name, caption: m.Caption, uniqueName: m.UniqueName(), base: m}, nil
}

// collectBaseMeasures acumula as medidas-base referenciadas por uma expressão
// (expandindo membros calculados via registry).
func collectBaseMeasures(exp mdx.Exp, cube *metadata.Cube, reg calcRegistry, into map[string]*metadata.Measure, order *[]*metadata.Measure) {
	switch e := exp.(type) {
	case *mdx.Id:
		if !isMeasureId(e) {
			return
		}
		name := lastSeg(e)
		if reg.has(name) {
			collectBaseMeasures(reg[strings.ToLower(name)], cube, reg, into, order)
			return
		}
		if m, ok := cube.FindMeasure(name); ok {
			addMeasure(m, into, order)
			return
		}
		for _, m := range cube.Measures {
			if strings.EqualFold(m.Name, name) {
				addMeasure(m, into, order)
				return
			}
		}
	case *mdx.FunCall:
		for _, a := range e.Args {
			collectBaseMeasures(a, cube, reg, into, order)
		}
	}
}

func addMeasure(m *metadata.Measure, into map[string]*metadata.Measure, order *[]*metadata.Measure) {
	key := strings.ToLower(m.Name)
	if _, ok := into[key]; ok {
		return
	}
	into[key] = m
	*order = append(*order, m)
}

// evalNumeric avalia uma expressão numérica MDX sobre um ambiente de valores de
// medidas-base (nome minúsculo -> valor). Devolve ok=false quando indefinido
// (medida ausente, divisão por zero, expressão não-numérica).
func evalNumeric(exp mdx.Exp, env map[string]float64, reg calcRegistry) (float64, bool) {
	switch e := exp.(type) {
	case *mdx.NumericLiteral:
		return e.Value, true
	case *mdx.Id:
		if !isMeasureId(e) {
			return 0, false
		}
		name := strings.ToLower(lastSeg(e))
		if v, ok := env[name]; ok {
			return v, true
		}
		if sub, ok := reg[name]; ok {
			return evalNumeric(sub, env, reg)
		}
		return 0, false
	case *mdx.FunCall:
		switch e.Syntax {
		case mdx.SyntaxFunction:
			switch strings.ToUpper(e.Name) {
			case "IIF":
				if len(e.Args) == 3 {
					if evalBool(e.Args[0], env, reg) {
						return evalNumeric(e.Args[1], env, reg)
					}
					return evalNumeric(e.Args[2], env, reg)
				}
			case "COALESCEEMPTY":
				for _, a := range e.Args {
					if v, ok := evalNumeric(a, env, reg); ok {
						return v, true
					}
				}
				return 0, false
			}
			return 0, false
		case mdx.SyntaxParentheses:
			if len(e.Args) == 1 {
				return evalNumeric(e.Args[0], env, reg)
			}
			return 0, false
		case mdx.SyntaxPrefix:
			if e.Name == "-" && len(e.Args) == 1 {
				v, ok := evalNumeric(e.Args[0], env, reg)
				return -v, ok
			}
			return 0, false
		case mdx.SyntaxInfix:
			if len(e.Args) != 2 {
				return 0, false
			}
			a, ok1 := evalNumeric(e.Args[0], env, reg)
			b, ok2 := evalNumeric(e.Args[1], env, reg)
			if !ok1 || !ok2 {
				return 0, false
			}
			switch e.Name {
			case "+":
				return a + b, true
			case "-":
				return a - b, true
			case "*":
				return a * b, true
			case "/":
				if b == 0 {
					return 0, false
				}
				return a / b, true
			}
		}
	}
	return 0, false
}

// evalBool avalia uma condição (Filter): comparações entre numéricos, com AND/OR/NOT.
func evalBool(exp mdx.Exp, env map[string]float64, reg calcRegistry) bool {
	fc, ok := exp.(*mdx.FunCall)
	if !ok {
		v, ok := evalNumeric(exp, env, reg)
		return ok && v != 0
	}
	switch fc.Syntax {
	case mdx.SyntaxPrefix:
		if fc.Name == "NOT" && len(fc.Args) == 1 {
			return !evalBool(fc.Args[0], env, reg)
		}
	case mdx.SyntaxParentheses:
		if len(fc.Args) == 1 {
			return evalBool(fc.Args[0], env, reg)
		}
	case mdx.SyntaxInfix:
		if len(fc.Args) != 2 {
			return false
		}
		switch strings.ToUpper(fc.Name) {
		case "AND":
			return evalBool(fc.Args[0], env, reg) && evalBool(fc.Args[1], env, reg)
		case "OR":
			return evalBool(fc.Args[0], env, reg) || evalBool(fc.Args[1], env, reg)
		}
		a, ok1 := evalNumeric(fc.Args[0], env, reg)
		b, ok2 := evalNumeric(fc.Args[1], env, reg)
		if !ok1 || !ok2 {
			return false
		}
		switch fc.Name {
		case ">":
			return a > b
		case "<":
			return a < b
		case ">=":
			return a >= b
		case "<=":
			return a <= b
		case "=":
			return a == b
		case "<>":
			return a != b
		}
	}
	return false
}

// formatNumber formata um valor de célula (sem casas se inteiro, senão 2 casas).
func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

// toFloat converte um valor de record para float64.
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}
