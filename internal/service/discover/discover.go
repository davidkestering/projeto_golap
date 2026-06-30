// Package discover serve metadados dos schemas carregados, espelhando o papel do
// OlapMetaExplorer do Saiku. Modela a árvore connection → catalog → schema →
// cube sobre um ou mais schemas em memória (catálogo dinâmico: schemas podem ser
// adicionados/removidos em tempo de execução).
package discover

import (
	"fmt"
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

// AddSchema registra um novo schema. Erro se o nome já existir ou se algum nome
// de cubo colidir com um cubo já registrado.
func (s *Service) AddSchema(sc *metadata.Schema) error {
	if sc == nil || strings.TrimSpace(sc.Name) == "" {
		return fmt.Errorf("schema sem nome")
	}
	if len(sc.Cubes) == 0 {
		return fmt.Errorf("schema %q não define cubos", sc.Name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.indexOf(sc.Name) >= 0 {
		return fmt.Errorf("schema %q já existe", sc.Name)
	}
	for _, c := range sc.Cubes {
		for _, existing := range s.schemas {
			if _, ok := existing.FindCube(c.Name); ok {
				return fmt.Errorf("cubo %q já existe no schema %q", c.Name, existing.Name)
			}
		}
	}
	s.schemas = append(s.schemas, sc)
	return nil
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
