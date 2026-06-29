package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestMdxParseEndpoint(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"mdx":"SELECT NON EMPTY {[Measures].[Unit Sales]} ON COLUMNS, [Store].[Store Country].Members ON ROWS FROM [Sales] WHERE [Time].[1997]"}`
	resp := postJSON(t, ts.URL+"/saiku/api/mdx/parse", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["cube"] != "Sales" || out["cubeKnown"] != true {
		t.Errorf("cube/cubeKnown inesperados: %v %v", out["cube"], out["cubeKnown"])
	}
	axes, _ := out["axes"].([]any)
	if len(axes) != 2 {
		t.Fatalf("eixos = %d", len(axes))
	}
	a0 := axes[0].(map[string]any)
	if a0["name"] != "COLUMNS" || a0["nonEmpty"] != true {
		t.Errorf("eixo 0 inesperado: %v", a0)
	}
}

func TestMdxParseErrorReturns400(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := postJSON(t, ts.URL+"/saiku/api/mdx/parse", `{"mdx":"SELECT {[x]} ON COLUMNS"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", resp.StatusCode)
	}
}

func TestMdxExecuteWithoutDBReturns503(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"mdx":"SELECT {[Measures].[Unit Sales]} ON COLUMNS, [Store].[Store Country].Members ON ROWS FROM [Sales]"}`
	resp := postJSON(t, ts.URL+"/saiku/api/mdx/execute", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503 (sem banco no teste)", resp.StatusCode)
	}
}
