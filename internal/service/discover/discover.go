// Package discover serve metadados do schema carregado, espelhando o papel do
// OlapMetaExplorer do Saiku. Modela a árvore connection → catalog → schema →
// cube sobre um único schema em memória.
package discover

import "cubodw/internal/engine/metadata"

// Service expõe consultas de descoberta sobre um schema carregado.
type Service struct {
	connection string
	schema     *metadata.Schema
}

// New cria o serviço para um schema, sob um nome de conexão lógico.
func New(connection string, schema *metadata.Schema) *Service {
	return &Service{connection: connection, schema: schema}
}

// Connection devolve o nome lógico da conexão.
func (s *Service) Connection() string { return s.connection }

// Catalog/Schema usam o nome do schema como catálogo e schema (modelo simples).
func (s *Service) Catalog() string { return s.schema.Name }

// SchemaName devolve o nome do schema.
func (s *Service) SchemaName() string { return s.schema.Name }

// Schema devolve a IR completa.
func (s *Service) Schema() *metadata.Schema { return s.schema }

// Cubes devolve os cubos do schema.
func (s *Service) Cubes() []*metadata.Cube { return s.schema.Cubes }

// Cube devolve um cubo pelo nome.
func (s *Service) Cube(name string) (*metadata.Cube, bool) {
	return s.schema.FindCube(name)
}
