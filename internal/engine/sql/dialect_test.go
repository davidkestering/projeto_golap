package sql_test

import (
	"reflect"
	"testing"

	"cubodw/internal/engine/query"
	enginesql "cubodw/internal/engine/sql"
)

func TestMySQLBuild(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Measures: []string{"Unit Sales"},
		Filters:  []query.Filter{{Dimension: "Customers", Level: "Country", Members: []string{"USA"}}},
	}
	st, err := enginesql.Build(enginesql.MySQL{}, c, q)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := "SELECT CAST(sum(`f`.`unit_sales`) AS DOUBLE)\n" +
		"FROM `sales_fact_1997` AS `f`\n" +
		"JOIN `customer` AS `Customers` ON `f`.`customer_id` = `Customers`.`customer_id`\n" +
		"WHERE CAST(`Customers`.`country` AS CHAR) IN (?)"
	if st.SQL != want {
		t.Errorf("SQL MySQL inesperada:\n--- got ---\n%s\n--- want ---\n%s", st.SQL, want)
	}
	// MySQL usa N args escalares (não um array como o Postgres).
	if !reflect.DeepEqual(st.Args, []any{"USA"}) {
		t.Errorf("args = %#v, quero [USA] escalar", st.Args)
	}
}

func TestDuckDBBuild(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Rows:     []query.LevelRef{{Dimension: "Time", Level: "Year"}},
		Measures: []string{"Unit Sales"},
		Filters:  []query.Filter{{Dimension: "Customers", Level: "Country", Members: []string{"USA"}}},
	}
	st, err := enginesql.Build(enginesql.DuckDB{}, c, q)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := `SELECT "Time"."the_year", CAST(sum("f"."unit_sales") AS DOUBLE)
FROM "sales_fact_1997" AS "f"
JOIN "time_by_day" AS "Time" ON "f"."time_id" = "Time"."time_id"
JOIN "customer" AS "Customers" ON "f"."customer_id" = "Customers"."customer_id"
WHERE CAST("Customers"."country" AS VARCHAR) IN (?)
GROUP BY "Time"."the_year"
ORDER BY 1`
	if st.SQL != want {
		t.Errorf("SQL DuckDB inesperada:\n--- got ---\n%s\n--- want ---\n%s", st.SQL, want)
	}
	if !reflect.DeepEqual(st.Args, []any{"USA"}) {
		t.Errorf("args = %#v", st.Args)
	}
}

func TestSQLServerDrillthroughTop(t *testing.T) {
	c := salesCube(t)
	st, err := enginesql.BuildDrillthrough(enginesql.SQLServer{}, c,
		[]query.Filter{{Dimension: "Store", Level: "Store State", Members: []string{"CA"}}}, 5)
	if err != nil {
		t.Fatalf("BuildDrillthrough: %v", err)
	}
	// SQL Server: TOP n após SELECT, colchetes, @pN, CAST AS FLOAT, sem LIMIT.
	if !contains(st.SQL, "SELECT TOP 5 ") {
		t.Errorf("faltou TOP 5: %s", st.SQL)
	}
	if !contains(st.SQL, "CAST([Store].[store_state] AS NVARCHAR(4000)) IN (@p1)") {
		t.Errorf("predicado IN/colchetes/@p1 inesperado: %s", st.SQL)
	}
	if contains(st.SQL, "LIMIT") {
		t.Errorf("SQL Server não deve usar LIMIT: %s", st.SQL)
	}
}

func TestSQLServerBuild(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Rows:     []query.LevelRef{{Dimension: "Time", Level: "Year"}},
		Measures: []string{"Unit Sales"},
		Filters:  []query.Filter{{Dimension: "Customers", Level: "Country", Members: []string{"USA"}}},
	}
	st, err := enginesql.Build(enginesql.SQLServer{}, c, q)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := "SELECT [Time].[the_year], CAST(sum([f].[unit_sales]) AS FLOAT)\n" +
		"FROM [sales_fact_1997] AS [f]\n" +
		"JOIN [time_by_day] AS [Time] ON [f].[time_id] = [Time].[time_id]\n" +
		"JOIN [customer] AS [Customers] ON [f].[customer_id] = [Customers].[customer_id]\n" +
		"WHERE CAST([Customers].[country] AS NVARCHAR(4000)) IN (@p1)\n" +
		"GROUP BY [Time].[the_year]\n" +
		"ORDER BY 1"
	if st.SQL != want {
		t.Errorf("SQL SQL Server inesperada:\n--- got ---\n%s\n--- want ---\n%s", st.SQL, want)
	}
}

func TestOracleBuild(t *testing.T) {
	c := salesCube(t)
	q := query.Query{
		Cube:     "Sales",
		Rows:     []query.LevelRef{{Dimension: "Time", Level: "Year"}},
		Measures: []string{"Unit Sales"},
		Filters:  []query.Filter{{Dimension: "Customers", Level: "Country", Members: []string{"USA"}}},
	}
	st, err := enginesql.Build(enginesql.Oracle{}, c, q)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Oracle: alias de tabela SEM AS, bind :1, VARCHAR2/BINARY_DOUBLE.
	want := `SELECT "Time"."the_year", CAST(sum("f"."unit_sales") AS BINARY_DOUBLE)
FROM "sales_fact_1997" "f"
JOIN "time_by_day" "Time" ON "f"."time_id" = "Time"."time_id"
JOIN "customer" "Customers" ON "f"."customer_id" = "Customers"."customer_id"
WHERE CAST("Customers"."country" AS VARCHAR2(4000)) IN (:1)
GROUP BY "Time"."the_year"
ORDER BY 1`
	if st.SQL != want {
		t.Errorf("SQL Oracle inesperada:\n--- got ---\n%s\n--- want ---\n%s", st.SQL, want)
	}
}

func TestOracleDrillthroughFetchFirst(t *testing.T) {
	c := salesCube(t)
	st, err := enginesql.BuildDrillthrough(enginesql.Oracle{}, c,
		[]query.Filter{{Dimension: "Store", Level: "Store State", Members: []string{"CA"}}}, 5)
	if err != nil {
		t.Fatalf("BuildDrillthrough: %v", err)
	}
	if !contains(st.SQL, "FETCH FIRST 5 ROWS ONLY") {
		t.Errorf("faltou FETCH FIRST: %s", st.SQL)
	}
	// Oracle não usa LIMIT/TOP, nem "AS" antes de alias de tabela (AS "..."); o
	// "AS" dentro de CAST(... AS TIPO) é permitido.
	if contains(st.SQL, "LIMIT") || contains(st.SQL, "TOP ") || contains(st.SQL, `AS "`) {
		t.Errorf("Oracle não deve usar LIMIT/TOP/alias-AS: %s", st.SQL)
	}
}

func TestDialectByName(t *testing.T) {
	for name, want := range map[string]string{
		"":          "postgres",
		"postgres":  "postgres",
		"mysql":     "mysql",
		"mariadb":   "mysql",
		"duckdb":    "duckdb",
		"sqlserver": "sqlserver",
		"mssql":     "sqlserver",
		"oracle":    "oracle",
		"ora":       "oracle",
	} {
		d, err := enginesql.DialectByName(name)
		if err != nil {
			t.Fatalf("DialectByName(%q): %v", name, err)
		}
		if d.Name() != want {
			t.Errorf("DialectByName(%q) = %q, quero %q", name, d.Name(), want)
		}
	}
	if _, err := enginesql.DialectByName("db2"); err == nil {
		t.Error("esperava erro para dialeto desconhecido")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
