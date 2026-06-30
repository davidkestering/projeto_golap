// Package config carrega a configuração do serviço a partir de variáveis de
// ambiente (estilo 12-factor, adequado a containers).
package config

import (
	"os"
	"strconv"
	"strings"
)

// Config reúne os parâmetros de execução do motor.
type Config struct {
	// HTTPAddr é o endereço de escuta do servidor HTTP (ex.: ":8080").
	HTTPAddr string
	// PostgresDSN é a string de conexão com o data warehouse Postgres.
	// Vazia desabilita a conexão (o endpoint /ready reporta no-db).
	PostgresDSN string
	// SchemaPath é o caminho do schema a carregar (.xml = Mondrian, .yml/.yaml =
	// autoria YAML). Vazio usa o schema FoodMart embutido (demo).
	SchemaPath string
	// ConnectionName é o nome lógico da conexão exposto na descoberta.
	ConnectionName string
	// CacheSize é o nº máximo de resultados em cache (0 desabilita).
	CacheSize int
	// AuthEnabled liga a autenticação (default true).
	AuthEnabled bool
	// AuthSecret assina os cookies de sessão (HMAC). Vazio usa um default de dev.
	AuthSecret string
	// UsersFile persiste os usuários em JSON; vazio = só em memória.
	UsersFile string
}

// FromEnv monta a Config a partir do ambiente, aplicando defaults sensatos.
//
//	CUBODW_HTTP_ADDR  endereço HTTP            (default ":8080")
//	CUBODW_PG_DSN     DSN do Postgres          (fallback: PG_DSN)
func FromEnv() Config {
	return Config{
		HTTPAddr:       getenv("CUBODW_HTTP_ADDR", ":8080"),
		PostgresDSN:    firstNonEmpty(os.Getenv("CUBODW_PG_DSN"), os.Getenv("PG_DSN")),
		SchemaPath:     os.Getenv("CUBODW_SCHEMA"),
		ConnectionName: getenv("CUBODW_CONNECTION", "foodmart"),
		CacheSize:      getenvInt("CUBODW_CACHE_SIZE", 256),
		AuthEnabled:    getenvBool("CUBODW_AUTH_ENABLED", true),
		AuthSecret:     os.Getenv("CUBODW_AUTH_SECRET"),
		UsersFile:      os.Getenv("CUBODW_USERS_FILE"),
	}
}

func getenvBool(key string, def bool) bool {
	switch strings.ToLower(os.Getenv(key)) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	default:
		return def
	}
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
