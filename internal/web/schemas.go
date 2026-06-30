package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/schema/mondrian"
	"cubodw/internal/engine/schema/yaml"
	"cubodw/internal/service/discover"
)

// schemasAPI gerencia o catálogo de schemas/cubos em tempo de execução:
// listar, validar, adicionar e remover. Adicionar/remover exigem papel admin
// (ver isAdminOnly em auth.go). A persistência em disco é opcional (dir vazio =
// só em memória).
type schemasAPI struct {
	svc *discover.Service
	dir string
}

func (a *schemasAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /saiku/api/schemas", a.handleList)
	mux.HandleFunc("POST /saiku/api/schemas", a.handleAdd)
	mux.HandleFunc("POST /saiku/api/schemas/validate", a.handleValidate)
	mux.HandleFunc("DELETE /saiku/api/schemas/{name}", a.handleDelete)
}

type schemaRequest struct {
	Content string `json:"content"`
	Format  string `json:"format"` // "yaml" | "xml"; vazio = detectar
}

type cubeSummary struct {
	Name       string `json:"name"`
	Measures   int    `json:"measures"`
	Dimensions int    `json:"dimensions"`
}

type schemaInfo struct {
	Name  string        `json:"name"`
	Cubes []cubeSummary `json:"cubes"`
}

// parseSchema interpreta o conteúdo como YAML ou Mondrian XML.
func parseSchema(req schemaRequest) (*metadata.Schema, string, error) {
	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, "", fmt.Errorf("conteúdo vazio")
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		if strings.HasPrefix(content, "<") {
			format = "xml"
		} else {
			format = "yaml"
		}
	}
	switch format {
	case "xml", "mondrian":
		sc, err := mondrian.LoadBytes([]byte(content))
		return sc, "xml", err
	case "yaml", "yml":
		sc, err := yaml.LoadBytes([]byte(content))
		return sc, "yaml", err
	default:
		return nil, "", fmt.Errorf("formato desconhecido: %q (use yaml ou xml)", format)
	}
}

func summarize(sc *metadata.Schema) schemaInfo {
	info := schemaInfo{Name: sc.Name}
	for _, c := range sc.Cubes {
		info.Cubes = append(info.Cubes, cubeSummary{
			Name: c.Name, Measures: len(c.Measures), Dimensions: len(c.Dimensions),
		})
	}
	return info
}

func (a *schemasAPI) handleList(w http.ResponseWriter, _ *http.Request) {
	out := make([]schemaInfo, 0)
	for _, sc := range a.svc.Schemas() {
		out = append(out, summarize(sc))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *schemasAPI) handleValidate(w http.ResponseWriter, r *http.Request) {
	var req schemaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": "JSON inválido"})
		return
	}
	sc, _, err := parseSchema(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"valid": true, "schema": summarize(sc)})
}

func (a *schemasAPI) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req schemaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido"})
		return
	}
	sc, ext, err := parseSchema(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.svc.AddSchema(sc); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if a.dir != "" {
		if err := a.persist(sc.Name, ext, req.Content); err != nil {
			// Não desfaz o registro em memória; só avisa.
			writeJSON(w, http.StatusCreated, map[string]any{
				"schema": summarize(sc), "warning": "registrado, mas falhou ao persistir: " + err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"schema": summarize(sc)})
}

func (a *schemasAPI) handleDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !a.svc.RemoveSchema(name) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "schema não encontrado"})
		return
	}
	if a.dir != "" {
		a.removeFiles(name)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed", "name": name})
}

// --- persistência opcional em disco --------------------------------------

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (a *schemasAPI) persist(name, ext, content string) error {
	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(a.dir, sanitizeName(name)+"."+ext), []byte(content), 0o644)
}

func (a *schemasAPI) removeFiles(name string) {
	base := filepath.Join(a.dir, sanitizeName(name))
	for _, ext := range []string{".yaml", ".yml", ".xml"} {
		_ = os.Remove(base + ext)
	}
}

// loadSchemasDir carrega todos os schemas de um diretório (yaml/xml) e os
// registra no serviço. Erros por arquivo são devolvidos como avisos.
func loadSchemasDir(svc *discover.Service, dir string) []string {
	var warnings []string
	if dir == "" {
		return warnings
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return warnings // dir ainda não existe: nada a carregar
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != ".yaml" && ext != ".yml" && ext != ".xml" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			warnings = append(warnings, e.Name()+": "+err.Error())
			continue
		}
		sc, _, err := parseSchema(schemaRequest{Content: string(b)})
		if err != nil {
			warnings = append(warnings, e.Name()+": "+err.Error())
			continue
		}
		if err := svc.AddSchema(sc); err != nil {
			warnings = append(warnings, e.Name()+": "+err.Error())
		}
	}
	return warnings
}
