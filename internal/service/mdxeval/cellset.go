// Package mdxeval avalia uma query MDX (AST) reduzindo-a ao modelo de query da
// Fase 2 (níveis de agrupamento + medidas + filtros), executando via queryexec
// e pivotando os records num CellSet.
//
// Cobertura desta fase: medidas em eixo (ou medida default), conjuntos de
// membros explícitos, [Dim].[Nível].Members, [membro].Children, CrossJoin,
// tuplas e slicer (WHERE). Funções como Filter/Order/TopCount e membros
// calculados (WITH) ainda não são suportados (erro claro).
package mdxeval

// CellSet é o resultado de uma avaliação MDX: N eixos com posições e células.
type CellSet struct {
	Cube  string  `json:"cube"`
	SQL   string  `json:"sql"`
	Axes  []Axis  `json:"axes"`
	Cells []Cell  `json:"cells"`
	Grid  *Grid   `json:"grid,omitempty"` // conveniência para 2 eixos (colunas × linhas)
}

// Axis é um eixo do CellSet.
type Axis struct {
	Ordinal   int        `json:"ordinal"`
	Name      string     `json:"name"`
	Positions []Position `json:"positions"`
}

// Position é uma posição num eixo: uma tupla de membros.
type Position struct {
	Members []Member `json:"members"`
}

// Member é um membro numa posição.
type Member struct {
	Caption    string `json:"caption"`
	UniqueName string `json:"uniqueName"`
}

// Cell é uma célula: coordenadas (um índice por eixo) + valor.
type Cell struct {
	Coords    []int  `json:"coords"`
	Value     any    `json:"value"`
	Formatted string `json:"formatted"`
}

// Grid é uma renderização tabular para o caso de 2 eixos.
type Grid struct {
	ColumnHeaders []string   `json:"columnHeaders"`
	RowHeaders    []string   `json:"rowHeaders"`
	Rows          [][]string `json:"rows"`
}
