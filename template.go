package esh_vendors

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveIncludePath turns a user-supplied relative path into an absolute
// filesystem path, kept inside baseDir to prevent traversal.
func resolveIncludePath(baseDir, p string) (string, error) {
	if !filepath.IsAbs(p) {
		if baseDir == "" {
			cwd, _ := os.Getwd()
			baseDir = cwd
		}
		p = filepath.Join(baseDir, p)
	}
	clean := filepath.Clean(p)
	if baseDir != "" {
		absBase, _ := filepath.Abs(baseDir)
		absP, _ := filepath.Abs(clean)
		if !strings.HasPrefix(absP+string(filepath.Separator), absBase+string(filepath.Separator)) && absP != absBase {
			return "", fmt.Errorf("path outside base dir: %s", p)
		}
	}
	return clean, nil
}

// loadAndEval reads, preprocesses, parses, and evaluates an ESH source file
// inside the given environment. Returns nil on success or an *Error wrapping
// the source name with the underlying problem.
func loadAndEval(path string, env *Environment) *Error {
	srcBytes, err := os.ReadFile(path)
	if err != nil {
		return &Error{Message: err.Error()}
	}
	src := PreprocessTemplate(string(srcBytes))
	l := NewLexer(src)
	p := NewParser(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		return &Error{Message: fmt.Sprintf("%s: %s", filepath.Base(path), errs[0])}
	}
	result := Eval(prog, env)
	if e, ok := result.(*Error); ok {
		return &Error{Message: fmt.Sprintf("%s: %s", filepath.Base(path), e.Message)}
	}
	return nil
}

// preprocessTemplate converts a PHP-style template source (mixed HTML and
// <?es ... ?> / <?esh ... ?> / <?= ... ?> blocks) into pure ESH code that the existing
// lexer/parser can handle. Raw text chunks are wrapped as echo "...".
//
// If the source contains no template tags it is returned unchanged.
func PreprocessTemplate(src string) string {
	hasTags := strings.Contains(src, "<?es") || strings.Contains(src, "<?esh") || strings.Contains(src, "<?=")

	// If no tags, we must decide if it's pure code or pure HTML.
	// We treat it as HTML (template) if it contains anything that looks like
	// an HTML tag, otherwise we treat it as pure code for backward compatibility
	// with simple scripts.
	if !hasTags {
		if strings.Contains(src, "<") && strings.Contains(src, ">") {
			var out strings.Builder
			emitText(&out, src)
			return out.String()
		}
		return src
	}

	var out strings.Builder
	i := 0
	for i < len(src) {
		// Find next opening tag
		nextEs := indexFrom(src, "<?es", i)
		nextEsh := indexFrom(src, "<?esh", i)
		nextShort := indexFrom(src, "<?=", i)

		next := -1
		isShort := false
		tagLen := 0

		// Find the earliest tag
		if nextEs != -1 && (next == -1 || nextEs < next) {
			next = nextEs
			tagLen = 4 // <?es
		}
		if nextEsh != -1 && (next == -1 || nextEsh < next) {
			next = nextEsh
			tagLen = 5 // <?esh
		}
		if nextShort != -1 && (next == -1 || nextShort < next) {
			next = nextShort
			isShort = true
			tagLen = 3 // <?=
		}

		if next == -1 {
			emitText(&out, src[i:])
			return out.String()
		}

		// Raw text before the tag
		if next > i {
			emitText(&out, src[i:next])
		}

		// Skip opening tag
		i = next + tagLen

		// Find closing ?>
		closeIdx := indexFrom(src, "?>", i)
		if closeIdx == -1 {
			// unclosed tag — treat rest as code
			code := src[i:]
			if isShort {
				out.WriteString("echo (")
				out.WriteString(strings.TrimSpace(code))
				out.WriteString(");")
			} else {
				out.WriteString(code)
			}
			return out.String()
		}

		code := src[i:closeIdx]
		if isShort {
			out.WriteString("echo (")
			out.WriteString(strings.TrimSpace(code))
			out.WriteString(");")
		} else {
			out.WriteString(code)
			// ensure separation between code block and following echo
			out.WriteString(";")
		}
		i = closeIdx + 2

		// PHP convention: a single newline immediately after ?> is consumed
		if i < len(src) && src[i] == '\n' {
			i++
		} else if i+1 < len(src) && src[i] == '\r' && src[i+1] == '\n' {
			i += 2
		}
	}
	return out.String()
}

func indexFrom(s, sub string, from int) int {
	if from >= len(s) {
		return -1
	}
	idx := strings.Index(s[from:], sub)
	if idx == -1 {
		return -1
	}
	return from + idx
}

// emitText writes a raw text chunk as an echo statement with a properly
// escaped string literal.
func emitText(out *strings.Builder, text string) {
	if text == "" {
		return
	}
	out.WriteString(`echo "`)
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch c {
		case '\\':
			out.WriteString(`\\`)
		case '"':
			out.WriteString(`\"`)
		case '$':
			out.WriteString(`\$`)
		default:
			out.WriteByte(c)
		}
	}
	out.WriteString(`";`)
}
