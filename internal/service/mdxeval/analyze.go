package mdxeval

import (
	"fmt"
	"strings"

	"cubodw/internal/engine/mdx"
	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
)

// axisPlan é o resultado da análise estática de uma expressão de eixo: ou um
// conjunto de medidas, ou um conjunto de "bindings" de nível (cada um define um
// nível de agrupamento e os filtros que impõe).
type axisPlan struct {
	isMeasures bool
	measures   []*metadata.Measure
	bindings   []levelBinding
}

// levelBinding liga um nível de agrupamento aos filtros que a expressão impõe
// sobre ele (restrição de membros e/ou filtros de ancestrais).
type levelBinding struct {
	ref     query.LevelRef
	filters []query.Filter
}

// memberRef é um membro resolvido contra a hierarquia.
type memberRef struct {
	dim        *metadata.Dimension
	hier       *metadata.Hierarchy
	levelIndex int            // nível do próprio membro
	values     map[int]string // levelIndex -> valor (membro + ancestrais)
}

func isMeasureId(id *mdx.Id) bool {
	return len(id.Segments) >= 1 && strings.EqualFold(id.Segments[0].Name, metadata.MeasuresDimension)
}

func lastSeg(id *mdx.Id) string {
	return id.Segments[len(id.Segments)-1].Name
}

// analyzeAxis reduz uma expressão de eixo a um axisPlan.
func analyzeAxis(cube *metadata.Cube, exp mdx.Exp) (axisPlan, error) {
	switch e := exp.(type) {
	case *mdx.Id:
		return analyzeIdAxis(cube, e)
	case *mdx.FunCall:
		switch {
		case e.Syntax == mdx.SyntaxBraces:
			return analyzeBraces(cube, e)
		case e.Syntax == mdx.SyntaxParentheses:
			return analyzeTuple(cube, e)
		case e.Syntax == mdx.SyntaxFunction && strings.EqualFold(e.Name, "CrossJoin"):
			return analyzeCrossJoin(cube, e)
		default:
			return axisPlan{}, fmt.Errorf("função/expressão %q ainda não suportada na avaliação", e.Name)
		}
	default:
		return axisPlan{}, fmt.Errorf("expressão de eixo não suportada: %T", exp)
	}
}

func analyzeIdAxis(cube *metadata.Cube, id *mdx.Id) (axisPlan, error) {
	if isMeasureId(id) {
		m, err := resolveMeasure(cube, id)
		if err != nil {
			return axisPlan{}, err
		}
		return axisPlan{isMeasures: true, measures: []*metadata.Measure{m}}, nil
	}
	switch {
	case strings.EqualFold(lastSeg(id), "Members"):
		b, err := levelMembersBinding(cube, id)
		if err != nil {
			return axisPlan{}, err
		}
		return axisPlan{bindings: []levelBinding{b}}, nil
	case strings.EqualFold(lastSeg(id), "Children"):
		b, err := childrenBinding(cube, id)
		if err != nil {
			return axisPlan{}, err
		}
		return axisPlan{bindings: []levelBinding{b}}, nil
	default:
		ref, err := resolveMemberId(cube, id)
		if err != nil {
			return axisPlan{}, err
		}
		return axisPlan{bindings: []levelBinding{memberBinding(ref, []*memberRef{ref})}}, nil
	}
}

func analyzeBraces(cube *metadata.Cube, fc *mdx.FunCall) (axisPlan, error) {
	if len(fc.Args) == 0 {
		return axisPlan{}, fmt.Errorf("conjunto vazio {} não suportado")
	}
	// Se o único elemento for uma sub-expressão de conjunto, desembrulha.
	if len(fc.Args) == 1 {
		if _, ok := fc.Args[0].(*mdx.FunCall); ok {
			return analyzeAxis(cube, fc.Args[0])
		}
		if id, ok := fc.Args[0].(*mdx.Id); ok {
			if isMeasureId(id) || strings.EqualFold(lastSeg(id), "Members") || strings.EqualFold(lastSeg(id), "Children") {
				return analyzeAxis(cube, id)
			}
		}
	}

	// Caso geral: todos medidas, ou todos membros concretos do mesmo nível.
	allMeasures := true
	for _, a := range fc.Args {
		id, ok := a.(*mdx.Id)
		if !ok || !isMeasureId(id) {
			allMeasures = false
			break
		}
	}
	if allMeasures {
		var ms []*metadata.Measure
		for _, a := range fc.Args {
			m, err := resolveMeasure(cube, a.(*mdx.Id))
			if err != nil {
				return axisPlan{}, err
			}
			ms = append(ms, m)
		}
		return axisPlan{isMeasures: true, measures: ms}, nil
	}

	// Membros concretos do mesmo nível.
	var refs []*memberRef
	for _, a := range fc.Args {
		id, ok := a.(*mdx.Id)
		if !ok {
			return axisPlan{}, fmt.Errorf("conjunto misto não suportado nesta fase")
		}
		ref, err := resolveMemberId(cube, id)
		if err != nil {
			return axisPlan{}, err
		}
		if len(refs) > 0 && (refs[0].dim != ref.dim || refs[0].levelIndex != ref.levelIndex) {
			return axisPlan{}, fmt.Errorf("conjunto com membros de níveis diferentes não suportado nesta fase")
		}
		refs = append(refs, ref)
	}
	return axisPlan{bindings: []levelBinding{memberBinding(refs[0], refs)}}, nil
}

