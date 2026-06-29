// Command cubodw é o ponto de entrada do motor OLAP (estilo Mondrian) em Go.
//
// Subcomandos (Fase 0):
//
//	serve-engine   sobe o serviço HTTP do motor
//	healthcheck    GET no /health (usado pelo HEALTHCHECK do Docker em imagem distroless)
//	version        imprime a versão
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"cubodw/internal/config"
	"cubodw/internal/version"
	"cubodw/internal/web"
)

func main() {
	root := &cobra.Command{
		Use:           "cubodw",
		Short:         "CuboDW — motor OLAP estilo Mondrian, em Go",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(serveEngineCmd(), healthcheckCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

func serveEngineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve-engine",
		Short: "Sobe o serviço HTTP do motor (saúde + descoberta + MDX em construção)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if addr, _ := cmd.Flags().GetString("addr"); addr != "" {
				cfg.HTTPAddr = addr
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srv, err := web.NewServer(cfg)
			if err != nil {
				return err
			}
			return srv.Run(ctx)
		},
	}
	cmd.Flags().String("addr", "", "endereço HTTP (default $CUBODW_HTTP_ADDR ou :8080)")
	return cmd
}

func healthcheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Faz GET no endpoint de saúde; sai 0 se 200, !=0 caso contrário",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url, _ := cmd.Flags().GetString("url")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("status %d", resp.StatusCode)
			}
			return nil
		},
	}
	cmd.Flags().String("url", "http://localhost:8080/health", "URL de saúde")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Imprime a versão",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(version.Version)
		},
	}
}
