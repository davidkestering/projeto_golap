package mdxeval

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"cubodw/internal/engine/mdx"
	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	"cubodw/internal/service/queryexec"
)

// position é uma posição de um eixo de nível: valores por binding + (opcional)
// ambiente de medidas-base usado por Order/TopCount/Filter.
type position struct {
	values []string
	env    map[string]float64
}

// resolvedAxis é um eixo já resolvido: medidas, ou níveis com posições explícitas
// (já ordenadas/limitadas/filtradas).
type resolvedAxis struct {
	ordinal    int
	isMeasures bool
	slots      []measureSlot  // eixo de medidas
	bindings   []levelBinding // eixo de níveis
	positions  []position     // eixo de níveis (explícito, ordenado)
}

// Evaluate avalia a query MDX contra o cubo, executando via exec.
func Evaluate(ctx context.Context, cube *metadata.Cube, q *mdx.Query, exec *queryexec.Service) (*CellSet, error) {
	if !exec.HasDB() {
		return nil, fmt.Errorf("sem conexão de banco")
	}
	reg := buildCalcRegistry(q)

	var slicerFilters []query.Filter
	var slicerMeasure *metadata.Measure
	if q.Slicer != nil {
		f, m, err := analyzeSlicer(cube, q.Slicer)
		if err != nil {
			return nil, fmt.Errorf("WHERE: %w", err)
		}
		slicerFilters, slicerMeasure = f, m
	}

	axes := make([]resolvedAxis, len(q.Axes))
	measureAxis := -1
	for i, ax := range q.Axes {
		ra, err := resolveAxis(ctx, cube, ax.Exp, ax.Ordinal, slicerFilters, exec, reg)
		if err != nil {
			return nil, fmt.Errorf("eixo %s: %w", mdx.AxisName(ax.Ordinal), err)
		}
		if ra.isMeasures {
			if measureAxis >= 0 {
				return nil, fmt.Errorf("medidas aparecem em mais de um eixo")
			}
			measureAxis = i
		}
		axes[i] = ra
	}

	// Slots de medida para exibição.
	var slots []measureSlot
	if measureAxis >= 0 {
		slots = axes[measureAxis].slots
	} else {
		m, err := defaultMeasure(cube, slicerMeasure)
		if err != nil {
			return nil, err
		}
		slots = []measureSlot{{name: m.Name, caption: m.Caption, uniqueName: m.UniqueName(), base: m}}
	}

	// Medidas-base a buscar (expandindo calc members).
	into := map[string]*metadata.Measure{}
	var baseMeasures []*metadata.Measure
	for _, s := range slots {
		if s.isCalc {
			collectBaseMeasures(s.exp, cube, reg, into, &baseMeasures)
		} else {
			addMeasure(s.base, into, &baseMeasures)
		}
	}

	// Query da grade de células: agrupa por todos os níveis de eixo, restrita às
	// posições sobreviventes.
	qry := query.Query{Cube: cube.Name, Filters: append([]query.Filter(nil), slicerFilters...)}
	ranges := make([][2]int, len(axes))
	for i, ra := range axes {
		if ra.isMeasures {
			continue
		}
		ranges[i][0] = len(qry.Rows)
		for _, b := range ra.bindings {
			qry.Rows = append(qry.Rows, b.ref)
			qry.Filters = append(qry.Filters, b.filters...)
		}
		ranges[i][1] = len(qry.Rows)
		for bi := range ra.bindings {
			qry.Filters = append(qry.Filters, query.Filter{
				Dimension: ra.bindings[bi].ref.Dimension,
				Level:     ra.bindings[bi].ref.Level,
				Members:   distinctAt(ra.positions, bi),
			})
		}
	}
	for _, m := range baseMeasures {
		qry.Measures = append(qry.Measures, m.Name)
	}

	st, err := exec.Plan(cube, qry)
	if err != nil {
		return nil, err
	}
	res, err := exec.Run(ctx, cube, st)
	if err != nil {
		return nil, err
	}

	return pivot(q, axes, ranges, slots, baseMeasures, measureAxis, res, reg), nil
}

