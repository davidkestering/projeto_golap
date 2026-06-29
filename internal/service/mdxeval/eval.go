package mdxeval

import (
	"context"
	"fmt"
	"strings"

	"cubodw/internal/engine/mdx"
	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	"cubodw/internal/service/queryexec"
)

// axisLayout descreve, para um eixo, se ele carrega medidas ou níveis e — no
// caso de níveis — o intervalo de colunas de nível que lhe pertence.
type axisLayout struct {
	ordinal    int
	isMeasures bool
	levelStart int // [start,end) nas colunas de nível do resultado
	levelEnd   int
	dims       []string // dimensão de cada binding (para uniqueName)
	levels     []string // nível de cada binding
}

// Evaluate avalia a query MDX contra o cubo, executando via exec, e devolve o
// CellSet pivotado.
func Evaluate(ctx context.Context, cube *metadata.Cube, q *mdx.Query, exec *queryexec.Service) (*CellSet, error) {
	if len(q.Formulas) > 0 {
		return nil, fmt.Errorf("membros/conjuntos calculados (WITH) ainda não suportados nesta fase")
	}

	plans := make([]axisPlan, len(q.Axes))
	measureAxis := -1
	for i, ax := range q.Axes {
		p, err := analyzeAxis(cube, ax.Exp)
		if err != nil {
			return nil, fmt.Errorf("eixo %s: %w", mdx.AxisName(ax.Ordinal), err)
		}
		if p.isMeasures {
			if measureAxis >= 0 {
				return nil, fmt.Errorf("medidas aparecem em mais de um eixo")
			}
			measureAxis = i
		}
		plans[i] = p
	}

	var slicerFilters []query.Filter
	var slicerMeasure *metadata.Measure
	if q.Slicer != nil {
		f, m, err := analyzeSlicer(cube, q.Slicer)
		if err != nil {
			return nil, fmt.Errorf("WHERE: %w", err)
		}
		slicerFilters, slicerMeasure = f, m
	}

	measures, err := resolveMeasures(cube, plans, measureAxis, slicerMeasure)
	if err != nil {
		return nil, err
	}

	// Monta a query da Fase 2 e o layout dos eixos.
	qry := query.Query{Cube: cube.Name, Filters: slicerFilters}
	layouts := make([]axisLayout, len(q.Axes))
	for i, ax := range q.Axes {
		lay := axisLayout{ordinal: ax.Ordinal}
		if plans[i].isMeasures {
			lay.isMeasures = true
		} else {
			lay.levelStart = len(qry.Rows)
			for _, b := range plans[i].bindings {
				qry.Rows = append(qry.Rows, b.ref)
				qry.Filters = append(qry.Filters, b.filters...)
				lay.dims = append(lay.dims, b.ref.Dimension)
				lay.levels = append(lay.levels, b.ref.Level)
			}
			lay.levelEnd = len(qry.Rows)
		}
		layouts[i] = lay
	}
	for _, m := range measures {
		qry.Measures = append(qry.Measures, m.Name)
	}

	st, err := exec.Plan(cube, qry)
	if err != nil {
		return nil, err
	}
	if !exec.HasDB() {
		return nil, fmt.Errorf("sem conexão de banco")
	}
	res, err := exec.Run(ctx, cube, st)
	if err != nil {
		return nil, err
	}

	return pivot(cube, q, layouts, measures, measureAxis, res), nil
}

func resolveMeasures(cube *metadata.Cube, plans []axisPlan, measureAxis int, slicerMeasure *metadata.Measure) ([]*metadata.Measure, error) {
	if measureAxis >= 0 {
		return plans[measureAxis].measures, nil
	}
	if slicerMeasure != nil {
		return []*metadata.Measure{slicerMeasure}, nil
	}
	if cube.DefaultMeasure != "" {
		if m, ok := cube.FindMeasure(cube.DefaultMeasure); ok {
			return []*metadata.Measure{m}, nil
		}
	}
	if len(cube.Measures) > 0 {
		return []*metadata.Measure{cube.Measures[0]}, nil
	}
	return nil, fmt.Errorf("cubo %q sem medidas", cube.Name)
}

// pivot transforma os records num CellSet, derivando as posições dos eixos de
// nível a partir dos dados (semântica não-vazia).
func pivot(cube *metadata.Cube, q *mdx.Query, layouts []axisLayout, measures []*metadata.Measure, measureAxis int, res *query.Result) *CellSet {
	levelColCount := 0
	for _, lay := range layouts {
		if !lay.isMeasures && lay.levelEnd > levelColCount {
			levelColCount = lay.levelEnd
		}
	}

	cs := &CellSet{Cube: cube.Name, SQL: res.SQL}

	// Índice de posições por eixo de nível: tupla(string) -> índice.
	posIndex := make([]map[string]int, len(layouts))
	cs.Axes = make([]Axis, len(layouts))
	for ai, lay := range layouts {
		cs.Axes[ai] = Axis{Ordinal: lay.ordinal, Name: mdx.AxisName(lay.ordinal)}
		if lay.isMeasures {
			for _, m := range measures {
				cs.Axes[ai].Positions = append(cs.Axes[ai].Positions, Position{
					Members: []Member{{Caption: m.Caption, UniqueName: m.UniqueName()}},
				})
			}
		} else {
			posIndex[ai] = map[string]int{}
		}
	}

	// Coordenada de uma row nos eixos de nível; cria posições conforme aparecem.
	coordOf := func(row []query.Cell) []int {
		coords := make([]int, len(layouts))
		for ai, lay := range layouts {
			if lay.isMeasures {
				continue
			}
			vals := make([]string, 0, lay.levelEnd-lay.levelStart)
			for c := lay.levelStart; c < lay.levelEnd; c++ {
				vals = append(vals, fmt.Sprint(row[c].Value))
			}
			key := strings.Join(vals, "\x1f")
			idx, ok := posIndex[ai][key]
			if !ok {
				idx = len(cs.Axes[ai].Positions)
				posIndex[ai][key] = idx
				members := make([]Member, len(vals))
				for k, v := range vals {
					members[k] = Member{
						Caption:    v,
						UniqueName: fmt.Sprintf("[%s].[%s].[%s]", lay.dims[k], lay.levels[k], v),
					}
				}
				cs.Axes[ai].Positions = append(cs.Axes[ai].Positions, Position{Members: members})
			}
			coords[ai] = idx
		}
		return coords
	}

	for _, row := range res.Rows {
		base := coordOf(row)
		if measureAxis >= 0 {
			for mi := range measures {
				coords := append([]int(nil), base...)
				coords[measureAxis] = mi
				val := row[levelColCount+mi]
				cs.Cells = append(cs.Cells, Cell{Coords: coords, Value: val.Value, Formatted: val.Formatted})
			}
		} else {
			val := row[levelColCount] // medida única (default)
			cs.Cells = append(cs.Cells, Cell{Coords: base, Value: val.Value, Formatted: val.Formatted})
		}
	}

	if len(cs.Axes) == 2 {
		cs.Grid = buildGrid(cs)
	}
	return cs
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
