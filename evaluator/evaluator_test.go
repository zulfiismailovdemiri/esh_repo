package esh_vendors

import (
	"bytes"
	"testing"
)

// evalSrc runs the full lexer -> parser -> evaluator pipeline and returns the
// program's result value plus whatever was written via echo.
func evalSrc(t *testing.T, src string) (Object, string) {
	t.Helper()
	l := NewLexer(src)
	p := NewParser(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parser errors for %q: %v", src, errs)
	}
	env := NewEnvironment()
	var buf bytes.Buffer
	env.Out = &buf
	result := Eval(prog, env)
	return result, buf.String()
}

func TestEvalIntArithmetic(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"1 + 2 * 3;", 7},
		{"(1 + 2) * 3;", 9},
		{"10 % 3;", 1},
		{"8 / 2;", 4}, // evenly divisible int division stays an Integer
		{"10 - 3 - 2;", 5},
	}
	for _, tt := range tests {
		result, _ := evalSrc(t, tt.input)
		i, ok := result.(*Integer)
		if !ok {
			t.Fatalf("input %q: want *Integer, got %T (%s)", tt.input, result, result.Inspect())
		}
		if i.Value != tt.want {
			t.Errorf("input %q: got %d, want %d", tt.input, i.Value, tt.want)
		}
	}
}

func TestEvalDivisionPromotesToFloatWhenUneven(t *testing.T) {
	result, _ := evalSrc(t, "7 / 2;")
	f, ok := result.(*Float)
	if !ok {
		t.Fatalf("want *Float, got %T (%s)", result, result.Inspect())
	}
	if f.Value != 3.5 {
		t.Errorf("got %v, want 3.5", f.Value)
	}
}

func TestEvalDivisionAndModuloByZeroAreErrors(t *testing.T) {
	for _, input := range []string{"1 / 0;", "1 % 0;"} {
		result, _ := evalSrc(t, input)
		if !IsError(result) {
			t.Errorf("input %q: want error, got %T (%s)", input, result, result.Inspect())
		}
	}
}

func TestEvalStringConcatenation(t *testing.T) {
	_, out := evalSrc(t, `$name = "World"; echo "Hello, " . $name . "!";`)
	if out != "Hello, World!" {
		t.Errorf("got %q", out)
	}
}

// Regression test: interpolate() must look variables up under their
// '$'-prefixed key (how Environment.Set stores them), not the bare name.
// Before the fix every "$var" inside a string silently became "".
func TestEvalStringInterpolation(t *testing.T) {
	_, out := evalSrc(t, `$name = "Alice"; echo "Hi, $name!";`)
	if out != "Hi, Alice!" {
		t.Errorf("got %q, want %q", out, "Hi, Alice!")
	}
}

func TestEvalStringInterpolationMultipleVars(t *testing.T) {
	_, out := evalSrc(t, `$k = "color"; $v = "blue"; echo "$k=$v";`)
	if out != "color=blue" {
		t.Errorf("got %q, want %q", out, "color=blue")
	}
}

func TestEvalStringInterpolationUndefinedVarIsEmpty(t *testing.T) {
	_, out := evalSrc(t, `echo "before $missing after";`)
	if out != "before  after" {
		t.Errorf("got %q, want %q", out, "before  after")
	}
}

// Regression test: a "\$" escape must survive interpolation and produce a
// literal '$', not have its escaped text re-interpolated as "$name".
func TestEvalEscapedDollarIsNotInterpolated(t *testing.T) {
	_, out := evalSrc(t, `$name = "Alice"; echo "literal: \$name, real: $name";`)
	want := "literal: $name, real: Alice"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestEvalComparisonsLooseEquality(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{`1 == 1;`, true},
		{`1 == "1";`, true}, // PHP-like loose numeric/string equality
		{`"5" == 5;`, true},
		{`0 == "abc";`, false}, // non-numeric string never loosely equals a number
		{`true == 1;`, false}, // no int<->bool coercion path in objectsEqual
		{`null == null;`, true},
		{`"a" != "b";`, true},
	}
	for _, tt := range tests {
		result, _ := evalSrc(t, tt.input)
		b, ok := result.(*Boolean)
		if !ok {
			t.Fatalf("input %q: want *Boolean, got %T (%s)", tt.input, result, result.Inspect())
		}
		if b.Value != tt.want {
			t.Errorf("input %q: got %v, want %v", tt.input, b.Value, tt.want)
		}
	}
}