// resolveAxis resolve uma expressão de eixo num resolvedAxis.
func resolveAxis(ctx context.Context, cube *metadata.Cube, exp mdx.Exp, ordinal int, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) (resolvedAxis, error) {
	if isMeasuresExp(exp) {
		var slots []measureSlot
		for _, id := range extractMeasureIds(exp) {
			s, err := resolveMeasureSlot(cube, id, reg)
			if err != nil {
				return resolvedAxis{}, err
			}
			slots = append(slots, s)
		}
		return resolvedAxis{ordinal: ordinal, isMeasures: true, slots: slots}, nil
	}

	if fc, ok := exp.(*mdx.FunCall); ok && fc.Syntax == mdx.SyntaxFunction {
		switch strings.ToUpper(fc.Name) {
		case "ORDER":
			return resolveOrder(ctx, cube, fc, ordinal, slicer, exec, reg)
		case "TOPCOUNT":
			return resolveTopBottom(ctx, cube, fc, ordinal, slicer, exec, reg, true)
		case "BOTTOMCOUNT":
			return resolveTopBottom(ctx, cube, fc, ordinal, slicer, exec, reg, false)
		case "FILTER":
			return resolveFilter(ctx, cube, fc, ordinal, slicer, exec, reg)
		}
	}

	plan, err := analyzeAxis(cube, exp)
	if err != nil {
		return resolvedAxis{}, err
	}
	if plan.isMeasures {
		return resolvedAxis{ordinal: ordinal, isMeasures: true}, nil
	}
	positions, err := enumerate(ctx, cube, plan.bindings, slicer, nil, exec)
	if err != nil {
		return resolvedAxis{}, err
	}
	return resolvedAxis{ordinal: ordinal, bindings: plan.bindings, positions: positions}, nil
}

func resolveOrder(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, ordinal int, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) (resolvedAxis, error) {
	if len(fc.Args) < 2 {
		return resolvedAxis{}, fmt.Errorf("Order espera (conjunto, expressão [, ASC|DESC|BASC|BDESC])")
	}
	plan, err := innerBindings(cube, fc.Args[0])
	if err != nil {
		return resolvedAxis{}, err
	}
	valueExp := fc.Args[1]
	desc := false
	if len(fc.Args) >= 3 {
		if id, ok := fc.Args[2].(*mdx.Id); ok {
			desc = strings.Contains(strings.ToUpper(id.String()), "DESC")
		}
	}
	positions, err := enumerateForExpr(ctx, cube, plan, valueExp, slicer, exec, reg)
	if err != nil {
		return resolvedAxis{}, err
	}
	sortByExpr(positions, valueExp, reg, desc)
	return resolvedAxis{ordinal: ordinal, bindings: plan, positions: positions}, nil
}

func resolveTopBottom(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, ordinal int, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry, top bool) (resolvedAxis, error) {
	name := "TopCount"
	if !top {
		name = "BottomCount"
	}
	if len(fc.Args) < 2 {
		return resolvedAxis{}, fmt.Errorf("%s espera (conjunto, n [, expressão])", name)
	}
	plan, err := innerBindings(cube, fc.Args[0])
	if err != nil {
		return resolvedAxis{}, err
	}
	nlit, ok := fc.Args[1].(*mdx.NumericLiteral)
	if !ok {
		return resolvedAxis{}, fmt.Errorf("%s: 2º argumento deve ser um número", name)
	}
	n := int(nlit.Value)
	var valueExp mdx.Exp
	if len(fc.Args) >= 3 {
		valueExp = fc.Args[2]
	}
	positions, err := enumerateForExpr(ctx, cube, plan, valueExp, slicer, exec, reg)
	if err != nil {
		return resolvedAxis{}, err
	}
	if valueExp != nil {
		sortByExpr(positions, valueExp, reg, top) // top => desc
	}
	if n >= 0 && n < len(positions) {
		positions = positions[:n]
	}
	return resolvedAxis{ordinal: ordinal, bindings: plan, positions: positions}, nil
}

func resolveFilter(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, ordinal int, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) (resolvedAxis, error) {
	if len(fc.Args) != 2 {
		return resolvedAxis{}, fmt.Errorf("Filter espera (conjunto, condição)")
	}
	plan, err := innerBindings(cube, fc.Args[0])
	if err != nil {
		return resolvedAxis{}, err
	}
	cond := fc.Args[1]
	positions, err := enumerateForExpr(ctx, cube, plan, cond, slicer, exec, reg)
	if err != nil {
		return resolvedAxis{}, err
	}
	kept := positions[:0]
	for _, p := range positions {
		if evalBool(cond, p.env, reg) {
			kept = append(kept, p)
		}
	}
	return resolvedAxis{ordinal: ordinal, bindings: plan, positions: kept}, nil
}

