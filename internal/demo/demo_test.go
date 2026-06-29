package demo

import "testing"

// TestEmbeddedFoodMartLoads garante que o FoodMart embutido parseia e tem os
// cubos esperados — protege contra regressões no loader Mondrian contra o
// schema real (não só XML sintético).
func TestEmbeddedFoodMartLoads(t *testing.T) {
	s, err := Schema()
	if err != nil {
		t.Fatalf("Schema(): %v", err)
	}
	if s.Name != "FoodMart" {
		t.Fatalf("schema = %q", s.Name)
	}
	// FoodMart.xml define 7 cubos (Sales, Sales Scenario, Warehouse, Store, HR,
	// Sales Ragged, Sales 2).
	if len(s.Cubes) != 7 {
		t.Fatalf("cubos = %d, quero 7", len(s.Cubes))
	}

	sales, ok := s.FindCube("Sales")
	if !ok {
		t.Fatal("cubo Sales ausente")
	}
	if sales.Fact.Table != "sales_fact_1997" {
		t.Errorf("Sales.fact = %q", sales.Fact.Table)
	}
	if _, ok := sales.FindMeasure("Unit Sales"); !ok {
		t.Error("medida Unit Sales ausente no Sales")
	}
	// Dimensão compartilhada Time resolvida com foreignKey.
	time, ok := sales.FindDimension("Time")
	if !ok {
		t.Fatal("dim Time ausente no Sales")
	}
	if time.ForeignKey != "time_id" {
		t.Errorf("Time.foreignKey = %q", time.ForeignKey)
	}
	if len(time.Hierarchies) == 0 || len(time.Hierarchies[0].Levels) == 0 {
		t.Error("Time sem hierarquia/níveis resolvidos a partir da dimensão compartilhada")
	}
}
