package web

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUIIndexServed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui/")
	if err != nil {
		t.Fatalf("GET /ui/: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "CuboDW") || !strings.Contains(string(b), "app.js") {
		t.Errorf("index.html inesperado")
	}
}

func TestUIAssetsServed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	for _, f := range []string{"app.js", "app.css"} {
		resp, err := http.Get(ts.URL + "/ui/" + f)
		if err != nil {
			t.Fatalf("GET %s: %v", f, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d", f, resp.StatusCode)
		}
	}
}
