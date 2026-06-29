package metadata

import "testing"

func TestBracket(t *testing.T) {
	cases := map[string]string{
		"Store":       "[Store]",
		"Unit Sales":  "[Unit Sales]",
		"Weird]Name":  "[Weird]]Name]",
	}
	for in, want := range cases {
		if got := Bracket(in); got != want {
			t.Errorf("Bracket(%q) = %q, quero %q", in, got, want)
		}
	}
}

func TestUniqueNames(t *testing.T) {
	d := &Dimension{Name: "Store"}
	hDefault := &Hierarchy{Name: ""}
	hNamed := &Hierarchy{Name: "Weekly"}
	lvl := &Level{Name: "Store Country"}
	m := &Measure{Name: "Unit Sales"}

	if got := d.UniqueName(); got != "[Store]" {
		t.Errorf("dim uniqueName = %q", got)
	}
	if got := hDefault.UniqueName(d); got != "[Store]" {
		t.Errorf("hier default uniqueName = %q", got)
	}
	if got := hNamed.UniqueName(d); got != "[Store].[Weekly]" {
		t.Errorf("hier named uniqueName = %q", got)
	}
	if got := lvl.UniqueName(d, hDefault); got != "[Store].[Store Country]" {
		t.Errorf("level uniqueName = %q", got)
	}
	if got := m.UniqueName(); got != "[Measures].[Unit Sales]" {
		t.Errorf("measure uniqueName = %q", got)
	}
}
