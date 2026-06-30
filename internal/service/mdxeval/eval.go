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

	sets := buildNamedSets(q)
	axes := make([]resolvedAxis, len(q.Axes))
	measureAxis := -1
	for i, ax := range q.Axes {
		ra, err := resolveAxis(ctx, cube, expandSets(ax.Exp, sets, 0), ax.Ordinal, slicerFilters, exec, reg)
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
				Hierarchy: ra.bindings[bi].ref.Hierarchy,
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

	aggLookups, err := buildAggLookups(ctx, cube, slots, qry.Rows, slicerFilters, exec, reg)
	if err != nil {
		return nil, err
	}

	return pivot(q, axes, ranges, slots, baseMeasures, measureAxis, res, reg, aggLookups), nil
}

// aggLookup guarda, para um nó de agregação sobre conjunto, os valores
// pré-computados indexados pela tupla das dimensões do grid fora do conjunto.
type aggLookup struct {
	key       string // = nó.String() (chave no env das células)
	otherCols []int  // colunas de nível do grid usadas como chave
	values    map[string]float64
}

// buildAggLookups pré-computa os nós Sum/Avg/Count/Aggregate dos calc members.
func buildAggLookups(ctx context.Context, cube *metadata.Cube, slots []measureSlot, gridRows []query.LevelRef, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) ([]aggLookup, error) {
	var nodes []*mdx.FunCall
	seen := map[string]bool{}
	for _, s := range slots {
		if s.isCalc {
			collectSetAggNodes(s.exp, &nodes, seen)
		}
	}
	var out []aggLookup
	for _, node := range nodes {
		lu, err := computeAgg(ctx, cube, node, gridRows, slicer, exec, reg)
		if err != nil {
			return nil, err
		}
		out = append(out, lu)
	}
	return out, nil
}

func computeAgg(ctx context.Context, cube *metadata.Cube, node *mdx.FunCall, gridRows []query.LevelRef, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) (aggLookup, error) {
	name := strings.ToUpper(node.Name)
	setBindings, err := innerBindings(cube, node.Args[0])
	if err != nil {
		return aggLookup{}, fmt.Errorf("%s: %w", name, err)
	}
	setDims := map[string]bool{}
	for _, b := range setBindings {
		setDims[b.ref.Dimension] = true
	}
	// Dimensões do grid que NÃO pertencem ao conjunto (contexto do subtotal).
	var otherRefs []query.LevelRef
	var otherCols []int
	for i, r := range gridRows {
		if !setDims[r.Dimension] {
			otherRefs = append(otherRefs, r)
			otherCols = append(otherCols, i)
		}
	}

	memberPos, err := enumerate(ctx, cube, setBindings, slicer, nil, exec)
	if err != nil {
		return aggLookup{}, err
	}
	memberCount := float64(len(memberPos))

	lu := aggLookup{key: node.String(), otherCols: otherCols, values: map[string]float64{}}
	if name == "COUNT" {
		lu.otherCols = nil
		lu.values[""] = memberCount
		return lu, nil
	}
	if len(node.Args) < 2 {
		return aggLookup{}, fmt.Errorf("%s espera (conjunto, expressão)", name)
	}
	valueExp := node.Args[1]
	into := map[string]*metadata.Measure{}
	var valBase []*metadata.Measure
	collectBaseMeasures(valueExp, cube, reg, into, &valBase)

	// Min/Max sobre conjunto: precisa do valor POR MEMBRO (agrupa também pelos
	// níveis do conjunto) e então tira o min/max em Go por contexto.
	if name == "MIN" || name == "MAX" {
		return computeMinMax(ctx, cube, node, name, setBindings, otherRefs, otherCols, valueExp, valBase, slicer, exec, reg)
	}

	qry := query.Query{Cube: cube.Name}
	qry.Rows = append(qry.Rows, otherRefs...)
	for _, b := range setBindings {
		qry.Filters = append(qry.Filters, b.filters...)
	}
	qry.Filters = append(qry.Filters, slicer...)
	for _, m := range valBase {
		qry.Measures = append(qry.Measures, m.Name)
	}
	st, err := exec.Plan(cube, qry)
	if err != nil {
		return aggLookup{}, err
	}
	res, err := exec.Run(ctx, cube, st)
	if err != nil {
		return aggLookup{}, err
	}
	oc := len(otherRefs)
	for _, row := range res.Rows {
		parts := make([]string, oc)
		for i := 0; i < oc; i++ {
			parts[i] = fmt.Sprint(row[i].Value)
		}
		env := make(map[string]float64, len(valBase))
		for k, m := range valBase {
			f, _ := toFloat(row[oc+k].Value)
			env[strings.ToLower(m.Name)] = f
		}
		v, ok := evalNumeric(valueExp, env, reg)
		if !ok {
			continue
		}
		if name == "AVG" {
			if memberCount == 0 {
				continue
			}
			v /= memberCount
		}
		lu.values[strings.Join(parts, "\x1f")] = v
	}
	return lu, nil
}

