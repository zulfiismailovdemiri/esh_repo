package esh_vendors

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func Eval(node Node, env *Environment) Object {
	switch n := node.(type) {
	case *Program:
		return evalProgram(n, env)
	case *BlockStatement:
		return evalBlock(n, env)
	case *AssignStatement:
		// Allow $err = mail(...) to capture errors as values rather than
		// aborting the script. Callers can use is_error() to inspect them.
		val := Eval(n.Value, env)
		switch target := n.Name.(type) {
		case *Variable:
			env.Set(target.Name, val)
		case *IndexExpression:
			if err := evalIndexAssignment(target, val, env); IsError(err) {
				return err
			}
		default:
			return &Error{Message: "invalid assignment target"}
		}
		return NULL_VALUE
	case *EchoStatement:
		val := Eval(n.Value, env)
		if IsError(val) {
			return val
		}
		fmt.Fprint(env.Out, val.Inspect())
		return NULL_VALUE
	case *ReturnStatement:
		if n.Value == nil {
			return &ReturnValue{Value: NULL_VALUE}
		}
		val := Eval(n.Value, env)
		if IsError(val) {
			return val
		}
		return &ReturnValue{Value: val}
	case *ExpressionStatement:
		return Eval(n.Expression, env)
	case *IfStatement:
		return evalIf(n, env)
	case *WhileStatement:
		return evalWhile(n, env)
	case *ForStatement:
		return evalFor(n, env)
	case *ForeachStatement:
		return evalForeach(n, env)
	case *FunctionStatement:
		fn := &Function{Parameters: n.Parameters, Body: n.Body, Env: env}
		env.Set(n.Name, fn)
		return NULL_VALUE
	case *Variable:
		if v, ok := env.Get(n.Name); ok {
			return v
		}
		return &Error{Message: fmt.Sprintf("undefined variable %s", n.Name)} // Name already includes '$'
	case *Identifier:
		if v, ok := env.Get(n.Name); ok {
			return v
		}
		if b, ok := builtins[n.Name]; ok {
			return b
		}
		return &Error{Message: fmt.Sprintf("undefined identifier %s", n.Name)}
	case *IntegerLiteral:
		return &Integer{Value: n.Value}
	case *FloatLiteral:
		return &Float{Value: n.Value}
	case *StringLiteral:
		return &String{Value: interpolate(n.Value, env)}
	case *BooleanLiteral:
		if n.Value {
			return TRUE_VALUE
		}
		return FALSE_VALUE
	case *NullLiteral:
		return NULL_VALUE
	case *PrefixExpression:
		right := Eval(n.Right, env)
		if IsError(right) {
			return right
		}
		return evalPrefix(n.Operator, right)
	case *InfixExpression:
		left := Eval(n.Left, env)
		if IsError(left) {
			return left
		}
		right := Eval(n.Right, env)
		if IsError(right) {
			return right
		}
		return evalInfix(n.Operator, left, right)
	case *TernaryExpression:
		condition := Eval(n.Condition, env)
		if IsError(condition) {
			return condition
		}
		if isTruthy(condition) {
			return Eval(n.Consequent, env)
		}
		return Eval(n.Alternative, env)
	case *CallExpression:
		fn := Eval(n.Function, env)
		if IsError(fn) {
			return fn
		}
		args := []Object{}
		for _, a := range n.Arguments {
			v := Eval(a, env)
			if IsError(v) {
				return v
			}
			args = append(args, v)
		}
		return applyFunction(env, fn, args)
	case *ArrayLiteral:
		return evalArrayLiteral(n, env)
	case *IndexExpression:
		left := Eval(n.Left, env)
		if IsError(left) {
			return left
		}
		idx := Eval(n.Index, env)
		if IsError(idx) {
			return idx
		}
		return evalIndex(left, idx)
	}
	return &Error{Message: fmt.Sprintf("unknown node type %T", node)}
}

func evalProgram(p *Program, env *Environment) Object {
	var result Object = NULL_VALUE
	for _, stmt := range p.Statements {
		result = Eval(stmt, env)
		if rv, ok := result.(*ReturnValue); ok {
			return rv.Value
		}
		if IsError(result) {
			// exit() returns this sentinel; treat it as a clean termination.
			if errObj, _ := result.(*Error); errObj != nil && errObj.Message == "EXIT_SENTINEL" {
				return NULL_VALUE
			}
			return result
		}
	}
	return result
}

func evalBlock(b *BlockStatement, env *Environment) Object {
	var result Object = NULL_VALUE
	for _, stmt := range b.Statements {
		result = Eval(stmt, env)
		if result != nil {
			t := result.Type()
			if t == OBJ_RETURN || t == OBJ_ERROR {
				return result
			}
		}
	}
	return result
}

