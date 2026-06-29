package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

// As rotas de descoberta usam o schema FoodMart embutido (config sem SchemaPath).

func TestDiscoverConnections(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/saiku/api/discover")
	if err != nil {
		t.Fatalf("GET /discover: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var conns []connectionDTO
	if err := json.NewDecoder(resp.Body).Decode(&conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conns) != 1 || len(conns[0].Catalogs) != 1 || len(conns[0].Catalogs[0].Schemas) != 1 {
		t.Fatalf("árvore inesperada: %+v", conns)
	}
	cubes := conns[0].Catalogs[0].Schemas[0].Cubes
	if len(cubes) != 7 {
		t.Fatalf("cubos = %d, quero 7", len(cubes))
	}
}

func TestDiscoverCubeMetadata(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	url := ts.URL + "/saiku/api/discover/foodmart/FoodMart/FoodMart/Sales/metadata"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var meta cubeMetadataDTO
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.Name != "Sales" || meta.UniqueName != "[Sales]" {
		t.Errorf("cube meta inesperado: %+v", meta)
	}
	if len(meta.Dimensions) == 0 || len(meta.Measures) == 0 {
		t.Fatalf("dims=%d measures=%d", len(meta.Dimensions), len(meta.Measures))
	}
	// Verifica uniqueName de uma medida conhecida.
	var found bool
	for _, m := range meta.Measures {
		if m.Name == "Unit Sales" && m.UniqueName == "[Measures].[Unit Sales]" {
			found = true
		}
	}
	if !found {
		t.Errorf("medida Unit Sales com uniqueName esperado não encontrada: %+v", meta.Measures)
	}
}

func TestDiscoverUnknownCube404(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	url := ts.URL + "/saiku/api/discover/foodmart/FoodMart/FoodMart/Inexistente/metadata"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", resp.StatusCode)
	}
}
