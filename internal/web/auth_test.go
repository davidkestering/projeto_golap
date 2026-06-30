package web

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"cubodw/internal/config"
)

func newAuthServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := NewServer(config.Config{HTTPAddr: ":0", ConnectionName: "foodmart", AuthEnabled: true, AuthSecret: "test-secret"})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return httptest.NewServer(s.http.Handler)
}

func clientWithJar(t *testing.T) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func TestAuthProtectsAndLogin(t *testing.T) {
	ts := newAuthServer(t)
	defer ts.Close()
	cli := clientWithJar(t)

	// Sem login, endpoint protegido => 401.
	resp, _ := cli.Get(ts.URL + "/saiku/api/discover")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sem login: status = %d, quero 401", resp.StatusCode)
	}

	// Login admin/admin (semeado) => 200 + cookie.
	resp, err := cli.Post(ts.URL+"/saiku/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"admin"}`))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || body["role"] != "admin" {
		t.Fatalf("login admin falhou: %d %v", resp.StatusCode, body)
	}

	// Agora o endpoint protegido funciona (cookie no jar).
	resp, _ = cli.Get(ts.URL + "/saiku/api/discover")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("com login: status = %d, quero 200", resp.StatusCode)
	}

	// /me reflete o admin.
	resp, _ = cli.Get(ts.URL + "/saiku/api/auth/me")
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body["username"] != "admin" || body["role"] != "admin" {
		t.Errorf("/me inesperado: %v", body)
	}
}

func TestAuthLoginBadPassword(t *testing.T) {
	ts := newAuthServer(t)
	defer ts.Close()
	resp, _ := http.Post(ts.URL+"/saiku/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"errada"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("senha errada: status = %d, quero 401", resp.StatusCode)
	}
}

func TestAuthRegisterCreatesUser(t *testing.T) {
	ts := newAuthServer(t)
	defer ts.Close()
	cli := clientWithJar(t)

	resp, _ := cli.Post(ts.URL+"/saiku/api/auth/register", "application/json", strings.NewReader(`{"username":"david","password":"s3nha"}`))
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || body["role"] != "user" {
		t.Fatalf("registro falhou: %d %v", resp.StatusCode, body)
	}
	// Após registrar, já está logado (cookie) e acessa a API.
	resp, _ = cli.Get(ts.URL + "/saiku/api/ai/cubes")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("acesso após registro: status = %d", resp.StatusCode)
	}
	// Registrar duplicado => 400.
	resp, _ = http.Post(ts.URL+"/saiku/api/auth/register", "application/json", strings.NewReader(`{"username":"david","password":"x"}`))
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("registro duplicado: status = %d, quero 400", resp.StatusCode)
	}
}

func TestAuthAdminOnlyCacheClear(t *testing.T) {
	ts := newAuthServer(t)
	defer ts.Close()
	cli := clientWithJar(t)
	// registra usuário comum
	cli.Post(ts.URL+"/saiku/api/auth/register", "application/json", strings.NewReader(`{"username":"comum","password":"p"}`))
	// usuário comum NÃO pode limpar o cache (admin-only) => 403
	resp, _ := cli.Post(ts.URL+"/saiku/api/cache/clear", "application/json", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cache/clear por user comum: status = %d, quero 403", resp.StatusCode)
	}
}
