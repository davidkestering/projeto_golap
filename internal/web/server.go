// Package web expõe o servidor HTTP do motor.
//
// Na Fase 0 ele oferece apenas saúde/prontidão e um endpoint de info. As
// surfaces de descoberta de metadados e execução de MDX serão adicionadas nas
// fases seguintes, montadas sob /saiku/api/* para compatibilidade futura com a
// UI do Saiku.
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cubodw/internal/config"
	"cubodw/internal/demo"
	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/schema/mondrian"
	"cubodw/internal/engine/schema/yaml"
	"cubodw/internal/service/discover"
	"cubodw/internal/service/queryexec"
	"cubodw/internal/version"
)

// Server encapsula o HTTP server, o pool de conexões e o serviço de descoberta.
type Server struct {
	cfg      config.Config
	pool     *pgxpool.Pool
	http     *http.Server
	log      *slog.Logger
	discover *discover.Service
}

// NewServer constrói o servidor: carrega o schema (config ou demo embutido),
// monta o serviço de descoberta e, se houver DSN, cria o pool de Postgres
// (a conectividade só é exercida em /ready).
func NewServer(cfg config.Config) (*Server, error) {
	log := slog.Default()

	schema, err := loadSchema(cfg.SchemaPath)
	if err != nil {
		return nil, err
	}
	log.Info("schema carregado", "name", schema.Name, "cubos", len(schema.Cubes))

	var pool *pgxpool.Pool
	if cfg.PostgresDSN != "" {
		p, err := pgxpool.New(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		pool = p
	}

	s := &Server{
		cfg:      cfg,
		pool:     pool,
		log:      log,
		discover: discover.New(cfg.ConnectionName, schema),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.HandleFunc("GET /saiku/api/info", s.handleInfo)
	exec := queryexec.New(pool)
	(&discoverAPI{svc: s.discover}).register(mux)
	(&queryAPI{discover: s.discover, exec: exec}).register(mux)
	(&mdxAPI{discover: s.discover, exec: exec}).register(mux)
	(&aiAPI{discover: s.discover, exec: exec}).register(mux)

	s.http = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s, nil
}

// Run inicia o servidor e bloqueia até o contexto ser cancelado, fazendo um
// shutdown gracioso.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("motor HTTP iniciando", "addr", s.cfg.HTTPAddr, "db", s.pool != nil)
		errCh <- s.http.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if s.pool != nil {
			s.pool.Close()
		}
		s.log.Info("motor HTTP encerrando")
		return s.http.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// handleHealth é o probe de liveness: sempre 200 enquanto o processo responde.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReady é o probe de readiness: verifica a conexão com o Postgres.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.pool == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "no-db",
			"detail": "PG DSN não configurado (defina CUBODW_PG_DSN)",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "db-unreachable",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// handleInfo devolve metadados básicos do serviço.
func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":       "cubodw-engine",
		"version":    version.Version,
		"engine":     "mondrian-go (em construção)",
		"connection": s.discover.Connection(),
		"schema":     s.discover.SchemaName(),
		"cubes":      len(s.discover.Cubes()),
	})
}

// loadSchema carrega o schema do caminho indicado (.yml/.yaml = YAML; demais =
// Mondrian XML) ou, se vazio, o FoodMart embutido (demo).
func loadSchema(path string) (*metadata.Schema, error) {
	if path == "" {
		return demo.Schema()
	}
	switch {
	case strings.HasSuffix(path, ".yml"), strings.HasSuffix(path, ".yaml"):
		return yaml.LoadFile(path)
	case strings.HasSuffix(path, ".xml"):
		return mondrian.LoadFile(path)
	default:
		return nil, fmt.Errorf("schema %q: extensão não suportada (use .xml, .yml ou .yaml)", path)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
