package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cubodw/internal/auth"
)

const (
	sessionCookie = "cubodw_session"
	sessionTTL    = 12 * time.Hour
)

// authAPI cuida do registro/login e do middleware de proteção da API.
type authAPI struct {
	store   *auth.Store
	secret  []byte
	enabled bool
}

func (a *authAPI) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /saiku/api/auth/register", a.handleRegister)
	mux.HandleFunc("POST /saiku/api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /saiku/api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /saiku/api/auth/me", a.handleMe)
}

type credsRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *authAPI) handleRegister(w http.ResponseWriter, r *http.Request) {
	var c credsRequest
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido"})
		return
	}
	if err := a.store.Register(c.Username, c.Password, auth.RoleUser); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	u := auth.User{Username: c.Username, Role: auth.RoleUser}
	a.setSession(w, u)
	writeJSON(w, http.StatusCreated, map[string]any{"username": u.Username, "role": u.Role})
}

func (a *authAPI) handleLogin(w http.ResponseWriter, r *http.Request) {
	var c credsRequest
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "JSON inválido"})
		return
	}
	u, ok := a.store.Authenticate(c.Username, c.Password)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "usuário ou senha inválidos"})
		return
	}
	a.setSession(w, u)
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "role": u.Role})
}

func (a *authAPI) handleLogout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (a *authAPI) handleMe(w http.ResponseWriter, r *http.Request) {
	if !a.enabled {
		writeJSON(w, http.StatusOK, map[string]any{"username": "guest", "role": auth.RoleAdmin, "authDisabled": true})
		return
	}
	u, ok := a.userFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "não autenticado"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": u.Username, "role": u.Role})
}

func (a *authAPI) setSession(w http.ResponseWriter, u auth.User) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    auth.SignToken(a.secret, u, sessionTTL),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (a *authAPI) userFromRequest(r *http.Request) (auth.User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.User{}, false
	}
	return auth.VerifyToken(a.secret, c.Value)
}

// middleware protege os endpoints quando a auth está ligada. Rotas públicas:
// /health, /ready, /saiku/api/info, /saiku/api/auth/* e a UI (/ui/*).
func (a *authAPI) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.enabled || isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		u, ok := a.userFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "autenticação requerida"})
			return
		}
		if isAdminOnly(r.Method, r.URL.Path) && u.Role != auth.RoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requer papel admin"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isPublicPath(path string) bool {
	switch path {
	case "/health", "/ready", "/saiku/api/info":
		return true
	}
	return strings.HasPrefix(path, "/saiku/api/auth/") ||
		path == "/ui" || strings.HasPrefix(path, "/ui/")
}

func isAdminOnly(method, path string) bool {
	return method == http.MethodPost && path == "/saiku/api/cache/clear"
}