// computeMinMax calcula Min/Max de uma expressão sobre os membros de um conjunto,
// por contexto (demais dimensões do grid). Agrupa por outras-dims + níveis do
// conjunto para obter o valor por membro, e reduz com min/max em Go.
func computeMinMax(ctx context.Context, cube *metadata.Cube, node *mdx.FunCall, name string,
	setBindings []levelBinding, otherRefs []query.LevelRef, otherCols []int,
	valueExp mdx.Exp, valBase []*metadata.Measure, slicer []query.Filter,
	exec *queryexec.Service, reg calcRegistry) (aggLookup, error) {

	qry := query.Query{Cube: cube.Name}
	qry.Rows = append(qry.Rows, otherRefs...)
	for _, b := range setBindings {
		qry.Rows = append(qry.Rows, b.ref)
		qry.Filters = append(qry.Filters, b.filters...)
	}
	qry.Filters = append(qry.Filters, slicer...)
	for _, m := range valBase {
		qry.Measures = append(qry.Measures, m.Name)
	}
	st, err := exec.Plan(cube, qry)
	if err != nil {
		return aggLookup{}, err
	}
	res, err := exec.Run(ctx, cube, st)
	if err != nil {
		return aggLookup{}, err
	}

	nOther := len(otherRefs)
	measStart := nOther + len(setBindings)
	lu := aggLookup{key: node.String(), otherCols: otherCols, values: map[string]float64{}}
	for _, row := range res.Rows {
		parts := make([]string, nOther)
		for i := 0; i < nOther; i++ {
			parts[i] = fmt.Sprint(row[i].Value)
		}
		env := make(map[string]float64, len(valBase))
		for k, m := range valBase {
			f, _ := toFloat(row[measStart+k].Value)
			env[strings.ToLower(m.Name)] = f
		}
		v, ok := evalNumeric(valueExp, env, reg)
		if !ok {
			continue
		}
		key := strings.Join(parts, "\x1f")
		cur, exists := lu.values[key]
		if !exists || (name == "MIN" && v < cur) || (name == "MAX" && v > cur) {
			lu.values[key] = v
		}
	}
	return lu, nil
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

	bindings, positions, err := resolveMemberSet(ctx, cube, exp, slicer, exec, reg)
	if err != nil {
		return resolvedAxis{}, err
	}
	return resolvedAxis{ordinal: ordinal, bindings: bindings, positions: positions}, nil
}