func analyzeTuple(cube *metadata.Cube, fc *mdx.FunCall) (axisPlan, error) {
	var bindings []levelBinding
	for _, a := range fc.Args {
		id, ok := a.(*mdx.Id)
		if !ok || isMeasureId(id) {
			return axisPlan{}, fmt.Errorf("tupla com expressão não suportada nesta fase")
		}
		ref, err := resolveMemberId(cube, id)
		if err != nil {
			return axisPlan{}, err
		}
		bindings = append(bindings, memberBinding(ref, []*memberRef{ref}))
	}
	return axisPlan{bindings: bindings}, nil
}

func analyzeCrossJoin(cube *metadata.Cube, fc *mdx.FunCall) (axisPlan, error) {
	if len(fc.Args) != 2 {
		return axisPlan{}, fmt.Errorf("CrossJoin espera 2 argumentos")
	}
	var out axisPlan
	for _, a := range fc.Args {
		p, err := analyzeAxis(cube, a)
		if err != nil {
			return axisPlan{}, err
		}
		if p.isMeasures {
			return axisPlan{}, fmt.Errorf("CrossJoin com medidas ainda não suportado")
		}
		out.bindings = append(out.bindings, p.bindings...)
	}
	return out, nil
}

// --- resolução -----------------------------------------------------------

