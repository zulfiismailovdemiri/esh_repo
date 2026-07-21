package esh_vendors

import (
	"bytes"
	"testing"
)

// renderTemplate runs the full pipeline used by the HTTP server and CLI file
// runner: preprocess -> lex -> parse -> eval, returning whatever was echoed.
func renderTemplate(t *testing.T, src string) string {
	t.Helper()
	processed := PreprocessTemplate(src)
	l := NewLexer(processed)
	p := NewParser(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parser errors for preprocessed %q (from source %q): %v", processed, src, errs)
	}
	env := NewEnvironment()
	var buf bytes.Buffer
	env.Out = &buf
	if result := Eval(prog, env); IsError(result) {
		t.Fatalf("eval error for preprocessed %q (from source %q): %s", processed, src, result.Inspect())
	}
	return buf.String()
}

func TestTemplateNoTagsPureCodeUnchanged(t *testing.T) {
	src := `$x = 1 + 1;`
	if got := PreprocessTemplate(src); got != src {
		t.Errorf("got %q, want unchanged %q", got, src)
	}
}

func TestTemplateCodeBlockAndShortEcho(t *testing.T) {
	src := `<?es $name = "World"; ?>Hello, <?= $name ?>!`
	got := renderTemplate(t, src)
	if got != "Hello, World!" {
		t.Errorf("got %q, want %q", got, "Hello, World!")
	}
}

func TestTemplateRawHTMLPassthrough(t *testing.T) {
	src := `<h1>Title</h1><?es $x = 1; ?>`
	got := renderTemplate(t, src)
	if got != "<h1>Title</h1>" {
		t.Errorf("got %q, want %q", got, "<h1>Title</h1>")
	}
}

func TestTemplateForeachInsideTags(t *testing.T) {
	src := `<?es $items = ["Apples", "Bread"]; ?><ul><?es foreach ($items as $item) { ?><li><?= $item ?></li><?es } ?></ul>`
	got := renderTemplate(t, src)
	want := "<ul><li>Apples</li><li>Bread</li></ul>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTemplateNewlineAfterCloseTagIsConsumed(t *testing.T) {
	// PHP convention: a single newline right after '?>' is swallowed so the
	// code block itself doesn't introduce a blank line into the output.
	src := "<?es $x = 1; ?>\nnext line"
	got := renderTemplate(t, src)
	if got != "next line" {
		t.Errorf("got %q, want %q", got, "next line")
	}
}

func TestTemplateDollarInRawHTMLIsLiteral(t *testing.T) {
	// Outside of tags, '$' has no special meaning and must survive verbatim.
	// Regression coverage: the preprocessor turns raw text into an
	// echo "...\$..."-style string (see emitText in template.go), which
	// round-trips through the same lexer escape + evaluator interpolation
	// passes covered by the bug fix in evaluator_test.go. A follower letter
	// ("$support", not "$5") is what previously triggered the bug, since the
	// interpolation regex only matches "$" followed by an identifier.
	src := `<p>Contact: $support</p>`
	got := renderTemplate(t, src)
	if got != "<p>Contact: $support</p>" {
		t.Errorf("got %q, want %q", got, "<p>Contact: $support</p>")
	}
}
