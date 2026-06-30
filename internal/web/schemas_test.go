package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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

func postSchema(ts string, path, yaml string) (*http.Response, error) {
	body, _ := json.Marshal(map[string]string{"content": yaml})
	return http.Post(ts+path, "application/json", strings.NewReader(string(body)))
}

func TestSchemasValidateAddListDelete(t *testing.T) {
	ts := newTestServer(t) // auth desligada neste helper
	defer ts.Close()

	// validar (dry-run)
	resp, _ := postSchema(ts.URL, "/saiku/api/schemas/validate", testCubeYAML)
	var vb map[string]any
	json.NewDecoder(resp.Body).Decode(&vb)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || vb["valid"] != true {
		t.Fatalf("validate: %d %v", resp.StatusCode, vb)
	}

	// adicionar
	resp, _ = postSchema(ts.URL, "/saiku/api/schemas", testCubeYAML)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add: status = %d, quero 201", resp.StatusCode)
	}

	// agora aparece na descoberta
	resp, _ = http.Get(ts.URL + "/saiku/api/ai/cubes")
	var cubes []map[string]any
	json.NewDecoder(resp.Body).Decode(&cubes)
	resp.Body.Close()
	if !hasCube(cubes, "TestCube") {
		t.Errorf("TestCube não apareceu na descoberta: %v", cubes)
	}

	// remover
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/saiku/api/schemas/TestSchema", nil)
	resp, _ = http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: status = %d, quero 200", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/saiku/api/ai/cubes")
	json.NewDecoder(resp.Body).Decode(&cubes)
	resp.Body.Close()
	if hasCube(cubes, "TestCube") {
		t.Errorf("TestCube ainda presente após remoção")
	}
}

func TestSchemasInvalidAndCollision(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// conteúdo inválido => 400
	resp, _ := postSchema(ts.URL, "/saiku/api/schemas", "isto não é um schema válido :::")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("inválido: status = %d, quero 400", resp.StatusCode)
	}

	// cubo cujo nome colide com o FoodMart (Sales) => 409
	collide := strings.Replace(testCubeYAML, "name: TestCube", "name: Sales", 1)
	collide = strings.Replace(collide, "schema: TestSchema", "schema: Other", 1)
	resp, _ = postSchema(ts.URL, "/saiku/api/schemas", collide)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("colisão: status = %d, quero 409", resp.StatusCode)
	}
}

func TestSchemasAdminOnly(t *testing.T) {
	ts := newAuthServer(t) // auth ligada
	defer ts.Close()
	cli := clientWithJar(t)
	cli.Post(ts.URL+"/saiku/api/auth/register", "application/json",
		strings.NewReader(`{"username":"comum","password":"p"}`))

	// usuário comum não pode adicionar => 403
	body, _ := json.Marshal(map[string]string{"content": testCubeYAML})
	resp, _ := cli.Post(ts.URL+"/saiku/api/schemas", "application/json", strings.NewReader(string(body)))
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("user comum adicionando: status = %d, quero 403", resp.StatusCode)
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
