package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cubodw/internal/config"
)

const testCubeYAML = `schema: TestSchema
cubes:
  - name: TestCube
    fact: some_fact
    measures:
      - {name: M, column: c, agg: sum}
    dimensions:
      - name: D
        foreignKey: d_id
        table: d
        primaryKey: d_id
        levels:
          - {name: L, column: l}
`

func postSchema(ts, path, yaml string) (*http.Response, error) {
	body, _ := json.Marshal(map[string]string{"content": yaml})
	return http.Post(ts+path, "application/json", strings.NewReader(string(body)))
}

// addedNames adiciona um schema e devolve os nomes finais (schema, primeiro cubo).
func addedNames(t *testing.T, ts, yaml string) (string, string) {
	t.Helper()
	resp, _ := postSchema(ts, "/saiku/api/schemas", yaml)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: status = %d, quero 201", resp.StatusCode)
	}
	var b struct {
		Schema struct {
			Name  string `json:"name"`
			Cubes []struct {
				Name string `json:"name"`
			} `json:"cubes"`
		} `json:"schema"`
	}
	json.NewDecoder(resp.Body).Decode(&b)
	return b.Schema.Name, b.Schema.Cubes[0].Name
}

func TestSchemasAddListDelete(t *testing.T) {
	ts := newTestServer(t) // auth desligada neste helper
	defer ts.Close()

	// validar (dry-run) já devolve o nome FINAL normalizado (MAIÚSCULO).
	resp, _ := postSchema(ts.URL, "/saiku/api/schemas/validate", testCubeYAML)
	var vb map[string]any
	json.NewDecoder(resp.Body).Decode(&vb)
	resp.Body.Close()
	if vb["valid"] != true {
		t.Fatalf("validate: %v", vb)
	}

	schemaName, cubeName := addedNames(t, ts.URL, testCubeYAML)
	if schemaName != "TESTSCHEMA" || cubeName != "TESTCUBE" {
		t.Fatalf("nomes normalizados inesperados: schema=%q cubo=%q", schemaName, cubeName)
	}

	// aparece na descoberta com o nome normalizado
	resp, _ = http.Get(ts.URL + "/saiku/api/ai/cubes")
	var cubes []map[string]any
	json.NewDecoder(resp.Body).Decode(&cubes)
	resp.Body.Close()
	if !hasCube(cubes, "TESTCUBE") {
		t.Errorf("TESTCUBE não apareceu na descoberta: %v", cubes)
	}

	// remover pelo nome final
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/saiku/api/schemas/"+schemaName, nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: status = %d, quero 200", resp.StatusCode)
	}
}

func TestSchemasNormalizeAndVersion(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// nome com espaços/caracteres especiais => MAIÚSCULAS, tudo junto.
	yaml := strings.Replace(testCubeYAML, "name: TestCube", "name: \"Vendas Mensais!@ 2024\"", 1)
	yaml = strings.Replace(yaml, "schema: TestSchema", "schema: \"Meu Schema\"", 1)
	sname, cname := addedNames(t, ts.URL, yaml)
	if sname != "MEUSCHEMA" || cname != "VENDASMENSAIS2024" {
		t.Fatalf("normalização inesperada: schema=%q cubo=%q", sname, cname)
	}

	// colisão com o FoodMart: cubo "Sales" => "SALES" já existe => "SALESV1".
	collide := strings.Replace(testCubeYAML, "name: TestCube", "name: Sales", 1)
	collide = strings.Replace(collide, "schema: TestSchema", "schema: S1", 1)
	_, c1 := addedNames(t, ts.URL, collide)
	if c1 != "SALESV1" {
		t.Errorf("primeira colisão: cubo = %q, quero SALESV1", c1)
	}
	// de novo => SALESV2; e mais uma => SALESV3 (incremental, mesmo já existindo V1).
	collide2 := strings.Replace(collide, "schema: S1", "schema: S2", 1)
	_, c2 := addedNames(t, ts.URL, collide2)
	if c2 != "SALESV2" {
		t.Errorf("segunda colisão: cubo = %q, quero SALESV2", c2)
	}
	collide3 := strings.Replace(collide, "schema: S1", "schema: S3", 1)
	_, c3 := addedNames(t, ts.URL, collide3)
	if c3 != "SALESV3" {
		t.Errorf("terceira colisão: cubo = %q, quero SALESV3", c3)
	}
}

func TestSchemasInvalid(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	resp, _ := postSchema(ts.URL, "/saiku/api/schemas", "isto não é um schema válido :::")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("inválido: status = %d, quero 400", resp.StatusCode)
	}
}

func TestSchemasAdminOnly(t *testing.T) {
	ts := newAuthServer(t) // auth ligada
	defer ts.Close()
	cli := clientWithJar(t)
	cli.Post(ts.URL+"/saiku/api/auth/register", "application/json",
		strings.NewReader(`{"username":"comum","password":"p"}`))

	body, _ := json.Marshal(map[string]string{"content": testCubeYAML})
	resp, _ := cli.Post(ts.URL+"/saiku/api/schemas", "application/json", strings.NewReader(string(body)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user comum adicionando: status = %d, quero 403", resp.StatusCode)
	}
}

func TestSchemasPersistence(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{HTTPAddr: ":0", ConnectionName: "foodmart", SchemasDir: dir}

	// servidor 1: adiciona um cubo -> deve persistir um arquivo no dir
	s1, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer 1: %v", err)
	}
	ts1 := httptest.NewServer(s1.http.Handler)
	resp, _ := postSchema(ts1.URL, "/saiku/api/schemas", testCubeYAML)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: status = %d", resp.StatusCode)
	}
	ts1.Close()

	files, _ := os.ReadDir(dir)
	if len(files) == 0 {
		t.Fatal("nenhum schema persistido no dir")
	}

	// servidor 2 (novo processo simulado) no MESMO dir: deve recarregar o cubo
	s2, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer 2: %v", err)
	}
	ts2 := httptest.NewServer(s2.http.Handler)
	defer ts2.Close()
	resp, _ = http.Get(ts2.URL + "/saiku/api/ai/cubes")
	var cubes []map[string]any
	json.NewDecoder(resp.Body).Decode(&cubes)
	resp.Body.Close()
	if !hasCube(cubes, "TESTCUBE") {
		t.Errorf("cubo não sobreviveu ao restart: %v", cubes)
	}
}

func hasCube(cubes []map[string]any, name string) bool {
	for _, c := range cubes {
		if c["cubeName"] == name {
			return true
		}
	}
	return false
}