func evalIf(n *IfStatement, env *Environment) Object {
	cond := Eval(n.Condition, env)
	if IsError(cond) {
		return cond
	}
	if isTruthy(cond) {
		return Eval(n.Consequence, env)
	} else if n.Alternative != nil {
		return Eval(n.Alternative, env)
	}
	return NULL_VALUE
}

func evalWhile(n *WhileStatement, env *Environment) Object {
	for {
		cond := Eval(n.Condition, env)
		if IsError(cond) {
			return cond
		}
		if !isTruthy(cond) {
			break
		}
		res := Eval(n.Body, env)
		if res != nil {
			t := res.Type()
			if t == OBJ_RETURN || t == OBJ_ERROR {
				return res
			}
		}
	}
	return NULL_VALUE
}

func evalFor(n *ForStatement, env *Environment) Object {
	if n.Init != nil {
		if r := Eval(n.Init, env); IsError(r) {
			return r
		}
	}
	for {
		cond := Eval(n.Condition, env)
		if IsError(cond) {
			return cond
		}
		if !isTruthy(cond) {
			break
		}
		res := Eval(n.Body, env)
		if res != nil {
			t := res.Type()
			if t == OBJ_RETURN || t == OBJ_ERROR {
				return res
			}
		}
		if n.Post != nil {
			if r := Eval(n.Post, env); IsError(r) {
				return r
			}
		}
	}
	return NULL_VALUE
}

func evalForeach(n *ForeachStatement, env *Environment) Object {
	it := Eval(n.Iterable, env)
	if IsError(it) {
		return it
	}
	arr, ok := it.(*Array)
	if !ok {
		return &Error{Message: fmt.Sprintf("foreach expects array, got %s", it.Type())}
	}
	// Iterate in insertion order using arr.Order, which is exactly what PHP
	// semantics require for associative arrays.
	for _, k := range arr.Order {
		env.Set(n.ValueVar.Name, arr.Items[k])
		if n.KeyVar != nil {
			if k.IsString {
				env.Set(n.KeyVar.Name, &String{Value: k.StrVal})
			} else {
				env.Set(n.KeyVar.Name, &Integer{Value: k.IntVal})
			}
		}
		res := Eval(n.Body, env)
		if res != nil {
			t := res.Type()
			if t == OBJ_RETURN || t == OBJ_ERROR {
				return res
			}
		}
	}
	return NULL_VALUE
}

func evalPrefix(op string, right Object) Object {
	switch op {
	case "!":
		return boolToObj(!isTruthy(right))
	case "-":
		switch r := right.(type) {
		case *Integer:
			return &Integer{Value: -r.Value}
		case *Float:
			return &Float{Value: -r.Value}
		}
		return &Error{Message: fmt.Sprintf("cannot negate %s", right.Type())}
	}
	return &Error{Message: fmt.Sprintf("unknown prefix operator %s", op)}
}

func evalInfix(op string, left, right Object) Object {
	// Logical and equality ops work on any value type — check before type-specific branches
	switch op {
	case "&&":
		return boolToObj(isTruthy(left) && isTruthy(right))
	case "||":
		return boolToObj(isTruthy(left) || isTruthy(right))
	case "==":
		return boolToObj(objectsEqual(left, right))
	case "!=":
		return boolToObj(!objectsEqual(left, right))
	}

	// String concatenation
	if op == "." {
		return &String{Value: toString(left) + toString(right)}
	}

	// Numeric ops: promote int+float -> float
	if isNumeric(left) && isNumeric(right) {
		if hasFloatPart(left) || hasFloatPart(right) {
			return evalFloatInfix(op, toFloat(left), toFloat(right))
		}
		return evalIntInfix(op, toInt(left), toInt(right))
	}

	// String comparison
	if left.Type() == OBJ_STRING && right.Type() == OBJ_STRING {
		return evalStringInfix(op, left.(*String).Value, right.(*String).Value)
	}

	return &Error{Message: fmt.Sprintf("type mismatch: %s %s %s", left.Type(), op, right.Type())}
}

func evalIntInfix(op string, l, r int64) Object {
	switch op {
	case "+":
		return &Integer{Value: l + r}
	case "-":
		return &Integer{Value: l - r}
	case "*":
		return &Integer{Value: l * r}
	case "/":
		if r == 0 {
			return &Error{Message: "division by zero"}
		}
		if l%r == 0 {
			return &Integer{Value: l / r}
		}
		return &Float{Value: float64(l) / float64(r)}
	case "%":
		if r == 0 {
			return &Error{Message: "modulo by zero"}
		}
		return &Integer{Value: l % r}
	case "<":
		return boolToObj(l < r)
	case ">":
		return boolToObj(l > r)
	case "<=":
		return boolToObj(l <= r)
	case ">=":
		return boolToObj(l >= r)
	case "==":
		return boolToObj(l == r)
	case "!=":
		return boolToObj(l != r)
	}
	return &Error{Message: fmt.Sprintf("unknown int operator %s", op)}
}

