package web

import (
	"net/http"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/service/discover"
)

// discoverAPI registra as rotas de descoberta sob /saiku/api/discover, no shape
// (campos name/uniqueName/caption) compatível com o Saiku.
type discoverAPI struct {
	svc *discover.Service
}

func (a *discoverAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /saiku/api/discover", a.handleConnections)
	base := "GET /saiku/api/discover/{connection}/{catalog}/{schema}/{cube}"
	mux.HandleFunc(base+"/metadata", a.handleCubeMetadata)
	mux.HandleFunc(base+"/dimensions", a.handleDimensions)
}

// handleConnections devolve a árvore connection → catalog → schema → cubes.
func (a *discoverAPI) handleConnections(w http.ResponseWriter, _ *http.Request) {
	conn := connectionDTO{
		Name: a.svc.Connection(),
		Catalogs: []catalogDTO{{
			Name: a.svc.Catalog(),
			Schemas: []schemaDTO{{
				Name:  a.svc.SchemaName(),
				Cubes: a.cubeDTOs(),
			}},
		}},
	}
	writeJSON(w, http.StatusOK, []connectionDTO{conn})
}

// handleCubeMetadata devolve dimensões (com hierarquias e níveis) e medidas.
func (a *discoverAPI) handleCubeMetadata(w http.ResponseWriter, r *http.Request) {
	c, ok := a.resolveCube(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, cubeMetadataDTO{
		Name:           c.Name,
		UniqueName:     metadata.Bracket(c.Name),
		Caption:        c.Caption,
		DefaultMeasure: c.DefaultMeasure,
		Dimensions:     dimensionDTOs(c),
		Measures:       measureDTOs(c),
	})
}

// handleDimensions devolve apenas a lista de dimensões do cubo.
func (a *discoverAPI) handleDimensions(w http.ResponseWriter, r *http.Request) {
	c, ok := a.resolveCube(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, dimensionDTOs(c))
}

// resolveCube valida connection/catalog/schema e localiza o cubo; responde erro
// e devolve ok=false quando algo não bate.
func (a *discoverAPI) resolveCube(w http.ResponseWriter, r *http.Request) (*metadata.Cube, bool) {
	if r.PathValue("connection") != a.svc.Connection() ||
		r.PathValue("catalog") != a.svc.Catalog() ||
		r.PathValue("schema") != a.svc.SchemaName() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "connection/catalog/schema desconhecido"})
		return nil, false
	}
	c, found := a.svc.Cube(r.PathValue("cube"))
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error":     "cubo não encontrado",
			"cube":      r.PathValue("cube"),
			"available": cubeNames(a.svc.Cubes()),
		})
		return nil, false
	}
	return c, true
}

func (a *discoverAPI) cubeDTOs() []cubeDTO {
	out := make([]cubeDTO, 0, len(a.svc.Cubes()))
	for _, c := range a.svc.Cubes() {
		out = append(out, cubeDTO{
			Name:       c.Name,
			UniqueName: metadata.Bracket(c.Name),
			Caption:    c.Caption,
			Connection: a.svc.Connection(),
			Catalog:    a.svc.Catalog(),
			Schema:     a.svc.SchemaName(),
			Visible:    c.Visible,
		})
	}
	return out
}

func cubeNames(cubes []*metadata.Cube) []string {
	names := make([]string, 0, len(cubes))
	for _, c := range cubes {
		names = append(names, c.Name)
	}
	return names
}

func dimensionDTOs(c *metadata.Cube) []dimensionDTO {
	out := make([]dimensionDTO, 0, len(c.Dimensions))
	for _, d := range c.Dimensions {
		dto := dimensionDTO{
			Name:       d.Name,
			UniqueName: d.UniqueName(),
			Caption:    d.Caption,
			Type:       d.Type,
			Visible:    true,
		}
		for _, h := range d.Hierarchies {
			hdto := hierarchyDTO{
				Name:       h.EffectiveName(d),
				UniqueName: h.UniqueName(d),
				Caption:    h.EffectiveName(d),
				HasAll:     h.HasAll,
			}
			for _, l := range h.Levels {
				hdto.Levels = append(hdto.Levels, levelDTO{
					Name:       l.Name,
					UniqueName: l.UniqueName(d, h),
					Caption:    l.Name,
					LevelType:  l.LevelType,
				})
			}
			dto.Hierarchies = append(dto.Hierarchies, hdto)
		}
		out = append(out, dto)
	}
	return out
}

func measureDTOs(c *metadata.Cube) []measureDTO {
	out := make([]measureDTO, 0, len(c.Measures))
	for _, m := range c.Measures {
		out = append(out, measureDTO{
			Name:         m.Name,
			UniqueName:   m.UniqueName(),
			Caption:      m.Caption,
			Aggregator:   m.Aggregator,
			FormatString: m.FormatString,
			Visible:      m.Visible,
		})
	}
	return out
}

// --- DTOs (JSON) ---------------------------------------------------------

type connectionDTO struct {
	Name     string       `json:"name"`
	Catalogs []catalogDTO `json:"catalogs"`
}

type catalogDTO struct {
	Name    string      `json:"name"`
	Schemas []schemaDTO `json:"schemas"`
}

type schemaDTO struct {
	Name  string    `json:"name"`
	Cubes []cubeDTO `json:"cubes"`
}

type cubeDTO struct {
	Name       string `json:"name"`
	UniqueName string `json:"uniqueName"`
	Caption    string `json:"caption"`
	Connection string `json:"connection"`
	Catalog    string `json:"catalog"`
	Schema     string `json:"schema"`
	Visible    bool   `json:"visible"`
}

type cubeMetadataDTO struct {
	Name           string         `json:"name"`
	UniqueName     string         `json:"uniqueName"`
	Caption        string         `json:"caption"`
	DefaultMeasure string         `json:"defaultMeasure"`
	Dimensions     []dimensionDTO `json:"dimensions"`
	Measures       []measureDTO   `json:"measures"`
}

type dimensionDTO struct {
	Name        string         `json:"name"`
	UniqueName  string         `json:"uniqueName"`
	Caption     string         `json:"caption"`
	Type        string         `json:"type"`
	Visible     bool           `json:"visible"`
	Hierarchies []hierarchyDTO `json:"hierarchies"`
}

type hierarchyDTO struct {
	Name       string     `json:"name"`
	UniqueName string     `json:"uniqueName"`
	Caption    string     `json:"caption"`
	HasAll     bool       `json:"hasAll"`
	Levels     []levelDTO `json:"levels"`
}

type levelDTO struct {
	Name       string `json:"name"`
	UniqueName string `json:"uniqueName"`
	Caption    string `json:"caption"`
	LevelType  string `json:"levelType"`
}

type measureDTO struct {
	Name         string `json:"name"`
	UniqueName   string `json:"uniqueName"`
	Caption      string `json:"caption"`
	Aggregator   string `json:"aggregator"`
	FormatString string `json:"formatString"`
	Visible      bool   `json:"visible"`
}
