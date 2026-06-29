package yaml

import "testing"

const sampleYAML = `
schema: FoodMart
cubes:
  - name: Sales
    fact: sales_fact_1997
    defaultMeasure: Unit Sales
    measures:
      - {name: Unit Sales, column: unit_sales, agg: sum}
      - {name: Store Sales, column: store_sales, agg: sum, format: "#,###.00"}
    dimensions:
      - name: Time
        foreignKey: time_id
        table: time_by_day
        primaryKey: time_id
        type: time
        levels:
          - {name: Year, column: the_year, type: Numeric, levelType: TimeYears}
          - {name: Quarter, column: quarter, levelType: TimeQuarters}
`

func TestLoadYAML(t *testing.T) {
	s, err := LoadBytes([]byte(sampleYAML))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if s.Name != "FoodMart" {
		t.Fatalf("schema = %q", s.Name)
	}
	c, ok := s.FindCube("Sales")
	if !ok {
		t.Fatal("cubo Sales ausente")
	}
	if c.Fact.Table != "sales_fact_1997" || c.DefaultMeasure != "Unit Sales" {
		t.Errorf("fato/defaultMeasure inesperados: %+v", c)
	}
	if len(c.Measures) != 2 {
		t.Fatalf("measures = %d", len(c.Measures))
	}
	d, ok := c.FindDimension("Time")
	if !ok {
		t.Fatal("dim Time ausente")
	}
	if d.Type != "TimeDimension" {
		t.Errorf("Time.type = %q, quero TimeDimension", d.Type)
	}
	if d.ForeignKey != "time_id" {
		t.Errorf("Time.foreignKey = %q", d.ForeignKey)
	}
	if len(d.Hierarchies) != 1 || !d.Hierarchies[0].HasAll {
		t.Fatalf("hierarquia única com All esperada: %+v", d.Hierarchies)
	}
	if d.Hierarchies[0].Table.Table != "time_by_day" {
		t.Errorf("dim table = %q", d.Hierarchies[0].Table.Table)
	}
	if len(d.Hierarchies[0].Levels) != 2 {
		t.Errorf("níveis = %d", len(d.Hierarchies[0].Levels))
	}
}

func TestLoadYAMLRejectsMissingFact(t *testing.T) {
	bad := "schema: X\ncubes:\n  - name: C\n"
	if _, err := LoadBytes([]byte(bad)); err == nil {
		t.Fatal("esperava erro para cubo sem fact")
	}
}
