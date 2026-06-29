package mondrian

import "testing"

const sampleXML = `<?xml version="1.0"?>
<Schema name="Test">
  <Dimension name="Store">
    <Hierarchy hasAll="true" primaryKey="store_id">
      <Table name="store"/>
      <Level name="Store Country" column="store_country" uniqueMembers="true"/>
      <Level name="Store City" column="store_city"/>
    </Hierarchy>
  </Dimension>
  <Cube name="Sales" defaultMeasure="Unit Sales">
    <Table name="sales_fact_1997"/>
    <DimensionUsage name="Store" source="Store" foreignKey="store_id"/>
    <Dimension name="Promotion Media" foreignKey="promotion_id">
      <Hierarchy hasAll="true" allMemberName="All Media" primaryKey="promotion_id">
        <Table name="promotion"/>
        <Level name="Media Type" column="media_type" uniqueMembers="true"/>
      </Hierarchy>
    </Dimension>
    <Measure name="Unit Sales" column="unit_sales" aggregator="sum" formatString="Standard"/>
    <Measure name="Promotion Sales" aggregator="sum">
      <MeasureExpression>
        <SQL dialect="generic">(case when "x" then 1 else 0 end)</SQL>
        <SQL dialect="postgres">coalesce("promo", 0)</SQL>
      </MeasureExpression>
    </Measure>
  </Cube>
</Schema>`

func TestLoadResolvesDimensionUsageAndInline(t *testing.T) {
	s, err := LoadBytes([]byte(sampleXML))
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	if s.Name != "Test" {
		t.Fatalf("schema name = %q", s.Name)
	}
	if len(s.SharedDimensions) != 1 {
		t.Fatalf("shared dims = %d, quero 1", len(s.SharedDimensions))
	}
	c, ok := s.FindCube("Sales")
	if !ok {
		t.Fatal("cubo Sales não encontrado")
	}
	if c.Fact.Table != "sales_fact_1997" {
		t.Errorf("fact = %q", c.Fact.Table)
	}
	if len(c.Dimensions) != 2 {
		t.Fatalf("dims = %d, quero 2 (DimensionUsage + inline)", len(c.Dimensions))
	}

	// DimensionUsage resolvida: Store deve herdar níveis da compartilhada e
	// receber a foreignKey do uso.
	store, ok := c.FindDimension("Store")
	if !ok {
		t.Fatal("dim Store não resolvida")
	}
	if store.ForeignKey != "store_id" {
		t.Errorf("Store.foreignKey = %q", store.ForeignKey)
	}
	if len(store.Hierarchies) != 1 || len(store.Hierarchies[0].Levels) != 2 {
		t.Errorf("Store hierarquia/níveis inesperados: %+v", store.Hierarchies)
	}
	if store.Hierarchies[0].Table.Table != "store" {
		t.Errorf("Store table = %q", store.Hierarchies[0].Table.Table)
	}

	// Dimensão inline.
	media, ok := c.FindDimension("Promotion Media")
	if !ok {
		t.Fatal("dim inline Promotion Media não carregada")
	}
	if media.ForeignKey != "promotion_id" {
		t.Errorf("Promotion Media.foreignKey = %q", media.ForeignKey)
	}

	// Medidas.
	if len(c.Measures) != 2 {
		t.Fatalf("measures = %d", len(c.Measures))
	}
	ps, _ := c.FindMeasure("Promotion Sales")
	if ps.Expression != `coalesce("promo", 0)` {
		t.Errorf("MeasureExpression (postgres preferido) = %q", ps.Expression)
	}
}

func TestLoadRejectsUnknownDimensionUsage(t *testing.T) {
	bad := `<Schema name="X"><Cube name="C"><Table name="f"/>
	  <DimensionUsage name="D" source="NaoExiste" foreignKey="d_id"/></Cube></Schema>`
	if _, err := LoadBytes([]byte(bad)); err == nil {
		t.Fatal("esperava erro para DimensionUsage com source inexistente")
	}
}
