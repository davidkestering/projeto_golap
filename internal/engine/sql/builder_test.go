package sql_test

import (
	"reflect"
	"testing"

	"cubodw/internal/demo"
	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	enginesql "cubodw/internal/engine/sql"
)

func salesCube(t *testing.T) *metadata.Cube {
	t.Helper()
	s, err := demo.Schema()
	if err != nil {
		t.Fatalf("demo.Schema: %v", err)
	}
	c, ok := s.FindCube("Sales")
	if !ok {
		t.Fatal("cubo Sales ausente")
	}
	return c
}

func TestBuildSimpleAggregation(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Rows:     []query.LevelRef{{Dimension: "Time", Level: "Year"}},
		Measures: []string{"Unit Sales"},
	}
	st, err := enginesql.Build(enginesql.Postgres{}, c, q)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := `SELECT "Time"."the_year", sum("f"."unit_sales")::float8
FROM "sales_fact_1997" AS "f"
JOIN "time_by_day" AS "Time" ON "f"."time_id" = "Time"."time_id"
GROUP BY "Time"."the_year"
ORDER BY 1`
	if st.SQL != want {
		t.Errorf("SQL inesperada:\n--- got ---\n%s\n--- want ---\n%s", st.SQL, want)
	}
	if len(st.Columns) != 2 || st.Columns[0].Kind != "level" || st.Columns[1].Kind != "measure" {
		t.Errorf("colunas inesperadas: %+v", st.Columns)
	}
}

func TestBuildWithFilter(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Measures: []string{"Unit Sales"},
		Filters:  []query.Filter{{Dimension: "Customers", Level: "Country", Members: []string{"USA"}}},
	}
	st, err := enginesql.Build(enginesql.Postgres{}, c, q)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := `SELECT sum("f"."unit_sales")::float8
FROM "sales_fact_1997" AS "f"
JOIN "customer" AS "Customers" ON "f"."customer_id" = "Customers"."customer_id"
WHERE ("Customers"."country")::text = ANY($1)`
	if st.SQL != want {
		t.Errorf("SQL inesperada:\n--- got ---\n%s\n--- want ---\n%s", st.SQL, want)
	}
	if len(st.Args) != 1 || !reflect.DeepEqual(st.Args[0], []string{"USA"}) {
		t.Errorf("args inesperados: %#v", st.Args)
	}
}

func TestBuildSnowflakeRejected(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Rows:     []query.LevelRef{{Dimension: "Product", Level: "Product Family"}},
		Measures: []string{"Unit Sales"},
	}
	if _, err := enginesql.Build(enginesql.Postgres{}, c, q); err == nil {
		t.Fatal("esperava erro para dimensão snowflake (Product/Join)")
	}
}

func TestBuildUnknownMeasure(t *testing.T) {
	c := salesCube(t)
	q := query.Query{Cube: "Sales", Measures: []string{"Inexistente"}}
	if _, err := enginesql.Build(enginesql.Postgres{}, c, q); err == nil {
		t.Fatal("esperava erro para medida inexistente")
	}
}
