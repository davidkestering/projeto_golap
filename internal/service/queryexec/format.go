package queryexec

import (
	"fmt"
	"strconv"
	"time"
)

// formatValue produz uma representação textual básica de uma célula. A
// formatação rica por formatString da medida virá numa fase posterior.
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
