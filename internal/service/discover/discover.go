// Package discover serve metadados dos schemas carregados, espelhando o papel do
// OlapMetaExplorer do Saiku. Modela a árvore connection → catalog → schema →
// cube sobre um ou mais schemas em memória (catálogo dinâmico: schemas podem ser
// adicionados/removidos em tempo de execução).
package discover

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"cubodw/internal/engine/metadata"
)

// Service expõe consultas de descoberta sobre os schemas carregados.
type Service struct {
	mu         sync.RWMutex
	connection string
	schemas    []*metadata.Schema
}

// New cria o serviço para um nome de conexão lógico e zero ou mais schemas.
func New(connection string, schemas ...*metadata.Schema) *Service {
	s := &Service{connection: connection}
	for _, sc := range schemas {
		if sc != nil {
			s.schemas = append(s.schemas, sc)
		}
	}
	return s
}

// Connection devolve o nome lógico da conexão.
func (s *Service) Connection() string { return s.connection }

// Schemas devolve uma cópia da lista de schemas registrados.
func (s *Service) Schemas() []*metadata.Schema {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*metadata.Schema, len(s.schemas))
	copy(out, s.schemas)
	return out
}

// SchemaName devolve o nome do primeiro schema (compat. com o modelo simples).
func (s *Service) SchemaName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.schemas) == 0 {
		return ""
	}
	return s.schemas[0].Name
}

// Catalog usa o nome do primeiro schema como catálogo (modelo simples).
func (s *Service) Catalog() string { return s.SchemaName() }

// Cubes devolve todos os cubos de todos os schemas.
func (s *Service) Cubes() []*metadata.Cube {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*metadata.Cube
	for _, sc := range s.schemas {
		out = append(out, sc.Cubes...)
	}
	return out
}

// Cube devolve um cubo pelo nome (procura em todos os schemas).
func (s *Service) Cube(name string) (*metadata.Cube, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.schemas {
		if c, ok := sc.FindCube(name); ok {
			return c, true
		}
	}
	return nil, false
}

// SchemaOfCube devolve o nome do schema que contém o cubo (ou "").
func (s *Service) SchemaOfCube(cube string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sc := range s.schemas {
		if _, ok := sc.FindCube(cube); ok {
			return sc.Name
		}
	}
	return ""
}

// HasSchema indica se há um schema com esse nome (case-insensitive).
func (s *Service) HasSchema(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.indexOf(name) >= 0
}

// RegisterSchema registra um schema aplicando as regras de nome (ver
// normalizeName): nomes de schema e de cubos viram MAIÚSCULAS, sem espaços nem
// caracteres especiais, e colisões recebem sufixo incremental V1/V2/V3…. Nunca
// falha por colisão — sempre encontra um nome livre. Muta sc com os nomes finais.
func (s *Service) RegisterSchema(sc *metadata.Schema) (*metadata.Schema, error) {
	if sc == nil {
		return nil, fmt.Errorf("schema nulo")
	}
	if len(sc.Cubes) == 0 {
		return nil, fmt.Errorf("schema %q não define cubos", sc.Name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	schemaName, cubeNames := s.resolveNamesLocked(sc)
	sc.Name = schemaName
	for i, c := range sc.Cubes {
		c.Name = cubeNames[i]
	}
	s.schemas = append(s.schemas, sc)
	return sc, nil
}

// PreviewRegister calcula (sem registrar) os nomes finais que o schema receberia
// se fosse adicionado agora — útil para o dry-run de validação.
func (s *Service) PreviewRegister(sc *metadata.Schema) (schemaName string, cubeNames []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveNamesLocked(sc)
}

// resolveNamesLocked devolve o nome de schema e os nomes de cubos finais
// (normalizados + desambiguados contra o que já está registrado e entre si).
func (s *Service) resolveNamesLocked(sc *metadata.Schema) (schemaName string, cubeNames []string) {
	schemaTaken := map[string]bool{}
	for _, e := range s.schemas {
		schemaTaken[strings.ToUpper(e.Name)] = true
	}
	schemaName = uniqueName(normalizeOrDefault(sc.Name, "SCHEMA"), schemaTaken)

	cubeTaken := map[string]bool{}
	for _, e := range s.schemas {
		for _, c := range e.Cubes {
			cubeTaken[strings.ToUpper(c.Name)] = true
		}
	}
	cubeNames = make([]string, len(sc.Cubes))
	for i, c := range sc.Cubes {
		n := uniqueName(normalizeOrDefault(c.Name, "CUBE"), cubeTaken)
		cubeTaken[n] = true
		cubeNames[i] = n
	}
	return schemaName, cubeNames
}

// normalizeName mantém apenas letras/dígitos ASCII e devolve em MAIÚSCULAS
// (sem espaços nem caracteres especiais — "tudo junto").
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - ('a' - 'A'))
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeOrDefault(s, def string) string {
	if n := normalizeName(s); n != "" {
		return n
	}
	return def
}

// uniqueName devolve base se livre; senão base+"V1", base+"V2", … até um livre.
func uniqueName(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for n := 1; ; n++ {
		cand := base + "V" + strconv.Itoa(n)
		if !taken[cand] {
			return cand
		}
	}
}

// RemoveSchema remove um schema pelo nome; devolve false se não existir.
func (s *Service) RemoveSchema(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(name)
	if i < 0 {
		return false
	}
	s.schemas = append(s.schemas[:i], s.schemas[i+1:]...)
	return true
}

func (s *Service) indexOf(name string) int {
	for i, sc := range s.schemas {
		if strings.EqualFold(sc.Name, name) {
			return i
		}
	}
	return -1
}
