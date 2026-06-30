// Package query define a especificação de uma consulta tabular (pivot) sobre um
// cubo e o formato do resultado tipado (records). É o precursor do modelo "thin"
// e da AI Query API: descreve medidas, níveis em linhas/colunas e filtros sem
// expor MDX. Nas próximas fases o avaliador MDX produzirá esta mesma estrutura.
package query

// Query é uma consulta: medidas agregadas por uma combinação de níveis
// (linhas + colunas), opcionalmente filtrada.
type Query struct {
	Cube     string     `json:"cube"`
	Measures []string   `json:"measures"`
	Rows     []LevelRef `json:"rows,omitempty"`
	Columns  []LevelRef `json:"columns,omitempty"`
	Filters  []Filter   `json:"filters,omitempty"`
	// Totals acrescenta uma linha de total geral (todas as medidas sobre o
	// conjunto filtrado, sem agrupar por níveis).
	Totals bool `json:"totals,omitempty"`
}

// LevelRef referencia um nível de uma hierarquia/dimensão.
type LevelRef struct {
	Dimension string `json:"dimension"`
	Hierarchy string `json:"hierarchy,omitempty"`
	Level     string `json:"level"`
}

// Filter restringe um nível a um conjunto de membros (comparados como texto
// contra a coluna do nível).
type Filter struct {
	Dimension string   `json:"dimension"`
	Hierarchy string   `json:"hierarchy,omitempty"`
	Level     string   `json:"level"`
	Members   []string `json:"members"`
}

// AxisLevels devolve todos os níveis de eixo (linhas seguidas de colunas).
func (q *Query) AxisLevels() []LevelRef {
	out := make([]LevelRef, 0, len(q.Rows)+len(q.Columns))
	out = append(out, q.Rows...)
	out = append(out, q.Columns...)
	return out
}

// Column descreve uma coluna do resultado (um nível de eixo ou uma medida).
type Column struct {
	Name         string `json:"name"`
	UniqueName   string `json:"uniqueName"`
	Kind         string `json:"kind"` // "level" | "measure"
	FormatString string `json:"formatString,omitempty"`
}

// Cell é uma célula tipada do resultado.
type Cell struct {
	Value     any    `json:"value"`
	Formatted string `json:"formatted"`
}

// Result é o resultado em formato de records: colunas (níveis + medidas) e linhas.
type Result struct {
	Cube    string   `json:"cube"`
	SQL     string   `json:"sql"`
	Columns []Column `json:"columns"`
	Rows    [][]Cell `json:"rows"`
}