// innerBindings extrai os bindings do conjunto interno (deve ser um conjunto de
// membros, não medidas).
func innerBindings(cube *metadata.Cube, exp mdx.Exp) ([]levelBinding, error) {
	plan, err := analyzeAxis(cube, exp)
	if err != nil {
		return nil, err
	}
	if plan.isMeasures {
		return nil, fmt.Errorf("esperava um conjunto de membros, não de medidas")
	}
	return plan.bindings, nil
}

// enumerateForExpr enumera posições buscando as medidas-base de exp (para Order/
// TopCount/Filter avaliarem por posição).
func enumerateForExpr(ctx context.Context, cube *metadata.Cube, bindings []levelBinding, exp mdx.Exp, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) ([]position, error) {
	into := map[string]*metadata.Measure{}
	var valueMeasures []*metadata.Measure
	if exp != nil {
		collectBaseMeasures(exp, cube, reg, into, &valueMeasures)
	}
	return enumerate(ctx, cube, bindings, slicer, valueMeasures, exec)
}

// enumerate roda uma query agrupada pelos níveis dos bindings (+ filtros do
// binding + slicer), opcionalmente buscando valueMeasures por posição.
func enumerate(ctx context.Context, cube *metadata.Cube, bindings []levelBinding, slicer []query.Filter, valueMeasures []*metadata.Measure, exec *queryexec.Service) ([]position, error) {
	qry := query.Query{Cube: cube.Name}
	for _, b := range bindings {
		qry.Rows = append(qry.Rows, b.ref)
		qry.Filters = append(qry.Filters, b.filters...)
	}
	qry.Filters = append(qry.Filters, slicer...)
	for _, m := range valueMeasures {
		qry.Measures = append(qry.Measures, m.Name)
	}
	st, err := exec.Plan(cube, qry)
	if err != nil {
		return nil, err
	}
	res, err := exec.Run(ctx, cube, st)
	if err != nil {
		return nil, err
	}
	levelCount := len(bindings)
	out := make([]position, 0, len(res.Rows))
	for _, row := range res.Rows {
		vals := make([]string, levelCount)
		for c := 0; c < levelCount; c++ {
			vals[c] = fmt.Sprint(row[c].Value)
		}
		var env map[string]float64
		if len(valueMeasures) > 0 {
			env = make(map[string]float64, len(valueMeasures))
			for k, m := range valueMeasures {
				f, _ := toFloat(row[levelCount+k].Value)
				env[strings.ToLower(m.Name)] = f
			}
		}
		out = append(out, position{values: vals, env: env})
	}
	return out, nil
}

func sortByExpr(positions []position, exp mdx.Exp, reg calcRegistry, desc bool) {
	sort.SliceStable(positions, func(i, j int) bool {
		vi, oki := evalNumeric(exp, positions[i].env, reg)
		vj, okj := evalNumeric(exp, positions[j].env, reg)
		if !oki {
			return false
		}
		if !okj {
			return true
		}
		if desc {
			return vi > vj
		}
		return vi < vj
	})
}

func defaultMeasure(cube *metadata.Cube, slicerMeasure *metadata.Measure) (*metadata.Measure, error) {
	if slicerMeasure != nil {
		return slicerMeasure, nil
	}
	if cube.DefaultMeasure != "" {
		if m, ok := cube.FindMeasure(cube.DefaultMeasure); ok {
			return m, nil
		}
	}
	if len(cube.Measures) > 0 {
		return cube.Measures[0], nil
	}
	return nil, fmt.Errorf("cubo %q sem medidas", cube.Name)
}

