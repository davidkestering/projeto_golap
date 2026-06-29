// Package demo embute o schema FoodMart (Mondrian XML v3) usado como cubo
// padrão quando nenhum schema é configurado, espelhando o demo self-contained
// do Saiku. As tabelas referenciadas batem com o FoodMart carregado no Postgres.
package demo

import (
	_ "embed"

	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/schema/mondrian"
)

//go:embed FoodMart.xml
var foodMartXML []byte

// FoodMartXML devolve o conteúdo bruto do schema FoodMart embutido.
func FoodMartXML() []byte { return foodMartXML }

// Schema carrega o schema FoodMart embutido para a IR.
func Schema() (*metadata.Schema, error) {
	return mondrian.LoadBytes(foodMartXML)
}
