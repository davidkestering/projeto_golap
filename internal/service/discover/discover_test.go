package discover

import "testing"

func TestNormalizeNameTransliterates(t *testing.T) {
	cases := map[string]string{
		"Inventário Geral":   "INVENTARIOGERAL",
		"São Paulo":          "SAOPAULO",
		"Ñoño & Cia":         "NONOCIA",
		"Vendas Mensais 2024": "VENDASMENSAIS2024",
		"ção-com_traço":      "CAOCOMTRACO",
		"Preço (R$)":         "PRECOR",
		"  trim  ":           "TRIM",
		"Æsop ßeta øre":      "AESOPSSETAORE",
		"!@#$%":              "", // sem letras/dígitos => vazio (cai no default do chamador)
	}
	for in, want := range cases {
		if got := normalizeName(in); got != want {
			t.Errorf("normalizeName(%q) = %q, quero %q", in, got, want)
		}
	}
}

func TestUniqueNameVersioning(t *testing.T) {
	taken := map[string]bool{"SALES": true, "SALESV1": true}
	if got := uniqueName("SALES", taken); got != "SALESV2" {
		t.Errorf("uniqueName = %q, quero SALESV2 (pula V1 já existente)", got)
	}
	if got := uniqueName("NOVO", taken); got != "NOVO" {
		t.Errorf("uniqueName(NOVO) = %q, quero NOVO (livre)", got)
	}
}