// resolveMemberSet resolve uma expressão de conjunto de membros para
// (bindings, posições), tratando as funções de conjunto recursivamente (o que
// permite compô-las, ex.: Head(Order(...), 5)).
func resolveMemberSet(ctx context.Context, cube *metadata.Cube, exp mdx.Exp, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) ([]levelBinding, []position, error) {
	// Conjunto de 1 elemento {x} é equivalente a x — desembrulha para que funções
	// (tempo, conjunto) dentro das chaves sejam reconhecidas.
	if fc, ok := exp.(*mdx.FunCall); ok && fc.Syntax == mdx.SyntaxBraces && len(fc.Args) == 1 {
		return resolveMemberSet(ctx, cube, fc.Args[0], slicer, exec, reg)
	}

	// Inteligência de tempo: [m].PrevMember/.NextMember, .Lag(n)/.Lead(n), YTD(m).
	switch e := exp.(type) {
	case *mdx.Id:
		if u := strings.ToUpper(lastSeg(e)); (u == "PREVMEMBER" || u == "NEXTMEMBER") && len(e.Segments) >= 2 {
			shift := -1
			if u == "NEXTMEMBER" {
				shift = 1
			}
			return timeShift(ctx, cube, exec, &mdx.Id{Segments: e.Segments[:len(e.Segments)-1]}, shift)
		}
	case *mdx.FunCall:
		if e.Syntax == mdx.SyntaxMethod && len(e.Args) >= 2 {
			u := strings.ToUpper(e.Name)
			if u == "LAG" || u == "LEAD" {
				base, ok1 := e.Args[0].(*mdx.Id)
				nlit, ok2 := e.Args[1].(*mdx.NumericLiteral)
				if ok1 && ok2 {
					n := int(nlit.Value)
					if u == "LAG" {
						n = -n
					}
					return timeShift(ctx, cube, exec, base, n)
				}
			}
		}
		if e.Syntax == mdx.SyntaxFunction && strings.EqualFold(e.Name, "YTD") && len(e.Args) >= 1 {
			if base, ok := e.Args[0].(*mdx.Id); ok {
				return ytd(ctx, cube, exec, base)
			}
		}
		if e.Syntax == mdx.SyntaxInfix && e.Name == ":" && len(e.Args) == 2 {
			a, ok1 := e.Args[0].(*mdx.Id)
			b, ok2 := e.Args[1].(*mdx.Id)
			if ok1 && ok2 {
				return rangeSet(ctx, cube, exec, a, b)
			}
		}
	}

	if fc, ok := exp.(*mdx.FunCall); ok && fc.Syntax == mdx.SyntaxFunction {
		switch strings.ToUpper(fc.Name) {
		case "ORDER":
			return setOrder(ctx, cube, fc, slicer, exec, reg)
		case "TOPCOUNT":
			return setTopBottom(ctx, cube, fc, slicer, exec, reg, true)
		case "BOTTOMCOUNT":
			return setTopBottom(ctx, cube, fc, slicer, exec, reg, false)
		case "FILTER":
			return setFilter(ctx, cube, fc, slicer, exec, reg)
		case "UNION":
			return setBinaryOp(ctx, cube, fc, slicer, exec, reg, "union")
		case "EXCEPT":
			return setBinaryOp(ctx, cube, fc, slicer, exec, reg, "except")
		case "INTERSECT":
			return setBinaryOp(ctx, cube, fc, slicer, exec, reg, "intersect")
		case "HEAD":
			return setHeadTail(ctx, cube, fc, slicer, exec, reg, true)
		case "TAIL":
			return setHeadTail(ctx, cube, fc, slicer, exec, reg, false)
		case "DISTINCT":
			return setUnary(ctx, cube, fc, slicer, exec, reg, "distinct")
		case "HIERARCHIZE":
			return setUnary(ctx, cube, fc, slicer, exec, reg, "hierarchize")
		}
	}

	plan, err := analyzeAxis(cube, exp)
	if err != nil {
		return nil, nil, err
	}
	if plan.isMeasures {
		return nil, nil, fmt.Errorf("esperava um conjunto de membros, não de medidas")
	}

	// Caso comum (1 binding): enumera TODOS os membros via tabela de dimensão
	// (semântica "mostrar todos"; NON EMPTY poda os vazios depois). CrossJoin
	// (múltiplos bindings) permanece via fato por ora.
	if len(plan.bindings) == 1 {
		members, err := exec.EnumerateLevel(ctx, cube, plan.bindings[0].ref, plan.bindings[0].filters)
		if err != nil {
			return nil, nil, err
		}
		positions := make([]position, len(members))
		for i, m := range members {
			positions[i] = position{values: []string{m}}
		}
		return plan.bindings, positions, nil
	}

	positions, err := enumerate(ctx, cube, plan.bindings, slicer, nil, exec)
	if err != nil {
		return nil, nil, err
	}
	return plan.bindings, positions, nil
}

