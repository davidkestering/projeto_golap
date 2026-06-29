// Package yaml carrega cubos no formato de autoria enxuto (YAML) para a IR de
// metadados. Cada dimensão YAML é achatada numa hierarquia única (com "All"),
// privilegiando autoria rápida ("~20 linhas por cubo"). Para modelagem completa
// (múltiplas hierarquias, snowflake), use o import de Mondrian XML.
//
// Exemplo:
//
//	schema: FoodMart
//	cubes:
//	  - name: Sales
//	    fact: sales_fact_1997
//	    defaultMeasure: Unit Sales
//	    measures:
//	      - {name: Unit Sales, column: unit_sales, agg: sum}
//	    dimensions:
//	      - name: Time
//	        foreignKey: time_id
//	        table: time_by_day
//	        primaryKey: time_id
//	        type: time
//	        levels:
//	          - {name: Year, column: the_year, type: Numeric, levelType: TimeYears}
package yaml

import (
	"fmt"
	"io"
	"os"

	goyaml "gopkg.in/yaml.v3"

	"cubodw/internal/engine/metadata"
)

// LoadFile carrega cubos de um arquivo YAML.
func LoadFile(path string) (*metadata.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f)
}

// LoadBytes carrega cubos de um buffer YAML.
func LoadBytes(b []byte) (*metadata.Schema, error) {
	var ys yamlSchema
	if err := goyaml.Unmarshal(b, &ys); err != nil {
		return nil, fmt.Errorf("yaml: parse: %w", err)
	}
	return ys.toIR()
}

// Load carrega cubos de um io.Reader YAML.
func Load(r io.Reader) (*metadata.Schema, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return LoadBytes(b)
}

type yamlSchema struct {
	Schema string     `yaml:"schema"`
	Cubes  []yamlCube `yaml:"cubes"`
}

type yamlCube struct {
	Name           string          `yaml:"name"`
	Fact           string          `yaml:"fact"`
	FactSchema     string          `yaml:"factSchema"`
	DefaultMeasure string          `yaml:"defaultMeasure"`
	Measures       []yamlMeasure   `yaml:"measures"`
	Dimensions     []yamlDimension `yaml:"dimensions"`
}

type yamlMeasure struct {
	Name   string `yaml:"name"`
	Column string `yaml:"column"`
	Agg    string `yaml:"agg"`
	Format string `yaml:"format"`
}

type yamlDimension struct {
	Name       string      `yaml:"name"`
	ForeignKey string      `yaml:"foreignKey"`
	Table      string      `yaml:"table"`
	PrimaryKey string      `yaml:"primaryKey"`
	Type       string      `yaml:"type"` // "time" => TimeDimension
	AllMember  string      `yaml:"allMember"`
	Levels     []yamlLevel `yaml:"levels"`
}

type yamlLevel struct {
	Name      string `yaml:"name"`
	Column    string `yaml:"column"`
	Type      string `yaml:"type"`
	LevelType string `yaml:"levelType"`
}

func (ys *yamlSchema) toIR() (*metadata.Schema, error) {
	if ys.Schema == "" {
		return nil, fmt.Errorf("yaml: campo 'schema' (nome) obrigatório")
	}
	s := &metadata.Schema{Name: ys.Schema}
	for ci := range ys.Cubes {
		yc := &ys.Cubes[ci]
		if yc.Name == "" {
			return nil, fmt.Errorf("yaml: cubo #%d sem 'name'", ci+1)
		}
		if yc.Fact == "" {
			return nil, fmt.Errorf("yaml: cubo %q sem 'fact'", yc.Name)
		}
		c := &metadata.Cube{
			Name:           yc.Name,
			Caption:        yc.Name,
			DefaultMeasure: yc.DefaultMeasure,
			Visible:        true,
			Fact:           metadata.Relation{Schema: yc.FactSchema, Table: yc.Fact},
		}
		for mi := range yc.Measures {
			ym := &yc.Measures[mi]
			c.Measures = append(c.Measures, &metadata.Measure{
				Name:         ym.Name,
				Caption:      ym.Name,
				Column:       ym.Column,
				Aggregator:   firstNonEmpty(ym.Agg, "sum"),
				FormatString: ym.Format,
				Visible:      true,
			})
		}
		for di := range yc.Dimensions {
			yd := &yc.Dimensions[di]
			dim := &metadata.Dimension{
				Name:       yd.Name,
				Caption:    yd.Name,
				Type:       dimType(yd.Type),
				ForeignKey: yd.ForeignKey,
			}
			h := &metadata.Hierarchy{
				HasAll:        true,
				AllMemberName: yd.AllMember,
				PrimaryKey:    yd.PrimaryKey,
				Table:         metadata.Relation{Table: yd.Table},
			}
			for li := range yd.Levels {
				yl := &yd.Levels[li]
				h.Levels = append(h.Levels, &metadata.Level{
					Name:          yl.Name,
					Column:        yl.Column,
					Type:          yl.Type,
					LevelType:     firstNonEmpty(yl.LevelType, "Regular"),
					UniqueMembers: false,
				})
			}
			dim.Hierarchies = []*metadata.Hierarchy{h}
			c.Dimensions = append(c.Dimensions, dim)
		}
		s.Cubes = append(s.Cubes, c)
	}
	return s, nil
}

func dimType(t string) string {
	switch t {
	case "time", "TimeDimension":
		return "TimeDimension"
	default:
		return "StandardDimension"
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
