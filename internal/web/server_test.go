package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cubodw/internal/config"
)

// newTestServer cria um Server sem DSN (sem pool de Postgres) para exercitar os
// handlers em isolamento.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := NewServer(config.Config{HTTPAddr: ":0", ConnectionName: "foodmart"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return httptest.NewServer(s.http.Handler)
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quero 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status field = %q, quero \"ok\"", body["status"])
	}
}

func TestReadyWithoutDB(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer resp.Body.Close()

	// Sem DSN configurado, /ready deve reportar indisponível.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, quero 503", resp.StatusCode)
	}
}

func TestInfo(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/saiku/api/info")
	if err != nil {
		t.Fatalf("GET /saiku/api/info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quero 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["name"] != "cubodw-engine" {
		t.Fatalf("name = %v, quero cubodw-engine", body["name"])
	}
}