func setOrder(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) ([]levelBinding, []position, error) {
	if len(fc.Args) < 2 {
		return nil, nil, fmt.Errorf("Order espera (conjunto, expressão [, ASC|DESC|BASC|BDESC])")
	}
	bindings, err := innerBindings(cube, fc.Args[0])
	if err != nil {
		return nil, nil, err
	}
	valueExp := fc.Args[1]
	desc := false
	if len(fc.Args) >= 3 {
		if id, ok := fc.Args[2].(*mdx.Id); ok {
			desc = strings.Contains(strings.ToUpper(id.String()), "DESC")
		}
	}
	positions, err := enumerateForExpr(ctx, cube, bindings, valueExp, slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	sortByExpr(positions, valueExp, reg, desc)
	return bindings, positions, nil
}

func setTopBottom(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry, top bool) ([]levelBinding, []position, error) {
	name := "TopCount"
	if !top {
		name = "BottomCount"
	}
	if len(fc.Args) < 2 {
		return nil, nil, fmt.Errorf("%s espera (conjunto, n [, expressão])", name)
	}
	bindings, err := innerBindings(cube, fc.Args[0])
	if err != nil {
		return nil, nil, err
	}
	nlit, ok := fc.Args[1].(*mdx.NumericLiteral)
	if !ok {
		return nil, nil, fmt.Errorf("%s: 2º argumento deve ser um número", name)
	}
	n := int(nlit.Value)
	var valueExp mdx.Exp
	if len(fc.Args) >= 3 {
		valueExp = fc.Args[2]
	}
	positions, err := enumerateForExpr(ctx, cube, bindings, valueExp, slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	if valueExp != nil {
		sortByExpr(positions, valueExp, reg, top) // top => desc
	}
	if n >= 0 && n < len(positions) {
		positions = positions[:n]
	}
	return bindings, positions, nil
}

func setFilter(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry) ([]levelBinding, []position, error) {
	if len(fc.Args) != 2 {
		return nil, nil, fmt.Errorf("Filter espera (conjunto, condição)")
	}
	bindings, err := innerBindings(cube, fc.Args[0])
	if err != nil {
		return nil, nil, err
	}
	cond := fc.Args[1]
	positions, err := enumerateForExpr(ctx, cube, bindings, cond, slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	kept := positions[:0]
	for _, p := range positions {
		if evalBool(cond, p.env, reg) {
			kept = append(kept, p)
		}
	}
	return bindings, kept, nil
}

// setBinaryOp implementa Union/Except/Intersect entre dois conjuntos de mesmos níveis.
func setBinaryOp(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry, op string) ([]levelBinding, []position, error) {
	if len(fc.Args) != 2 {
		return nil, nil, fmt.Errorf("%s espera 2 conjuntos", op)
	}
	ba, pa, err := resolveMemberSet(ctx, cube, fc.Args[0], slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	bb, pb, err := resolveMemberSet(ctx, cube, fc.Args[1], slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	if !bindingsMatch(ba, bb) {
		return nil, nil, fmt.Errorf("%s exige conjuntos com os mesmos níveis", op)
	}
	inB := map[string]bool{}
	for _, p := range pb {
		inB[posKeyOf(p)] = true
	}
	var out []position
	seen := map[string]bool{}
	add := func(p position) {
		k := posKeyOf(p)
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, p)
	}
	switch op {
	case "union":
		for _, p := range pa {
			add(p)
		}
		for _, p := range pb {
			add(p)
		}
	case "except":
		for _, p := range pa {
			if !inB[posKeyOf(p)] {
				add(p)
			}
		}
	case "intersect":
		for _, p := range pa {
			if inB[posKeyOf(p)] {
				add(p)
			}
		}
	}
	return ba, out, nil
}

func setHeadTail(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry, head bool) ([]levelBinding, []position, error) {
	if len(fc.Args) < 1 {
		return nil, nil, fmt.Errorf("Head/Tail espera (conjunto [, n])")
	}
	bindings, positions, err := resolveMemberSet(ctx, cube, fc.Args[0], slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	n := 1
	if len(fc.Args) >= 2 {
		if nlit, ok := fc.Args[1].(*mdx.NumericLiteral); ok {
			n = int(nlit.Value)
		}
	}
	if n < 0 {
		n = 0
	}
	if n > len(positions) {
		n = len(positions)
	}
	if head {
		positions = positions[:n]
	} else {
		positions = positions[len(positions)-n:]
	}
	return bindings, positions, nil
}

func setUnary(ctx context.Context, cube *metadata.Cube, fc *mdx.FunCall, slicer []query.Filter, exec *queryexec.Service, reg calcRegistry, op string) ([]levelBinding, []position, error) {
	if len(fc.Args) < 1 {
		return nil, nil, fmt.Errorf("%s espera (conjunto)", op)
	}
	bindings, positions, err := resolveMemberSet(ctx, cube, fc.Args[0], slicer, exec, reg)
	if err != nil {
		return nil, nil, err
	}
	switch op {
	case "distinct":
		seen := map[string]bool{}
		out := positions[:0]
		for _, p := range positions {
			k := posKeyOf(p)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, p)
		}
		positions = out
	case "hierarchize":
		sort.SliceStable(positions, func(i, j int) bool {
			return posKeyOf(positions[i]) < posKeyOf(positions[j])
		})
	}
	return bindings, positions, nil
}

func bindingsMatch(a, b []levelBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ref.Dimension != b[i].ref.Dimension || a[i].ref.Level != b[i].ref.Level {
			return false
		}
	}
	return true
}

func posKeyOf(p position) string {
	return strings.Join(p.values, "\x1f")
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
func pivot(q *mdx.Query, axes []resolvedAxis, ranges [][2]int, slots []measureSlot, baseMeasures []*metadata.Measure, measureAxis int, res *query.Result, reg calcRegistry, aggLookups []aggLookup) *CellSet {
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

		env := make(map[string]float64, len(baseMeasures)+len(aggLookups))
		for k, m := range baseMeasures {
			f, _ := toFloat(row[levelColCount+k].Value)
			env[strings.ToLower(m.Name)] = f
		}
		// Injeta os subtotais pré-computados (Sum/Avg/Count sobre conjuntos).
		for _, lu := range aggLookups {
			key := ""
			if len(lu.otherCols) > 0 {
				parts := make([]string, len(lu.otherCols))
				for i, c := range lu.otherCols {
					parts[i] = fmt.Sprint(row[c].Value)
				}
				key = strings.Join(parts, "\x1f")
			}
			if v, ok := lu.values[key]; ok {
				env[lu.key] = v
			}
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

	pruneNonEmpty(cs, q)

	if len(cs.Axes) == 2 {
		cs.Grid = buildGrid(cs)
	}
	return cs
}

// pruneNonEmpty remove, dos eixos marcados NON EMPTY, as posições cujas células
// são todas vazias (Value nil), reindexando as células.
func pruneNonEmpty(cs *CellSet, q *mdx.Query) {
	nonEmpty := map[int]bool{}
	for i, ax := range q.Axes {
		if ax.NonEmpty && i < len(cs.Axes) {
			nonEmpty[i] = true
		}
	}
	if len(nonEmpty) == 0 {
		return
	}
	alive := map[int]map[int]bool{}
	for a := range nonEmpty {
		alive[a] = map[int]bool{}
	}
	for _, c := range cs.Cells {
		if c.Value == nil {
			continue
		}
		for a := range nonEmpty {
			alive[a][c.Coords[a]] = true
		}
	}
	remap := map[int]map[int]int{}
	for a := range nonEmpty {
		rm := map[int]int{}
		newPos := make([]Position, 0, len(cs.Axes[a].Positions))
		for pi, p := range cs.Axes[a].Positions {
			if alive[a][pi] {
				rm[pi] = len(newPos)
				newPos = append(newPos, p)
			}
		}
		remap[a] = rm
		cs.Axes[a].Positions = newPos
	}
	kept := cs.Cells[:0]
	for _, c := range cs.Cells {
		drop := false
		for a := range nonEmpty {
			ni, ok := remap[a][c.Coords[a]]
			if !ok {
				drop = true
				break
			}
			c.Coords[a] = ni
		}
		if !drop {
			kept = append(kept, c)
		}
	}
	cs.Cells = kept
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