func distinctAt(positions []position, bindingIdx int) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range positions {
		if bindingIdx >= len(p.values) {
			continue
		}
		v := p.values[bindingIdx]
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// pivot monta o CellSet a partir das posições explícitas dos eixos e dos records.
func pivot(q *mdx.Query, axes []resolvedAxis, ranges [][2]int, slots []measureSlot, baseMeasures []*metadata.Measure, measureAxis int, res *query.Result, reg calcRegistry) *CellSet {
	levelColCount := 0
	for _, r := range ranges {
		if r[1] > levelColCount {
			levelColCount = r[1]
		}
	}

	cs := &CellSet{SQL: res.SQL}
	cs.Axes = make([]Axis, len(axes))
	posMap := make([]map[string]int, len(axes))
	for ai, ra := range axes {
		cs.Axes[ai] = Axis{Ordinal: ra.ordinal, Name: mdx.AxisName(ra.ordinal)}
		if ra.isMeasures {
			for _, s := range ra.slots {
				cs.Axes[ai].Positions = append(cs.Axes[ai].Positions, Position{
					Members: []Member{{Caption: s.caption, UniqueName: s.uniqueName}},
				})
			}
			continue
		}
		posMap[ai] = make(map[string]int, len(ra.positions))
		for pi, p := range ra.positions {
			posMap[ai][strings.Join(p.values, "\x1f")] = pi
			members := make([]Member, len(p.values))
			for bi, v := range p.values {
				members[bi] = Member{
					Caption:    v,
					UniqueName: fmt.Sprintf("[%s].[%s].[%s]", ra.bindings[bi].ref.Dimension, ra.bindings[bi].ref.Level, v),
				}
			}
			cs.Axes[ai].Positions = append(cs.Axes[ai].Positions, Position{Members: members})
		}
	}

	for _, row := range res.Rows {
		coords := make([]int, len(axes))
		skip := false
		for ai, ra := range axes {
			if ra.isMeasures {
				continue
			}
			vals := make([]string, 0, ranges[ai][1]-ranges[ai][0])
			for c := ranges[ai][0]; c < ranges[ai][1]; c++ {
				vals = append(vals, fmt.Sprint(row[c].Value))
			}
			idx, ok := posMap[ai][strings.Join(vals, "\x1f")]
			if !ok {
				skip = true
				break
			}
			coords[ai] = idx
		}
		if skip {
			continue
		}

		env := make(map[string]float64, len(baseMeasures))
		for k, m := range baseMeasures {
			f, _ := toFloat(row[levelColCount+k].Value)
			env[strings.ToLower(m.Name)] = f
		}

		emit := func(coords []int, s measureSlot) {
			v, ok := slotValue(s, env, reg)
			c := Cell{Coords: append([]int(nil), coords...)}
			if ok {
				c.Value = v
				c.Formatted = formatNumber(v)
			}
			cs.Cells = append(cs.Cells, c)
		}

		if measureAxis >= 0 {
			for si, s := range slots {
				coords[measureAxis] = si
				emit(coords, s)
			}
		} else {
			emit(coords, slots[0])
		}
	}

	if len(cs.Axes) == 2 {
		cs.Grid = buildGrid(cs)
	}
	return cs
}

func slotValue(s measureSlot, env map[string]float64, reg calcRegistry) (float64, bool) {
	if s.isCalc {
		return evalNumeric(s.exp, env, reg)
	}
	v, ok := env[strings.ToLower(s.name)]
	return v, ok
}

// buildGrid renderiza o CellSet de 2 eixos como tabela (colunas = ordinal 0).
func buildGrid(cs *CellSet) *Grid {
	colAxis, rowAxis := axisByOrdinal(cs, 0), axisByOrdinal(cs, 1)
	if colAxis < 0 || rowAxis < 0 {
		return nil
	}
	g := &Grid{}
	for _, p := range cs.Axes[colAxis].Positions {
		g.ColumnHeaders = append(g.ColumnHeaders, positionLabel(p))
	}
	for _, p := range cs.Axes[rowAxis].Positions {
		g.RowHeaders = append(g.RowHeaders, positionLabel(p))
	}
	g.Rows = make([][]string, len(cs.Axes[rowAxis].Positions))
	for r := range g.Rows {
		g.Rows[r] = make([]string, len(cs.Axes[colAxis].Positions))
	}
	for _, cell := range cs.Cells {
		g.Rows[cell.Coords[rowAxis]][cell.Coords[colAxis]] = cell.Formatted
	}
	return g
}

func axisByOrdinal(cs *CellSet, ord int) int {
	for i, a := range cs.Axes {
		if a.Ordinal == ord {
			return i
		}
	}
	return -1
}

func positionLabel(p Position) string {
	parts := make([]string, len(p.Members))
	for i, m := range p.Members {
		parts[i] = m.Caption
	}
	return strings.Join(parts, " / ")
}
