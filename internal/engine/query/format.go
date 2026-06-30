package query

import (
	"strconv"
	"strings"
)

// Format aplica um formatString estilo Mondrian/VBA (subconjunto) a um valor:
//   - "" / "Standard" / "General Number": agrupamento de milhares, casas conforme
//     o valor (0 se inteiro, senão 2)
//   - "Currency": "$" + 2 casas + agrupamento
//   - padrões com "," => agrupamento de milhares; casas = nº de 0/# após o "."
//   - padrões com "%" => multiplica por 100 e adiciona "%"
//   - "$" no padrão => prefixo de moeda
func Format(v float64, format string) string {
	f := strings.TrimSpace(format)
	lower := strings.ToLower(f)

	switch lower {
	case "", "standard", "general number":
		dec := 0
		if v != float64(int64(v)) {
			dec = 2
		}
		return group(v, dec, true)
	case "currency":
		return "$" + group(v, 2, true)
	}

	percent := strings.Contains(f, "%")
	if percent {
		v *= 100
	}
	out := group(v, decimalsOf(f), strings.Contains(f, ","))
	if strings.Contains(f, "$") {
		out = "$" + out
	}
	if percent {
		out += "%"
	}
	return out
}

// decimalsOf conta os dígitos (0/#) logo após o ponto decimal do padrão.
func decimalsOf(f string) int {
	i := strings.LastIndex(f, ".")
	if i < 0 {
		return 0
	}
	n := 0
	for _, c := range f[i+1:] {
		if c == '0' || c == '#' {
			n++
		} else {
			break
		}
	}
	return n
}

// group formata v com `dec` casas e, se grouping, separadores de milhar.
func group(v float64, dec int, grouping bool) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	if grouping {
		intPart = thousands(intPart)
	}
	out := intPart + frac
	if neg {
		out = "-" + out
	}
	return out
}

func thousands(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	var b strings.Builder
	pre := n % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		b.WriteByte(',')
	}
	for i := pre; i < n; i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < n {
			b.WriteByte(',')
		}
	}
	return b.String()
}
