// Package metadata define a IR (representação interna) do modelo
// multidimensional, alvo comum dos loaders (Mondrian XML, YAML) e fonte da API
// de descoberta e — nas próximas fases — da geração de SQL.
//
// Hierarquia: Schema → Cube → Dimension → Hierarchy → Level (+ Property) e, em
// paralelo, Cube → Measure. Os nomes únicos seguem a convenção MDX do Mondrian
// (ex.: [Store].[Store Country], [Measures].[Unit Sales]).
package metadata

import "strings"

// Schema é o catálogo lógico carregado de um arquivo (Mondrian XML ou YAML).
type Schema struct {
	Name string
	// Cubes são os cubos do schema.
	Cubes []*Cube
	// SharedDimensions são dimensões de topo, referenciadas por DimensionUsage.
	// Mantidas para inspeção/conversão; os cubos já trazem suas dimensões resolvidas.
	SharedDimensions []*Dimension
}

// Cube é um cubo: uma tabela fato + dimensões resolvidas + medidas.
type Cube struct {
	Name           string
	Caption        string
	DefaultMeasure string
	Visible        bool
	Fact           Relation
	Dimensions     []*Dimension
	Measures       []*Measure
}

// Relation referencia uma tabela física (fato ou dimensão).
type Relation struct {
	Schema string // schema do banco; vazio = default (public)
	Table  string
	Alias  string
}

// Dimension agrupa hierarquias. Quando resolvida num cubo, ForeignKey é a coluna
// da tabela fato que liga à dimensão.
type Dimension struct {
	Name        string
	Caption     string
	Type        string // "StandardDimension" | "TimeDimension"
	ForeignKey  string
	Hierarchies []*Hierarchy
}

// Hierarchy é uma sequência de níveis sobre uma tabela de dimensão.
type Hierarchy struct {
	Name          string // vazio => usa o nome da dimensão
	HasAll        bool
	AllMemberName string
	PrimaryKey    string
	DefaultMember string
	Table         Relation
	Levels        []*Level
}

// Level é um nível de uma hierarquia.
type Level struct {
	Name          string
	Column        string
	Type          string // "String" | "Numeric" | "Integer" | "Boolean" | ...
	LevelType     string // "Regular" | "TimeYears" | "TimeQuarters" | "TimeMonths" | ...
	UniqueMembers bool
	Table         string // tabela do nível (snowflake); vazio = tabela da hierarquia
	// NameColumn: coluna de exibição dos membros (quando difere de Column).
	NameColumn string
	// ParentColumn: presente em hierarquias parent-child (auto-referência).
	ParentColumn    string
	NullParentValue string
	Properties      []*Property
}

// DisplayColumn devolve a coluna de exibição (NameColumn se houver, senão Column).
func (l *Level) DisplayColumn() string {
	if l.NameColumn != "" {
		return l.NameColumn
	}
	return l.Column
}

// IsParentChild indica se o nível é parent-child (auto-referência via ParentColumn).
func (l *Level) IsParentChild() bool { return l.ParentColumn != "" }

// Property é um atributo de membro num nível.
type Property struct {
	Name   string
	Column string
	Type   string
}

// Measure é uma medida agregada da tabela fato.
type Measure struct {
	Name         string
	Caption      string
	Column       string
	Aggregator   string // "sum" | "count" | "min" | "max" | "avg" | "distinct-count"
	FormatString string
	DataType     string
	Visible      bool
	Expression   string // SQL de MeasureExpression (preferido sobre Column quando presente)
}

// MeasuresDimension é o nome lógico da dimensão de medidas em MDX.
const MeasuresDimension = "Measures"

// Bracket aplica a quotação MDX a um nome: [Nome]; ']' é duplicado.
func Bracket(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// UniqueName de uma dimensão: [Dim].
func (d *Dimension) UniqueName() string { return Bracket(d.Name) }

// EffectiveName de uma hierarquia: seu nome, ou o da dimensão quando vazio.
func (h *Hierarchy) EffectiveName(d *Dimension) string {
	if h.Name == "" {
		return d.Name
	}
	return h.Name
}

// UniqueName de uma hierarquia: [Dim] quando a hierarquia é a default
// (sem nome ou com o nome da dimensão), senão [Dim].[Hier].
func (h *Hierarchy) UniqueName(d *Dimension) string {
	if h.Name == "" || h.Name == d.Name {
		return Bracket(d.Name)
	}
	return Bracket(d.Name) + "." + Bracket(h.Name)
}

// UniqueName de um nível: [Dim].[Hier?].[Level].
func (l *Level) UniqueName(d *Dimension, h *Hierarchy) string {
	return h.UniqueName(d) + "." + Bracket(l.Name)
}

// UniqueName de uma medida: [Measures].[Nome].
func (m *Measure) UniqueName() string {
	return Bracket(MeasuresDimension) + "." + Bracket(m.Name)
}

// FindCube retorna o cubo pelo nome (case-sensitive) e se foi encontrado.
func (s *Schema) FindCube(name string) (*Cube, bool) {
	for _, c := range s.Cubes {
		if c.Name == name {
			return c, true
		}
	}
	return nil, false
}

// FindDimension retorna a dimensão do cubo pelo nome.
func (c *Cube) FindDimension(name string) (*Dimension, bool) {
	for _, d := range c.Dimensions {
		if d.Name == name {
			return d, true
		}
	}
	return nil, false
}

// FindMeasure retorna a medida do cubo pelo nome.
func (c *Cube) FindMeasure(name string) (*Measure, bool) {
	for _, m := range c.Measures {
		if m.Name == name {
			return m, true
		}
	}
	return nil, false
}
