package web

import (
	"context"
	"encoding/json"
	"net/http"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	enginesql "cubodw/internal/engine/sql"
	"cubodw/internal/service/discover"
	"cubodw/internal/service/queryexec"
)

// queryAPI registra as rotas de execução de query sob /saiku/api/query.
type queryAPI struct {
	discover *discover.Service
	exec     *queryexec.Service
}

func (a *queryAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /saiku/api/query", a.handleQuery)
	mux.HandleFunc("POST /saiku/api/query/preview", a.handlePreview)
	mux.HandleFunc("POST /saiku/api/query/drillthrough", a.handleDrillthrough)
}

// handleQuery executa a query e devolve o Result (records).
func (a *queryAPI) handleQuery(w http.ResponseWriter, r *http.Request) {
	q, cube, st, ok := a.planFromRequest(w, r)
	if !ok {
		return
	}
	if !a.exec.HasDB() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "sem conexão de banco (defina CUBODW_PG_DSN)"})
		return
	}
	res, err := a.exec.Run(r.Context(), cube, st)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if q.Totals && len(q.Rows)+len(q.Columns) > 0 {
		if err := a.appendTotal(r.Context(), cube, q, res); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, res)
}

// appendTotal calcula o total geral (sem agrupar por níveis) e o anexa como
// última linha do resultado.
func (a *queryAPI) appendTotal(ctx context.Context, cube *metadata.Cube, q query.Query, res *query.Result) error {
	totalQ := query.Query{Cube: q.Cube, Measures: q.Measures, Filters: q.Filters}
	st, err := a.exec.Plan(cube, totalQ)
	if err != nil {
		return err
	}
	tres, err := a.exec.Run(ctx, cube, st)
	if err != nil {
		return err
	}
	if len(tres.Rows) == 0 {
		return nil
	}
	levelCount := 0
	for _, c := range res.Columns {
		if c.Kind == "level" {
			levelCount++
		}
	}
	row := make([]query.Cell, 0, len(res.Columns))
	for i := 0; i < levelCount; i++ {
		if i == 0 {
			row = append(row, query.Cell{Value: "Total", Formatted: "Total"})
		} else {
			row = append(row, query.Cell{Formatted: ""})
		}
	}
	row = append(row, tres.Rows[0]...) // medidas (totalQ só tem colunas de medida)
	res.Rows = append(res.Rows, row)
	return nil
}

// handleDrillthrough devolve as linhas de fato cruas por trás de um contexto.
func (a *queryAPI) handleDrillthrough(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cube    string         `json:"cube"`
		Filters []query.Filter `json:"filters"`
		Maxrows int            `json:"maxrows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido: " + err.Error()})
		return
	}
	cube, found := a.discover.Cube(req.Cube)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cubo não encontrado", "cube": req.Cube, "available": cubeNames(a.discover.Cubes())})
		return
	}
	if !a.exec.HasDB() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "sem conexão de banco"})
		return
	}
	res, err := a.exec.Drillthrough(r.Context(), cube, req.Filters, req.Maxrows)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handlePreview valida e gera a SQL sem executar.
func (a *queryAPI) handlePreview(w http.ResponseWriter, r *http.Request) {
	_, cube, st, ok := a.planFromRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cube":    cube.Name,
		"status":  "PREVIEW",
		"sql":     st.SQL,
		"columns": st.Columns,
	})
}

// planFromRequest decodifica a query, resolve o cubo e gera a SQL (validação).
// Responde o status adequado e devolve ok=false em caso de erro.
func (a *queryAPI) planFromRequest(w http.ResponseWriter, r *http.Request) (query.Query, *metadata.Cube, *enginesql.Statement, bool) {
	var q query.Query
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido: " + err.Error()})
		return q, nil, nil, false
	}
	cube, found := a.discover.Cube(q.Cube)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":     "cubo não encontrado",
			"cube":      q.Cube,
			"available": cubeNames(a.discover.Cubes()),
		})
		return q, nil, nil, false
	}
	st, err := a.exec.Plan(cube, q)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return q, nil, nil, false
	}
	return q, cube, st, true
}
