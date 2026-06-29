package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAICubes(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/saiku/api/ai/cubes")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var cubes []map[string]any
	json.NewDecoder(resp.Body).Decode(&cubes)
	if len(cubes) != 7 {
		t.Fatalf("cubos = %d, quero 7", len(cubes))
	}
	if cubes[0]["cubeName"] == nil || cubes[0]["measureCount"] == nil {
		t.Errorf("DTO incompleto: %+v", cubes[0])
	}
}

func TestAISchema(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/saiku/api/ai/schema/Sales")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var s map[string]any
	json.NewDecoder(resp.Body).Decode(&s)
	if s["cubeName"] != "Sales" {
		t.Errorf("cubeName = %v", s["cubeName"])
	}
	if ms, _ := s["measures"].([]any); len(ms) == 0 {
		t.Error("schema sem medidas")
	}
	if ds, _ := s["dimensions"].([]any); len(ds) == 0 {
		t.Error("schema sem dimensões")
	}
}

func TestAIQueryValidationBadMeasure(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp := postJSON(t, ts.URL+"/saiku/api/ai/query", `{"cube":"Sales","measures":["Inexistente"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", resp.StatusCode)
	}
	var e map[string]any
	json.NewDecoder(resp.Body).Decode(&e)
	if e["status"] != "VALIDATION_ERROR" || e["field"] != "measures" {
		t.Errorf("envelope inesperado: %+v", e)
	}
	if av, _ := e["available"].([]any); len(av) == 0 {
		t.Error("envelope sem candidatos 'available'")
	}
}

func TestAIQueryUnknownCube(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp := postJSON(t, ts.URL+"/saiku/api/ai/query", `{"cube":"Nope","measures":["Unit Sales"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", resp.StatusCode)
	}
}

func TestAIQueryBadLevel(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp := postJSON(t, ts.URL+"/saiku/api/ai/query",
		`{"cube":"Sales","measures":["Unit Sales"],"rows":[{"dimension":"Store","level":"Nope"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", resp.StatusCode)
	}
	var e map[string]any
	json.NewDecoder(resp.Body).Decode(&e)
	if e["field"] != "rows/columns.level" {
		t.Errorf("field = %v, quero rows/columns.level", e["field"])
	}
}
