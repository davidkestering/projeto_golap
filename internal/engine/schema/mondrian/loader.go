// Package mondrian carrega schemas Mondrian (formato XML v3) para a IR de
// metadados. Suporta o subconjunto necessário para descoberta e geração de SQL:
// Schema, Cube (Table fato), DimensionUsage (resolvido contra dimensões
// compartilhadas), Dimension/Hierarchy/Level/Property inline e Measure
// (incl. MeasureExpression). Construções avançadas (VirtualCube, Join/snowflake,
// CalculatedMember, Role/Grant, tabelas Agg) são ignoradas por ora.
package mondrian

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"cubodw/internal/engine/metadata"
)

// LoadFile carrega um schema Mondrian de um arquivo.
func LoadFile(path string) (*metadata.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Load(f)
}

// LoadBytes carrega um schema Mondrian de um buffer.
func LoadBytes(b []byte) (*metadata.Schema, error) {
	return Load(strings.NewReader(string(b)))
}

// Load carrega um schema Mondrian de um io.Reader.
func Load(r io.Reader) (*metadata.Schema, error) {
	var xs xmlSchema
	dec := xml.NewDecoder(r)
	if err := dec.Decode(&xs); err != nil {
		return nil, fmt.Errorf("mondrian: parse XML: %w", err)
	}
	if xs.Name == "" {
		return nil, fmt.Errorf("mondrian: <Schema> sem atributo name")
	}
	return xs.toIR()
}

func (xs *xmlSchema) toIR() (*metadata.Schema, error) {
	shared := make(map[string]*metadata.Dimension, len(xs.Dimensions))
	var sharedList []*metadata.Dimension
	for i := range xs.Dimensions {
		d := xs.Dimensions[i].toIR("")
		shared[d.Name] = d
		sharedList = append(sharedList, d)
	}

	s := &metadata.Schema{Name: xs.Name, SharedDimensions: sharedList}
	for i := range xs.Cubes {
		c, err := xs.Cubes[i].toIR(shared)
		if err != nil {
			return nil, err
		}
		s.Cubes = append(s.Cubes, c)
	}
	return s, nil
}

func (xc *xmlCube) toIR(shared map[string]*metadata.Dimension) (*metadata.Cube, error) {
	if xc.Table == nil || xc.Table.Name == "" {
		return nil, fmt.Errorf("mondrian: cubo %q sem <Table> fato", xc.Name)
	}
	c := &metadata.Cube{
		Name:           xc.Name,
		Caption:        firstNonEmpty(xc.Caption, xc.Name),
		DefaultMeasure: xc.DefaultMeasure,
		Visible:        boolDefault(xc.Visible, true),
		Fact:           metadata.Relation{Schema: xc.Table.Schema, Table: xc.Table.Name, Alias: xc.Table.Alias},
	}

	// Dimensões via DimensionUsage (resolvidas contra as compartilhadas).
	for _, du := range xc.DimensionUsages {
		src, ok := shared[du.Source]
		if !ok {
			return nil, fmt.Errorf("mondrian: cubo %q usa dimensão compartilhada %q inexistente", xc.Name, du.Source)
		}
		dim := cloneDimension(src)
		dim.Name = firstNonEmpty(du.Name, du.Source)
		if du.Caption != "" {
			dim.Caption = du.Caption
		}
		dim.ForeignKey = du.ForeignKey
		c.Dimensions = append(c.Dimensions, dim)
	}

	// Dimensões inline (definidas dentro do cubo).
	for i := range xc.Dimensions {
		dim := xc.Dimensions[i].toIR(xc.Dimensions[i].ForeignKey)
		c.Dimensions = append(c.Dimensions, dim)
	}

	for i := range xc.Measures {
		c.Measures = append(c.Measures, xc.Measures[i].toIR())
	}
	return c, nil
}

func (xd *xmlDimension) toIR(foreignKey string) *metadata.Dimension {
	d := &metadata.Dimension{
		Name:       xd.Name,
		Caption:    firstNonEmpty(xd.Caption, xd.Name),
		Type:       firstNonEmpty(xd.Type, "StandardDimension"),
		ForeignKey: firstNonEmpty(foreignKey, xd.ForeignKey),
	}
	for i := range xd.Hierarchies {
		d.Hierarchies = append(d.Hierarchies, xd.Hierarchies[i].toIR())
	}
	return d
}

func (xh *xmlHierarchy) toIR() *metadata.Hierarchy {
	h := &metadata.Hierarchy{
		Name:          xh.Name,
		HasAll:        boolDefault(xh.HasAll, true),
		AllMemberName: xh.AllMemberName,
		PrimaryKey:    xh.PrimaryKey,
		DefaultMember: xh.DefaultMember,
	}
	if xh.Table != nil {
		h.Table = metadata.Relation{Schema: xh.Table.Schema, Table: xh.Table.Name, Alias: xh.Table.Alias}
	}
	for i := range xh.Levels {
		h.Levels = append(h.Levels, xh.Levels[i].toIR())
	}
	return h
}

func (xl *xmlLevel) toIR() *metadata.Level {
	l := &metadata.Level{
		Name:            xl.Name,
		Column:          xl.Column,
		Type:            xl.Type,
		LevelType:       firstNonEmpty(xl.LevelType, "Regular"),
		UniqueMembers:   boolDefault(xl.UniqueMembers, false),
		Table:           xl.Table,
		NameColumn:      xl.NameColumn,
		ParentColumn:    xl.ParentColumn,
		NullParentValue: xl.NullParentValue,
	}
	for _, p := range xl.Properties {
		l.Properties = append(l.Properties, &metadata.Property{Name: p.Name, Column: p.Column, Type: p.Type})
	}
	return l
}

func (xm *xmlMeasure) toIR() *metadata.Measure {
	return &metadata.Measure{
		Name:         xm.Name,
		Caption:      firstNonEmpty(xm.Caption, xm.Name),
		Column:       xm.Column,
		Aggregator:   xm.Aggregator,
		FormatString: xm.FormatString,
		DataType:     xm.DataType,
		Visible:      boolDefault(xm.Visible, true),
		Expression:   xm.expression(),
	}
}

// cloneDimension faz uma cópia rasa da dimensão compartilhada. Hierarquias e
// níveis são imutáveis após o load, então o ponteiro pode ser reaproveitado;
// apenas Name/Caption/ForeignKey variam por uso no cubo.
func cloneDimension(src *metadata.Dimension) *metadata.Dimension {
	cp := *src
	return &cp
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func boolDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
