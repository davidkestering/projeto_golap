package web

import (
	"encoding/json"
	"net/http"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	"cubodw/internal/service/discover"
	"cubodw/internal/service/queryexec"
)

// aiAPI expõe a "AI Query API": uma surface tipada para agentes/LLMs consultarem
// cubos SEM escrever MDX. O agente busca um schema auto-descritivo, preenche um
// JSON contra ele, o servidor valida os nomes contra o cubo vivo e executa.
type aiAPI struct {
	discover *discover.Service
	exec     *queryexec.Service
}

func (a *aiAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /saiku/api/ai/cubes", a.handleCubes)
	mux.HandleFunc("GET /saiku/api/ai/schema/{cube}", a.handleSchema)
	mux.HandleFunc("POST /saiku/api/ai/query", a.handleQuery)
}

// --- GET /ai/cubes -------------------------------------------------------

type aiCubeDTO struct {
	ConnectionName string `json:"connectionName"`
	Catalog        string `json:"catalog"`
	Schema         string `json:"schema"`
	CubeName       string `json:"cubeName"`
	CubeCaption    string `json:"cubeCaption"`
	DefaultMeasure string `json:"defaultMeasure"`
	MeasureCount   int    `json:"measureCount"`
}

func (a *aiAPI) handleCubes(w http.ResponseWriter, _ *http.Request) {
	out := make([]aiCubeDTO, 0, len(a.discover.Cubes()))
	for _, c := range a.discover.Cubes() {
		schemaName := a.discover.SchemaOfCube(c.Name)
		out = append(out, aiCubeDTO{
			ConnectionName: a.discover.Connection(),
			Catalog:        schemaName,
			Schema:         schemaName,
			CubeName:       c.Name,
			CubeCaption:    c.Caption,
			DefaultMeasure: c.DefaultMeasure,
			MeasureCount:   len(c.Measures),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// --- GET /ai/schema/{cube} ----------------------------------------------

func (a *aiAPI) handleSchema(w http.ResponseWriter, r *http.Request) {
	c, ok := a.discover.Cube(r.PathValue("cube"))
	if !ok {
		a.notFoundCube(w, r.PathValue("cube"))
		return
	}

	measures := make([]map[string]any, 0, len(c.Measures))
	for _, m := range c.Measures {
		measures = append(measures, map[string]any{
			"name": m.Name, "uniqueName": m.UniqueName(), "aggregator": m.Aggregator,
		})
	}

	dims := make([]map[string]any, 0, len(c.Dimensions))
	for _, d := range c.Dimensions {
		hiers := make([]map[string]any, 0, len(d.Hierarchies))
		for _, h := range d.Hierarchies {
			levels := make([]map[string]any, 0, len(h.Levels))
			for _, l := range h.Levels {
				levels = append(levels, map[string]any{
					"name":          l.Name,
					"uniqueName":    l.UniqueName(d, h),
					"sampleMembers": a.sampleMembers(r, c, d, l),
				})
			}
			hiers = append(hiers, map[string]any{"name": h.EffectiveName(d), "levels": levels})
		}
		dims = append(dims, map[string]any{"name": d.Name, "type": d.Type, "hierarchies": hiers})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"cubeName":       c.Name,
		"cubeUniqueName": metadata.Bracket(c.Name),
		"defaultMeasure": c.DefaultMeasure,
		"measures":       measures,
		"dimensions":     dims,
		"examples":       a.examples(c),
		"requestSchema":  aiRequestSchema(),
	})
}

// sampleMembers busca alguns membros reais de um nível (até 5), para o schema
// ser auto-descritivo. Falhas (snowflake, sem banco) devolvem lista vazia.
func (a *aiAPI) sampleMembers(r *http.Request, c *metadata.Cube, d *metadata.Dimension, l *metadata.Level) []string {
	if !a.exec.HasDB() {
		return []string{}
	}
	members, err := a.exec.EnumerateLevel(r.Context(), c, query.LevelRef{Dimension: d.Name, Level: l.Name}, nil)
	if err != nil {
		return []string{}
	}
	if len(members) > 5 {
		members = members[:5]
	}
	return members
}

// examples devolve 1 corpo de requisição pronto (breakdown) para o cubo.
func (a *aiAPI) examples(c *metadata.Cube) []any {
	var measure string
	if c.DefaultMeasure != "" {
		measure = c.DefaultMeasure
	} else if len(c.Measures) > 0 {
		measure = c.Measures[0].Name
	}
	// primeira dimensão com tabela simples + 1º nível
	for _, d := range c.Dimensions {
		if len(d.Hierarchies) == 0 || len(d.Hierarchies[0].Levels) == 0 {
			continue
		}
		h := d.Hierarchies[0]
		if h.Table.Table == "" {
			continue // snowflake
		}
		return []any{map[string]any{
			"cube":     c.Name,
			"measures": []string{measure},
			"rows":     []map[string]string{{"dimension": d.Name, "level": h.Levels[0].Name}},
		}}
	}
	return []any{}
}

func aiRequestSchema() map[string]any {
	return map[string]any{
		"$schema":  "https://json-schema.org/draft/2020-12/schema",
		"title":    "AiQueryRequest",
		"required": []string{"cube", "measures"},
		"properties": map[string]any{
			"cube":     map[string]any{"type": "string"},
			"measures": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"rows":     map[string]any{"type": "array", "description": "[{dimension, level}]"},
			"columns":  map[string]any{"type": "array", "description": "[{dimension, level}]"},
			"filters":  map[string]any{"type": "array", "description": "[{dimension, level, members:[…]}]"},
		},
	}
}

// --- POST /ai/query ------------------------------------------------------

func (a *aiAPI) handleQuery(w http.ResponseWriter, r *http.Request) {
	var q query.Query
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "BAD_JSON", "error": err.Error()})
		return
	}
	c, ok := a.discover.Cube(q.Cube)
	if !ok {
		a.notFoundCube(w, q.Cube)
		return
	}
	// Validação com auto-correção: nome inválido devolve field + available.
	if verr := validateAIQuery(c, q); verr != nil {
		writeJSON(w, http.StatusBadRequest, verr)
		return
	}
	if !a.exec.HasDB() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "NO_DB", "error": "sem conexão de banco"})
		return
	}
	st, err := a.exec.Plan(c, q)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "PLAN_ERROR", "error": err.Error()})
		return
	}
	res, err := a.exec.Run(r.Context(), c, st)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"status": "RUN_ERROR", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "OK", "result": res})
}

