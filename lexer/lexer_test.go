package esh_vendors

import "testing"

func TestLexerOperators(t *testing.T) {
	tests := []struct {
		input string
		want  []TokenType
	}{
		{"+ - * / %", []TokenType{PLUS, MINUS, ASTERISK, SLASH, PERCENT, EOF}},
		{"== != <= >= < > =", []TokenType{EQ, NOT_EQ, LE, GE, LT, GT, ASSIGN, EOF}},
		{"&& || ! . =>", []TokenType{AND, OR, BANG, DOT, ARROW, EOF}},
		{"+= -= *= /= %= .=", []TokenType{PLUS_ASSIGN, MINUS_ASSIGN, MUL_ASSIGN, DIV_ASSIGN, MOD_ASSIGN, DOT_ASSIGN, EOF}},
		{"( ) { } [ ] , ; ? :", []TokenType{LPAREN, RPAREN, LBRACE, RBRACE, LBRACKET, RBRACKET, COMMA, SEMICOLON, QUESTION, COLON, EOF}},
	}
	for _, tt := range tests {
		l := NewLexer(tt.input)
		for i, want := range tt.want {
			tok := l.NextToken()
			if tok.Type != want {
				t.Fatalf("input %q token %d: got %s, want %s", tt.input, i, tok.Type, want)
			}
		}
	}
}

func TestLexerKeywordsAndIdentifiers(t *testing.T) {
	l := NewLexer("function return if else while for foreach as true false null echo myFunc")
	want := []TokenType{FUNCTION, RETURN, IF, ELSE, WHILE, FOR, FOREACH, AS, TRUE, FALSE, NULL, ECHO, IDENT, EOF}
	for i, wantType := range want {
		tok := l.NextToken()
		if tok.Type != wantType {
			t.Fatalf("token %d: got %s (%q), want %s", i, tok.Type, tok.Literal, wantType)
		}
	}
}

func TestLexerVariablesKeepDollarPrefix(t *testing.T) {
	l := NewLexer("$_POST $name $x1")
	for _, want := range []string{"$_POST", "$name", "$x1"} {
		tok := l.NextToken()
		if tok.Type != VAR || tok.Literal != want {
			t.Fatalf("got %s %q, want VAR %q", tok.Type, tok.Literal, want)
		}
	}
}

func TestLexerNumbers(t *testing.T) {
	tests := []struct {
		input     string
		wantType  TokenType
		wantValue string
	}{
		{"42", INT, "42"},
		{"0", INT, "0"},
		{"3.14", FLOAT, "3.14"},
		{"0.5", FLOAT, "0.5"},
	}
	for _, tt := range tests {
		l := NewLexer(tt.input)
		tok := l.NextToken()
		if tok.Type != tt.wantType || tok.Literal != tt.wantValue {
			t.Fatalf("input %q: got %s %q, want %s %q", tt.input, tok.Type, tok.Literal, tt.wantType, tt.wantValue)
		}
	}
}

func TestLexerStringEscapes(t *testing.T) {
	tests := []struct {
		input string // raw source text including surrounding quotes
		want  string // decoded literal value the lexer should produce
	}{
		{`"a\nb"`, "a\nb"},
		{`"a\tb"`, "a\tb"},
		{`"a\rb"`, "a\rb"},
		{`"say \"hi\""`, `say "hi"`},
		{`"back\\slash"`, `back\slash`},
		{`"no escape here"`, "no escape here"},
	}
	for _, tt := range tests {
		l := NewLexer(tt.input)
		tok := l.NextToken()
		if tok.Type != STRING || tok.Literal != tt.want {
			t.Fatalf("input %q: got %q, want %q", tt.input, tok.Literal, tt.want)
		}
	}
}

// A "\$" escape must decode to a value distinguishable from a literal '$',
// so a later interpolation pass over the string's value doesn't re-match it
// as the start of a "$name" substitution. See evaluator_test.go for the
// interpolation-facing regression test that depends on this.
func TestLexerEscapedDollarUsesSentinelNotLiteralDollar(t *testing.T) {
	l := NewLexer(`"\$name"`)
	tok := l.NextToken()
	if tok.Type != STRING {
		t.Fatalf("got token type %s, want STRING", tok.Type)
	}
	if tok.Literal == "$name" {
		t.Fatalf("escaped dollar decoded to literal %q; this would be re-interpolated as a variable reference", tok.Literal)
	}
	for _, r := range tok.Literal {
		if r == '$' {
			t.Fatalf("decoded value %q still contains a literal '$'", tok.Literal)
		}
	}
}

func TestLexerComments(t *testing.T) {
	input := "$a = 1; // line comment\n$b = 2; # hash comment\n$c = 3; /* block\ncomment */ $d = 4;"
	l := NewLexer(input)
	var vars []string
	for {
		tok := l.NextToken()
		if tok.Type == EOF {
			break
		}
		if tok.Type == VAR {
			vars = append(vars, tok.Literal)
		}
	}
	want := []string{"$a", "$b", "$c", "$d"}
	if len(vars) != len(want) {
		t.Fatalf("got vars %v, want %v", vars, want)
	}
	for i := range want {
		if vars[i] != want[i] {
			t.Errorf("var %d: got %q, want %q", i, vars[i], want[i])
		}
	}
}

func TestLexerLineTracking(t *testing.T) {
	l := NewLexer("$a\n$b\n\n$c")
	var lines []int
	for {
		tok := l.NextToken()
		if tok.Type == EOF {
			break
		}
		lines = append(lines, tok.Line)
	}
	want := []int{1, 2, 4}
	if len(lines) != len(want) {
		t.Fatalf("got lines %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("token %d: got line %d, want %d", i, lines[i], want[i])
		}
	}
}
