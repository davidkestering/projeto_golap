package queryexec

import (
	"fmt"
	"strconv"
	"time"
)

// asFloat converte um valor de célula para float64.
func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

// formatValue produz uma representação textual básica de uma célula. A
// formatação rica por formatString da medida é aplicada quando a coluna a tem.
func formatValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		// Sem casas decimais quando o valor é inteiro; senão 2 casas.
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', 2, 64)
	case float32:
		return formatValue(float64(t))
	case int64:
		return strconv.FormatInt(t, 10)
	case int32:
		return strconv.FormatInt(int64(t), 10)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	case time.Time:
		return t.Format("2006-01-02")
	default:
		return fmt.Sprint(v)
	}
}
