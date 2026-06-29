package mdx

import "testing"

func TestTokenizeMemberRef(t *testing.T) {
	toks, err := Tokenize(`{[Measures].[Unit Sales]}`)
	if err != nil {
		t.Fatalf("Tokenize: %v", err)
	}
	want := []TokenType{LBRACE, QUOTEDID, DOT, QUOTEDID, RBRACE, EOF}
	if len(toks) != len(want) {
		t.Fatalf("nº tokens = %d, quero %d: %+v", len(toks), len(want), toks)
	}
	for i, tt := range want {
		if toks[i].Type != tt {
			t.Errorf("token[%d] = %s, quero %s", i, toks[i].Type, tt)
		}
	}
	if toks[1].Text != "Measures" || toks[3].Text != "Unit Sales" {
		t.Errorf("texto dos quoted ids inesperado: %q %q", toks[1].Text, toks[3].Text)
	}
}

func TestTokenizeKeyAndOps(t *testing.T) {
	toks, _ := Tokenize(`[Time].&[1998] <> 3 || 'a''b' // comentário`)
	types := []TokenType{}
	for _, tk := range toks {
		types = append(types, tk.Type)
	}
	// [Time] . &[1998] <> 3 || 'a'b' EOF
	want := []TokenType{QUOTEDID, DOT, KEYID, NE, NUMBER, CONCAT, STRING, EOF}
	if len(types) != len(want) {
		t.Fatalf("tokens = %+v", toks)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("token[%d] = %s, quero %s", i, types[i], want[i])
		}
	}
	if toks[2].Text != "1998" {
		t.Errorf("key text = %q", toks[2].Text)
	}
	if toks[6].Text != "a'b" {
		t.Errorf("string com escape '' = %q, quero a'b", toks[6].Text)
	}
}

func TestTokenizeNumberVsDot(t *testing.T) {
	// "5.Members" deve virar NUMBER(5) DOT IDENT(Members), não NUMBER(5.).
	toks, _ := Tokenize(`5.Members 1.5`)
	if toks[0].Type != NUMBER || toks[0].Text != "5" {
		t.Errorf("token[0] = %+v", toks[0])
	}
	if toks[1].Type != DOT {
		t.Errorf("token[1] = %s, quero DOT", toks[1].Type)
	}
	if toks[2].Type != IDENT || toks[2].Text != "Members" {
		t.Errorf("token[2] = %+v", toks[2])
	}
	if toks[3].Type != NUMBER || toks[3].Text != "1.5" {
		t.Errorf("token[3] = %+v (quero NUMBER 1.5)", toks[3])
	}
}