func resolveMeasure(cube *metadata.Cube, id *mdx.Id) (*metadata.Measure, error) {
	name := lastSeg(id)
	if m, ok := cube.FindMeasure(name); ok {
		return m, nil
	}
	for _, m := range cube.Measures {
		if strings.EqualFold(m.Name, name) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("medida %q não existe no cubo %q", name, cube.Name)
}

func findDimension(cube *metadata.Cube, name string) (*metadata.Dimension, error) {
	if d, ok := cube.FindDimension(name); ok {
		return d, nil
	}
	for _, d := range cube.Dimensions {
		if strings.EqualFold(d.Name, name) {
			return d, nil
		}
	}
	return nil, fmt.Errorf("dimensão %q não existe no cubo %q", name, cube.Name)
}

// defaultHierarchy devolve a hierarquia default (sem nome, ou com o nome da dimensão).
func defaultHierarchy(dim *metadata.Dimension) *metadata.Hierarchy {
	for _, h := range dim.Hierarchies {
		if h.Name == "" || strings.EqualFold(h.Name, dim.Name) {
			return h
		}
	}
	return dim.Hierarchies[0]
}

// findNamedHierarchy procura uma hierarquia NOMEADA (ex.: "Weekly") pelo nome.
func findNamedHierarchy(dim *metadata.Dimension, name string) *metadata.Hierarchy {
	for _, h := range dim.Hierarchies {
		if h.Name != "" && strings.EqualFold(h.Name, name) {
			return h
		}
	}
	return nil
}

// resolveMemberId mapeia [Dim].([Hier]).[seg...].[membro] para nível/valor + ancestrais.
func resolveMemberId(cube *metadata.Cube, id *mdx.Id) (*memberRef, error) {
	if len(id.Segments) < 2 {
		return nil, fmt.Errorf("membro %q incompleto", id.String())
	}
	dim, err := findDimension(cube, id.Segments[0].Name)
	if err != nil {
		return nil, err
	}
	if len(dim.Hierarchies) == 0 {
		return nil, fmt.Errorf("dimensão %q sem hierarquias", dim.Name)
	}
	hier := defaultHierarchy(dim)
	rest := id.Segments[1:]
	// Desambiguação de hierarquia nomeada: [Dim].[Hier].[membro…].
	if len(rest) > 1 {
		if h := findNamedHierarchy(dim, rest[0].Name); h != nil {
			hier = h
			rest = rest[1:]
		}
	}
	levels := hier.Levels

	// Pula o membro "All", se explícito.
	if hier.HasAll && len(rest) > 1 && isAllSegment(rest[0].Name, hier, dim) {
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return nil, fmt.Errorf("membro %q sem nível", id.String())
	}
	if len(rest) > len(levels) {
		return nil, fmt.Errorf("membro %q tem mais segmentos que níveis da dimensão %q", id.String(), dim.Name)
	}
	values := make(map[int]string, len(rest))
	for i, seg := range rest {
		values[i] = seg.Name
	}
	return &memberRef{dim: dim, hier: hier, levelIndex: len(rest) - 1, values: values}, nil
}

func isAllSegment(name string, hier *metadata.Hierarchy, dim *metadata.Dimension) bool {
	if hier.AllMemberName != "" {
		return strings.EqualFold(name, hier.AllMemberName)
	}
	return strings.HasPrefix(strings.ToLower(name), "all ")
}

// memberBinding constrói o binding de agrupamento de um (conjunto de) membro(s)
// do mesmo nível: agrupa pelo nível do membro e restringe aos valores; aplica
// filtros de ancestrais (união por nível).
func memberBinding(level *memberRef, members []*memberRef) levelBinding {
	dim := level.dim
	hier := level.hier
	ownLevel := hier.Levels[level.levelIndex]

	var ownValues []string
	ancestors := map[int]map[string]struct{}{}
	for _, m := range members {
		ownValues = append(ownValues, m.values[m.levelIndex])
		for li, v := range m.values {
			if li == m.levelIndex {
				continue
			}
			if ancestors[li] == nil {
				ancestors[li] = map[string]struct{}{}
			}
			ancestors[li][v] = struct{}{}
		}
	}

	hName := hier.EffectiveName(dim)
	b := levelBinding{ref: query.LevelRef{Dimension: dim.Name, Hierarchy: hName, Level: ownLevel.Name}}
	b.filters = append(b.filters, query.Filter{
		Dimension: dim.Name, Hierarchy: hName, Level: ownLevel.Name, Members: ownValues,
	})
	for li, set := range ancestors {
		b.filters = append(b.filters, query.Filter{
			Dimension: dim.Name, Hierarchy: hName, Level: hier.Levels[li].Name, Members: keys(set),
		})
	}
	return b
}

// levelMembersBinding trata [Dim].[Nível].Members (todos os membros do nível).
func levelMembersBinding(cube *metadata.Cube, id *mdx.Id) (levelBinding, error) {
	if len(id.Segments) < 3 {
		return levelBinding{}, fmt.Errorf("%q: use [Dim].[Nível].Members", id.String())
	}
	dim, err := findDimension(cube, id.Segments[0].Name)
	if err != nil {
		return levelBinding{}, err
	}
	// Segmentos entre a dimensão e ".Members": opcionalmente [Hier] + [Nível].
	inner := id.Segments[1 : len(id.Segments)-1]
	hier := defaultHierarchy(dim)
	if len(inner) >= 2 {
		if h := findNamedHierarchy(dim, inner[0].Name); h != nil {
			hier = h
			inner = inner[1:]
		}
	}
	levelName := inner[len(inner)-1].Name
	for _, l := range hier.Levels {
		if strings.EqualFold(l.Name, levelName) {
			return levelBinding{ref: query.LevelRef{Dimension: dim.Name, Hierarchy: hier.EffectiveName(dim), Level: l.Name}}, nil
		}
	}
	return levelBinding{}, fmt.Errorf("nível %q não encontrado em [%s].Members", levelName, dim.Name)
}

// childrenBinding trata [membro].Children (membros do nível imediatamente abaixo).
func childrenBinding(cube *metadata.Cube, id *mdx.Id) (levelBinding, error) {
	parentID := &mdx.Id{Segments: id.Segments[:len(id.Segments)-1]}
	ref, err := resolveMemberId(cube, parentID)
	if err != nil {
		return levelBinding{}, err
	}
	childIdx := ref.levelIndex + 1
	if childIdx >= len(ref.hier.Levels) {
		return levelBinding{}, fmt.Errorf("membro %q não tem nível-filho", parentID.String())
	}
	childLevel := ref.hier.Levels[childIdx]
	hName := ref.hier.EffectiveName(ref.dim)
	b := levelBinding{ref: query.LevelRef{Dimension: ref.dim.Name, Hierarchy: hName, Level: childLevel.Name}}
	for li, v := range ref.values {
		b.filters = append(b.filters, query.Filter{
			Dimension: ref.dim.Name, Hierarchy: hName, Level: ref.hier.Levels[li].Name, Members: []string{v},
		})
	}
	return b, nil
}

// analyzeSlicer reduz o WHERE a filtros + (opcional) medida default.
func analyzeSlicer(cube *metadata.Cube, exp mdx.Exp) ([]query.Filter, *metadata.Measure, error) {
	var members []mdx.Exp
	if fc, ok := exp.(*mdx.FunCall); ok && fc.Syntax == mdx.SyntaxParentheses {
		members = fc.Args
	} else {
		members = []mdx.Exp{exp}
	}
	var filters []query.Filter
	var measure *metadata.Measure
	for _, m := range members {
		id, ok := m.(*mdx.Id)
		if !ok {
			return nil, nil, fmt.Errorf("slicer com expressão não suportada nesta fase")
		}
		if isMeasureId(id) {
			meas, err := resolveMeasure(cube, id)
			if err != nil {
				return nil, nil, err
			}
			measure = meas
			continue
		}
		ref, err := resolveMemberId(cube, id)
		if err != nil {
			return nil, nil, err
		}
		hName := ref.hier.EffectiveName(ref.dim)
		for li, v := range ref.values {
			filters = append(filters, query.Filter{
				Dimension: ref.dim.Name, Hierarchy: hName, Level: ref.hier.Levels[li].Name, Members: []string{v},
			})
		}
	}
	return filters, measure, nil
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