func TestEvalTernary(t *testing.T) {
	_, out := evalSrc(t, `$x = 5; echo $x > 0 ? "pos" : "non-pos";`)
	if out != "pos" {
		t.Errorf("got %q", out)
	}
}

func TestEvalIfElseIfElseChain(t *testing.T) {
	src := `
	$x = 2;
	if ($x == 1) { echo "one"; }
	else if ($x == 2) { echo "two"; }
	else { echo "other"; }
	`
	_, out := evalSrc(t, src)
	if out != "two" {
		t.Errorf("got %q, want %q", out, "two")
	}
}

func TestEvalWhileLoop(t *testing.T) {
	_, out := evalSrc(t, `$i = 0; while ($i < 5) { echo $i; $i += 1; }`)
	if out != "01234" {
		t.Errorf("got %q, want %q", out, "01234")
	}
}

func TestEvalForLoop(t *testing.T) {
	_, out := evalSrc(t, `for ($i = 0; $i < 5; $i += 1) { echo $i; }`)
	if out != "01234" {
		t.Errorf("got %q, want %q", out, "01234")
	}
}

func TestEvalForeachPreservesInsertionOrder(t *testing.T) {
	_, out := evalSrc(t, `$m = ["b" => 2, "a" => 1, "c" => 3]; foreach ($m as $k => $v) { echo "$k:$v "; }`)
	if out != "b:2 a:1 c:3 " {
		t.Errorf("got %q, want %q", out, "b:2 a:1 c:3 ")
	}
}

func TestEvalArrayIndexedAndAssociative(t *testing.T) {
	result, _ := evalSrc(t, `$user = ["name" => "Alice", "age" => 30]; $user["name"];`)
	s, ok := result.(*String)
	if !ok || s.Value != "Alice" {
		t.Fatalf("got %#v, want String(Alice)", result)
	}
}

func TestEvalArrayAppendForm(t *testing.T) {
	_, out := evalSrc(t, `$a = [10, 20]; $a[] = 30; foreach ($a as $v) { echo $v . " "; }`)
	if out != "10 20 30 " {
		t.Errorf("got %q, want %q", out, "10 20 30 ")
	}
}

func TestEvalFunctionCallAndRecursion(t *testing.T) {
	src := `
	function fib($n) {
	    if ($n < 2) { return $n; }
	    return fib($n - 1) + fib($n - 2);
	}
	fib(10);
	`
	result, _ := evalSrc(t, src)
	i, ok := result.(*Integer)
	if !ok || i.Value != 55 {
		t.Fatalf("got %#v, want Integer(55)", result)
	}
}

func TestEvalFunctionWrongArgCountIsError(t *testing.T) {
	src := `function add($a, $b) { return $a + $b; } add(1);`
	result, _ := evalSrc(t, src)
	if !IsError(result) {
		t.Fatalf("want error for wrong arg count, got %T (%s)", result, result.Inspect())
	}
}

func TestEvalUndefinedVariableIsError(t *testing.T) {
	result, _ := evalSrc(t, `$y;`)
	if !IsError(result) {
		t.Fatalf("want error for undefined variable, got %T (%s)", result, result.Inspect())
	}
}

func TestEvalBuiltinCall(t *testing.T) {
	result, _ := evalSrc(t, `count([1, 2, 3]);`)
	i, ok := result.(*Integer)
	if !ok || i.Value != 3 {
		t.Fatalf("got %#v, want Integer(3)", result)
	}
}
