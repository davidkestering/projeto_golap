package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func TestQueryPreview(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"cube":"Sales","rows":[{"dimension":"Time","level":"Year"}],"measures":["Unit Sales"]}`
	resp := postJSON(t, ts.URL+"/saiku/api/query/preview", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quero 200", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["status"] != "PREVIEW" {
		t.Errorf("status = %v", out["status"])
	}
	sql, _ := out["sql"].(string)
	if !strings.Contains(sql, "GROUP BY") || !strings.Contains(sql, "time_by_day") {
		t.Errorf("SQL inesperada: %q", sql)
	}
}

func TestQueryWithoutDBReturns503(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// O servidor de teste não tem pool de Postgres → execução real indisponível.
	body := `{"cube":"Sales","rows":[{"dimension":"Time","level":"Year"}],"measures":["Unit Sales"]}`
	resp := postJSON(t, ts.URL+"/saiku/api/query", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", resp.StatusCode)
	}
}

func TestQueryBadMeasureReturns400(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"cube":"Sales","measures":["Inexistente"]}`
	resp := postJSON(t, ts.URL+"/saiku/api/query/preview", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, quero 400", resp.StatusCode)
	}
}

func TestQueryUnknownCubeReturns404(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	body := `{"cube":"NaoExiste","measures":["Unit Sales"]}`
	resp := postJSON(t, ts.URL+"/saiku/api/query/preview", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, quero 404", resp.StatusCode)
	}
}