func evalFloatInfix(op string, l, r float64) Object {
	switch op {
	case "+":
		return &Float{Value: l + r}
	case "-":
		return &Float{Value: l - r}
	case "*":
		return &Float{Value: l * r}
	case "/":
		if r == 0 {
			return &Error{Message: "division by zero"}
		}
		return &Float{Value: l / r}
	case "<":
		return boolToObj(l < r)
	case ">":
		return boolToObj(l > r)
	case "<=":
		return boolToObj(l <= r)
	case ">=":
		return boolToObj(l >= r)
	case "==":
		return boolToObj(l == r)
	case "!=":
		return boolToObj(l != r)
	}
	return &Error{Message: fmt.Sprintf("unknown float operator %s", op)}
}

func evalStringInfix(op string, l, r string) Object {
	switch op {
	case "==":
		return boolToObj(l == r)
	case "!=":
		return boolToObj(l != r)
	case "<":
		return boolToObj(l < r)
	case ">":
		return boolToObj(l > r)
	case "<=":
		return boolToObj(l <= r)
	case ">=":
		return boolToObj(l >= r)
	}
	return &Error{Message: fmt.Sprintf("unknown string operator %s", op)}
}

func evalArrayLiteral(n *ArrayLiteral, env *Environment) Object {
	arr := NewArray()
	autoIdx := int64(0)
	for _, el := range n.Elements {
		val := Eval(el.Value, env)
		if IsError(val) {
			return val
		}
		var key ArrayKey
		if el.Key == nil {
			key = ArrayKey{IntVal: autoIdx}
			autoIdx++
		} else {
			k := Eval(el.Key, env)
			if IsError(k) {
				return k
			}
			switch kk := k.(type) {
			case *Integer:
				key = ArrayKey{IntVal: kk.Value}
				if kk.Value >= autoIdx {
					autoIdx = kk.Value + 1
				}
			case *String:
				key = ArrayKey{IsString: true, StrVal: kk.Value}
			default:
				return &Error{Message: fmt.Sprintf("invalid array key type %s", k.Type())}
			}
		}
		arr.Set(key, val)
	}
	return arr
}

func evalIndex(left, idx Object) Object {
	arr, ok := left.(*Array)
	if !ok {
		// allow string indexing too
		if s, ok := left.(*String); ok {
			i, ok := idx.(*Integer)
			if !ok {
				return &Error{Message: "string index must be integer"}
			}
			if i.Value < 0 || i.Value >= int64(len(s.Value)) {
				return NULL_VALUE
			}
			return &String{Value: string(s.Value[i.Value])}
		}
		return &Error{Message: fmt.Sprintf("cannot index %s", left.Type())}
	}
	var key ArrayKey
	switch k := idx.(type) {
	case *Integer:
		key = ArrayKey{IntVal: k.Value}
	case *String:
		key = ArrayKey{IsString: true, StrVal: k.Value}
	default:
		return &Error{Message: fmt.Sprintf("invalid index type %s", idx.Type())}
	}
	if v, ok := arr.Items[key]; ok {
		return v
	}
	return NULL_VALUE
}

// evalIndexAssignment handles `$x[i] = v`, `$x[] = v` (append), and chained
// forms like `$x[a][b] = v`. The right-hand value has already been evaluated.
func evalIndexAssignment(n *IndexExpression, val Object, env *Environment) Object {
	left := Eval(n.Left, env)
	if IsError(left) {
		return left
	}
	arr, ok := left.(*Array)
	if !ok {
		// `$x[...]= ...` against an unset/non-array variable initialises
		// it as a fresh array on first use, mirroring PHP semantics.
		if v, isVar := n.Left.(*Variable); isVar {
			arr = NewArray()
			env.Set(v.Name, arr)
		} else {
			return &Error{Message: "index assignment target is not an array"}
		}
	}

	var key ArrayKey
	if n.Index == nil {
		// Append form $x[] = v — pick max int key + 1.
		maxIdx := int64(-1)
		for _, k := range arr.Order {
			if !k.IsString && k.IntVal > maxIdx {
				maxIdx = k.IntVal
			}
		}
		key = ArrayKey{IntVal: maxIdx + 1}
	} else {
		idx := Eval(n.Index, env)
		if IsError(idx) {
			return idx
		}
		switch i := idx.(type) {
		case *Integer:
			key = ArrayKey{IntVal: i.Value}
		case *String:
			key = ArrayKey{IsString: true, StrVal: i.Value}
		default:
			return &Error{Message: "array index must be integer or string"}
		}
	}

	arr.Set(key, val)
	return NULL_VALUE
}

