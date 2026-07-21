package esh_vendors

import "testing"

func parseProgram(t *testing.T, src string) *Program {
	t.Helper()
	l := NewLexer(src)
	p := NewParser(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("parser errors for %q: %v", src, errs)
	}
	return prog
}

func TestParserOperatorPrecedence(t *testing.T) {
	tests := []struct{ input, want string }{
		{"1 + 2 * 3;", "(1 + (2 * 3));"},
		{"(1 + 2) * 3;", "((1 + 2) * 3);"},
		{"1 + 2 . 3;", "((1 + 2) . 3);"},
		{"true && false || true;", "((true && false) || true);"},
		{"1 < 2 == true;", "((1 < 2) == true);"},
		{`1 > 0 ? "yes" : "no";`, `((1 > 0) ? "yes" : "no");`},
		{"-1 + 2;", "((-1) + 2);"},
		{"!true;", "(!true);"},
		{"1 + 2 + 3;", "((1 + 2) + 3);"}, // left-associative
	}
	for _, tt := range tests {
		prog := parseProgram(t, tt.input)
		if got := prog.String(); got != tt.want {
			t.Errorf("input %q: got %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParserCompoundAssignDesugars(t *testing.T) {
	prog := parseProgram(t, "$x += 5;")
	if len(prog.Statements) != 1 {
		t.Fatalf("want 1 statement, got %d", len(prog.Statements))
	}
	stmt, ok := prog.Statements[0].(*AssignStatement)
	if !ok {
		t.Fatalf("want *AssignStatement, got %T", prog.Statements[0])
	}
	target, ok := stmt.Name.(*Variable)
	if !ok || target.Name != "$x" {
		t.Fatalf("want assign target $x, got %#v", stmt.Name)
	}
	infix, ok := stmt.Value.(*InfixExpression)
	if !ok || infix.Operator != "+" {
		t.Fatalf("want desugared '+' infix, got %#v", stmt.Value)
	}
	left, ok := infix.Left.(*Variable)
	if !ok || left.Name != "$x" {
		t.Fatalf("want left operand $x, got %#v", infix.Left)
	}
	right, ok := infix.Right.(*IntegerLiteral)
	if !ok || right.Value != 5 {
		t.Fatalf("want right operand 5, got %#v", infix.Right)
	}
}

func TestParserIfElseIfElse(t *testing.T) {
	prog := parseProgram(t, `if ($x == 1) { echo "one"; } else if ($x == 2) { echo "two"; } else { echo "other"; }`)
	stmt, ok := prog.Statements[0].(*IfStatement)
	if !ok {
		t.Fatalf("want *IfStatement, got %T", prog.Statements[0])
	}
	elseIf, ok := stmt.Alternative.(*IfStatement)
	if !ok {
		t.Fatalf("want else-if branch to be *IfStatement, got %T", stmt.Alternative)
	}
	if _, ok := elseIf.Alternative.(*BlockStatement); !ok {
		t.Fatalf("want final else branch to be *BlockStatement, got %T", elseIf.Alternative)
	}
}

func TestParserWhileAndFor(t *testing.T) {
	prog := parseProgram(t, `while ($i < 10) { $i += 1; }`)
	if _, ok := prog.Statements[0].(*WhileStatement); !ok {
		t.Fatalf("want *WhileStatement, got %T", prog.Statements[0])
	}

	prog = parseProgram(t, `for ($i = 0; $i < 5; $i += 1) { echo $i; }`)
	forStmt, ok := prog.Statements[0].(*ForStatement)
	if !ok {
		t.Fatalf("want *ForStatement, got %T", prog.Statements[0])
	}
	if forStmt.Init == nil || forStmt.Condition == nil || forStmt.Post == nil {
		t.Fatalf("want fully populated for-clauses, got %#v", forStmt)
	}
}

func TestParserForeachWithAndWithoutKey(t *testing.T) {
	prog := parseProgram(t, `foreach ($items as $v) { echo $v; }`)
	stmt, ok := prog.Statements[0].(*ForeachStatement)
	if !ok {
		t.Fatalf("want *ForeachStatement, got %T", prog.Statements[0])
	}
	if stmt.KeyVar != nil {
		t.Fatalf("want nil KeyVar for value-only form, got %#v", stmt.KeyVar)
	}
	if stmt.ValueVar.Name != "$v" {
		t.Fatalf("want ValueVar $v, got %#v", stmt.ValueVar)
	}

	prog = parseProgram(t, `foreach ($items as $k => $v) { echo $k; }`)
	stmt = prog.Statements[0].(*ForeachStatement)
	if stmt.KeyVar == nil || stmt.KeyVar.Name != "$k" {
		t.Fatalf("want KeyVar $k, got %#v", stmt.KeyVar)
	}
	if stmt.ValueVar.Name != "$v" {
		t.Fatalf("want ValueVar $v, got %#v", stmt.ValueVar)
	}
}

func TestParserFunctionStatement(t *testing.T) {
	prog := parseProgram(t, `function add($a, $b) { return $a + $b; }`)
	stmt, ok := prog.Statements[0].(*FunctionStatement)
	if !ok {
		t.Fatalf("want *FunctionStatement, got %T", prog.Statements[0])
	}
	if stmt.Name != "add" {
		t.Fatalf("want name %q, got %q", "add", stmt.Name)
	}
	if len(stmt.Parameters) != 2 || stmt.Parameters[0].Name != "$a" || stmt.Parameters[1].Name != "$b" {
		t.Fatalf("want params [$a, $b], got %#v", stmt.Parameters)
	}
	if len(stmt.Body.Statements) != 1 {
		t.Fatalf("want 1 body statement, got %d", len(stmt.Body.Statements))
	}
}

func TestParserArrayLiteralMixedKeys(t *testing.T) {
	prog := parseProgram(t, `$a = ["x", "k" => 99, "y"];`)
	stmt := prog.Statements[0].(*AssignStatement)
	arr, ok := stmt.Value.(*ArrayLiteral)
	if !ok {
		t.Fatalf("want *ArrayLiteral, got %T", stmt.Value)
	}
	if len(arr.Elements) != 3 {
		t.Fatalf("want 3 elements, got %d", len(arr.Elements))
	}
	if arr.Elements[0].Key != nil {
		t.Fatalf("element 0 should be auto-indexed (nil key), got %#v", arr.Elements[0].Key)
	}
	keyLit, ok := arr.Elements[1].Key.(*StringLiteral)
	if !ok || keyLit.Value != "k" {
		t.Fatalf(`element 1 key should be string "k", got %#v`, arr.Elements[1].Key)
	}
	if arr.Elements[2].Key != nil {
		t.Fatalf("element 2 should be auto-indexed (nil key), got %#v", arr.Elements[2].Key)
	}
}

func TestParserIndexAssignmentAppendForm(t *testing.T) {
	prog := parseProgram(t, `$a[] = 1;`)
	stmt := prog.Statements[0].(*AssignStatement)
	idx, ok := stmt.Name.(*IndexExpression)
	if !ok {
		t.Fatalf("want *IndexExpression target, got %T", stmt.Name)
	}
	if idx.Index != nil {
		t.Fatalf("want nil Index for append form, got %#v", idx.Index)
	}
}

func TestParserChainedIndexAssignment(t *testing.T) {
	prog := parseProgram(t, `$a[0][1] = 5;`)
	stmt := prog.Statements[0].(*AssignStatement)
	outer, ok := stmt.Name.(*IndexExpression)
	if !ok {
		t.Fatalf("want outer *IndexExpression, got %T", stmt.Name)
	}
	if _, ok := outer.Left.(*IndexExpression); !ok {
		t.Fatalf("want chained *IndexExpression as Left, got %T", outer.Left)
	}
}

func TestParserCommandStyleCall(t *testing.T) {
	prog := parseProgram(t, `include "foo.es";`)
	stmt, ok := prog.Statements[0].(*ExpressionStatement)
	if !ok {
		t.Fatalf("want *ExpressionStatement, got %T", prog.Statements[0])
	}
	call, ok := stmt.Expression.(*CallExpression)
	if !ok {
		t.Fatalf("want *CallExpression (command-style), got %T", stmt.Expression)
	}
	fnName, ok := call.Function.(*Identifier)
	if !ok || fnName.Name != "include" {
		t.Fatalf("want function identifier %q, got %#v", "include", call.Function)
	}
	if len(call.Arguments) != 1 {
		t.Fatalf("want 1 argument, got %d", len(call.Arguments))
	}
}

func TestParserRegularCallExpression(t *testing.T) {
	prog := parseProgram(t, `add(1, 2);`)
	stmt := prog.Statements[0].(*ExpressionStatement)
	call, ok := stmt.Expression.(*CallExpression)
	if !ok {
		t.Fatalf("want *CallExpression, got %T", stmt.Expression)
	}
	if len(call.Arguments) != 2 {
		t.Fatalf("want 2 arguments, got %d", len(call.Arguments))
	}
}

func TestParserReportsErrorOnInvalidSyntax(t *testing.T) {
	l := NewLexer(`if ($x { echo "broken"; }`) // missing closing paren
	p := NewParser(l)
	p.ParseProgram()
	if len(p.Errors()) == 0 {
		t.Fatalf("want parse errors for malformed input, got none")
	}
}
