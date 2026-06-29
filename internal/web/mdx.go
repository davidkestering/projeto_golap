package web

import (
	"encoding/json"
	"net/http"

	"cubodw/internal/engine/mdx"
	"cubodw/internal/service/discover"
	"cubodw/internal/service/mdxeval"
	"cubodw/internal/service/queryexec"
)

// mdxAPI expõe o parser MDX (texto → AST) e o avaliador (MDX → CellSet) sob
// /saiku/api/mdx/*.
type mdxAPI struct {
	discover *discover.Service
	exec     *queryexec.Service
}

func (a *mdxAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /saiku/api/mdx/parse", a.handleParse)
	mux.HandleFunc("POST /saiku/api/mdx/execute", a.handleExecute)
}

type mdxParseRequest struct {
	MDX string `json:"mdx"`
}

type mdxAxisDTO struct {
	Ordinal  int    `json:"ordinal"`
	Name     string `json:"name"`
	NonEmpty bool   `json:"nonEmpty"`
	Exp      string `json:"exp"`
}

type mdxFormulaDTO struct {
	Member bool   `json:"member"`
	Name   string `json:"name"`
	Exp    string `json:"exp"`
}

func (a *mdxAPI) handleParse(w http.ResponseWriter, r *http.Request) {
	var req mdxParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido: " + err.Error()})
		return
	}
	q, err := mdx.ParseQuery(req.MDX)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "PARSE_ERROR", "error": err.Error()})
		return
	}

	axes := make([]mdxAxisDTO, 0, len(q.Axes))
	for _, ax := range q.Axes {
		axes = append(axes, mdxAxisDTO{
			Ordinal:  ax.Ordinal,
			Name:     mdx.AxisName(ax.Ordinal),
			NonEmpty: ax.NonEmpty,
			Exp:      ax.Exp.String(),
		})
	}
	formulas := make([]mdxFormulaDTO, 0, len(q.Formulas))
	for _, f := range q.Formulas {
		formulas = append(formulas, mdxFormulaDTO{Member: f.IsMember, Name: f.Name.String(), Exp: f.Exp.String()})
	}
	var slicer string
	if q.Slicer != nil {
		slicer = q.Slicer.String()
	}

	_, cubeKnown := a.discover.Cube(q.Cube)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "PARSED",
		"cube":      q.Cube,
		"cubeKnown": cubeKnown,
		"formulas":  formulas,
		"axes":      axes,
		"slicer":    slicer,
		"canonical": q.String(),
	})
}

// handleExecute faz o parse, avalia e devolve o CellSet.
func (a *mdxAPI) handleExecute(w http.ResponseWriter, r *http.Request) {
	var req mdxParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido: " + err.Error()})
		return
	}
	q, err := mdx.ParseQuery(req.MDX)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "PARSE_ERROR", "error": err.Error()})
		return
	}
	cube, found := a.discover.Cube(q.Cube)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "cubo não encontrado", "cube": q.Cube,
			"available": cubeNames(a.discover.Cubes()),
		})
		return
	}
	if !a.exec.HasDB() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "sem conexão de banco (defina CUBODW_PG_DSN)"})
		return
	}
	cs, err := mdxeval.Evaluate(r.Context(), cube, q, a.exec)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": "EVAL_ERROR", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cs)
}
