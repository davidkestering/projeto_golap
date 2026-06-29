package web

import (
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
	_ = q
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
