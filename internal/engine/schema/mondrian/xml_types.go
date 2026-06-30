package mondrian

import "strings"

// Tipos de binding XML para o formato Mondrian v3. Atributos booleanos usam
// *bool para distinguir "ausente" (default) de "false".

type xmlSchema struct {
	Name       string         `xml:"name,attr"`
	Dimensions []xmlDimension `xml:"Dimension"` // dimensões compartilhadas (topo)
	Cubes      []xmlCube      `xml:"Cube"`
}

type xmlCube struct {
	Name            string              `xml:"name,attr"`
	Caption         string              `xml:"caption,attr"`
	DefaultMeasure  string              `xml:"defaultMeasure,attr"`
	Visible         *bool               `xml:"visible,attr"`
	Table           *xmlTable           `xml:"Table"`
	DimensionUsages []xmlDimensionUsage `xml:"DimensionUsage"`
	Dimensions      []xmlDimension      `xml:"Dimension"` // dimensões inline
	Measures        []xmlMeasure        `xml:"Measure"`
}

type xmlTable struct {
	Schema string `xml:"schema,attr"`
	Name   string `xml:"name,attr"`
	Alias  string `xml:"alias,attr"`
}

type xmlDimensionUsage struct {
	Name       string `xml:"name,attr"`
	Source     string `xml:"source,attr"`
	ForeignKey string `xml:"foreignKey,attr"`
	Caption    string `xml:"caption,attr"`
}

type xmlDimension struct {
	Name        string         `xml:"name,attr"`
	Caption     string         `xml:"caption,attr"`
	Type        string         `xml:"type,attr"`
	ForeignKey  string         `xml:"foreignKey,attr"`
	Hierarchies []xmlHierarchy `xml:"Hierarchy"`
}

type xmlHierarchy struct {
	Name          string     `xml:"name,attr"`
	HasAll        *bool      `xml:"hasAll,attr"`
	AllMemberName string     `xml:"allMemberName,attr"`
	PrimaryKey    string     `xml:"primaryKey,attr"`
	DefaultMember string     `xml:"defaultMember,attr"`
	Table         *xmlTable  `xml:"Table"`
	Levels        []xmlLevel `xml:"Level"`
}

type xmlLevel struct {
	Name            string        `xml:"name,attr"`
	Column          string        `xml:"column,attr"`
	Table           string        `xml:"table,attr"`
	Type            string        `xml:"type,attr"`
	LevelType       string        `xml:"levelType,attr"`
	NameColumn      string        `xml:"nameColumn,attr"`
	ParentColumn    string        `xml:"parentColumn,attr"`
	NullParentValue string        `xml:"nullParentValue,attr"`
	UniqueMembers   *bool         `xml:"uniqueMembers,attr"`
	Properties      []xmlProperty `xml:"Property"`
}

type xmlProperty struct {
	Name   string `xml:"name,attr"`
	Column string `xml:"column,attr"`
	Type   string `xml:"type,attr"`
}

type xmlMeasure struct {
	Name         string          `xml:"name,attr"`
	Caption      string          `xml:"caption,attr"`
	Column       string          `xml:"column,attr"`
	Aggregator   string          `xml:"aggregator,attr"`
	FormatString string          `xml:"formatString,attr"`
	DataType     string          `xml:"datatype,attr"`
	Visible      *bool           `xml:"visible,attr"`
	MeasureExpr  *xmlMeasureExpr `xml:"MeasureExpression"`
}

type xmlMeasureExpr struct {
	SQL []xmlSQL `xml:"SQL"`
}

type xmlSQL struct {
	Dialect string `xml:"dialect,attr"`
	Content string `xml:",chardata"`
}

// expression escolhe a melhor SQL da MeasureExpression: prefere o dialeto
// postgres/generic; cai para a primeira disponível.
func (xm *xmlMeasure) expression() string {
	if xm.MeasureExpr == nil || len(xm.MeasureExpr.SQL) == 0 {
		return ""
	}
	var first, generic string
	for _, s := range xm.MeasureExpr.SQL {
		c := strings.TrimSpace(s.Content)
		if first == "" {
			first = c
		}
		switch s.Dialect {
		case "postgres", "postgresql":
			return c
		case "generic", "":
			if generic == "" {
				generic = c
			}
		}
	}
	if generic != "" {
		return generic
	}
	return first
}