func applyFunction(env *Environment, fn Object, args []Object) Object {
	switch f := fn.(type) {
	case *Function:
		if len(args) != len(f.Parameters) {
			return &Error{Message: fmt.Sprintf("function expects %d args, got %d", len(f.Parameters), len(args))}
		}
		ext := NewEnclosedEnvironment(f.Env)
		ext.Out = env.Out // inherit current output target, not the def-site one
		for i, p := range f.Parameters {
			ext.Set(p.Name, args[i])
		}
		res := Eval(f.Body, ext)
		if rv, ok := res.(*ReturnValue); ok {
			return rv.Value
		}
		if IsError(res) {
			return res
		}
		return NULL_VALUE
	case *Builtin:
		return f.Fn(env, args...)
	}
	return &Error{Message: fmt.Sprintf("not a function: %s", fn.Type())}
}

// helpers

var interpVarRe = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)

func interpolate(s string, env *Environment) string {
	result := interpVarRe.ReplaceAllStringFunc(s, func(match string) string {
		// Variables are stored under their '$'-prefixed name (see lexer.go),
		// so the lookup key must keep the '$' rather than strip it.
		if v, ok := env.Get(match); ok {
			return toString(v)
		}
		return ""
	})
	// A user-escaped "\$" was lexed into escapedDollarMarker (see lexer.go)
	// specifically so it survives the substitution pass above untouched;
	// only now do we turn it into the literal '$' the escape asked for.
	if strings.IndexByte(result, escapedDollarMarker) != -1 {
		result = strings.ReplaceAll(result, string(escapedDollarMarker), "$")
	}
	return result
}

func toString(o Object) string {
	switch v := o.(type) {
	case *String:
		return v.Value
	case *Integer:
		return strconv.FormatInt(v.Value, 10)
	case *Float:
		return strconv.FormatFloat(v.Value, 'g', -1, 64)
	case *Boolean:
		if v.Value {
			return "1"
		}
		return ""
	case *Null:
		return ""
	}
	return o.Inspect()
}

func toFloat(o Object) float64 {
	switch v := o.(type) {
	case *Integer:
		return float64(v.Value)
	case *Float:
		return v.Value
	case *String:
		f, _ := strconv.ParseFloat(v.Value, 64)
		return f
	}
	return 0
}

func toInt(o Object) int64 {
	switch v := o.(type) {
	case *Integer:
		return v.Value
	case *Float:
		return int64(v.Value)
	case *String:
		if i, err := strconv.ParseInt(v.Value, 10, 64); err == nil {
			return i
		}
		f, _ := strconv.ParseFloat(v.Value, 64)
		return int64(f)
	}
	return 0
}

func hasFloatPart(o Object) bool {
	if o.Type() == OBJ_FLOAT {
		return true
	}
	if s, ok := o.(*String); ok {
		return strings.ContainsAny(s.Value, ".eE")
	}
	return false
}

func isNumeric(o Object) bool {
	if o.Type() == OBJ_INT || o.Type() == OBJ_FLOAT {
		return true
	}
	if s, ok := o.(*String); ok {
		_, err := strconv.ParseFloat(s.Value, 64)
		return err == nil
	}
	return false
}

func isTruthy(o Object) bool {
	switch v := o.(type) {
	case *Null:
		return false
	case *Boolean:
		return v.Value
	case *Integer:
		return v.Value != 0
	case *Float:
		return v.Value != 0
	case *String:
		return v.Value != "" && v.Value != "0"
	case *Array:
		return len(v.Order) > 0
	}
	return true
}

func IsError(o Object) bool {
	return o != nil && o.Type() == OBJ_ERROR
}

func boolToObj(b bool) *Boolean {
	if b {
		return TRUE_VALUE
	}
	return FALSE_VALUE
}

func objectsEqual(a, b Object) bool {
	// Loose equality for PHP-like behavior
	if isNumeric(a) && isNumeric(b) {
		return toFloat(a) == toFloat(b)
	}

	// String <-> Number comparison
	if a.Type() == OBJ_STRING && isNumeric(b) {
		f, err := strconv.ParseFloat(a.(*String).Value, 64)
		return err == nil && f == toFloat(b)
	}
	if isNumeric(a) && b.Type() == OBJ_STRING {
		f, err := strconv.ParseFloat(b.(*String).Value, 64)
		return err == nil && toFloat(a) == f
	}

	if a.Type() != b.Type() {
		return false
	}
	switch av := a.(type) {
	case *String:
		return av.Value == b.(*String).Value
	case *Boolean:
		return av.Value == b.(*Boolean).Value
	case *Null:
		return true
	}
	return false
}