type aiValidationError struct {
	Status    string   `json:"status"`
	Field     string   `json:"field"`
	Value     string   `json:"value"`
	Message   string   `json:"message"`
	Available []string `json:"available"`
}

// validateAIQuery confere medidas, dimensões e níveis contra o cubo, devolvendo
// um envelope de auto-correção no primeiro nome inválido.
func validateAIQuery(c *metadata.Cube, q query.Query) *aiValidationError {
	if len(q.Measures) == 0 {
		return &aiValidationError{Status: "VALIDATION_ERROR", Field: "measures", Message: "informe ao menos uma medida", Available: measureNames(c)}
	}
	for _, m := range q.Measures {
		if _, ok := c.FindMeasure(m); !ok {
			return &aiValidationError{Status: "VALIDATION_ERROR", Field: "measures", Value: m, Message: "medida inexistente", Available: measureNames(c)}
		}
	}
	for _, ref := range q.AxisLevels() {
		if verr := validateLevelRef(c, "rows/columns", ref); verr != nil {
			return verr
		}
	}
	for _, f := range q.Filters {
		if verr := validateLevelRef(c, "filters", query.LevelRef{Dimension: f.Dimension, Hierarchy: f.Hierarchy, Level: f.Level}); verr != nil {
			return verr
		}
	}
	return nil
}

func validateLevelRef(c *metadata.Cube, field string, ref query.LevelRef) *aiValidationError {
	d, ok := c.FindDimension(ref.Dimension)
	if !ok {
		return &aiValidationError{Status: "VALIDATION_ERROR", Field: field + ".dimension", Value: ref.Dimension, Message: "dimensão inexistente", Available: dimensionNames(c)}
	}
	if len(d.Hierarchies) == 0 {
		return &aiValidationError{Status: "VALIDATION_ERROR", Field: field + ".dimension", Value: ref.Dimension, Message: "dimensão sem hierarquias"}
	}
	h := d.Hierarchies[0]
	for _, l := range h.Levels {
		if l.Name == ref.Level {
			return nil
		}
	}
	return &aiValidationError{Status: "VALIDATION_ERROR", Field: field + ".level", Value: ref.Level, Message: "nível inexistente na dimensão " + ref.Dimension, Available: levelNames(h)}
}

func measureNames(c *metadata.Cube) []string {
	out := make([]string, 0, len(c.Measures))
	for _, m := range c.Measures {
		out = append(out, m.Name)
	}
	return out
}
func dimensionNames(c *metadata.Cube) []string {
	out := make([]string, 0, len(c.Dimensions))
	for _, d := range c.Dimensions {
		out = append(out, d.Name)
	}
	return out
}
func levelNames(h *metadata.Hierarchy) []string {
	out := make([]string, 0, len(h.Levels))
	for _, l := range h.Levels {
		out = append(out, l.Name)
	}
	return out
}

func (a *aiAPI) notFoundCube(w http.ResponseWriter, name string) {
	writeJSON(w, http.StatusNotFound, map[string]any{
		"status": "VALIDATION_ERROR", "field": "cube", "value": name,
		"message": "cubo inexistente", "available": cubeNames(a.discover.Cubes()),
	})
}
