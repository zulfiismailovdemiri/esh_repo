package esh_vendors

import (
	"fmt"
	"strings"
)

type Node interface {
	String() string
}

type Statement interface {
	Node
	stmtNode()
}

type Expression interface {
	Node
	exprNode()
}

type Program struct {
	Statements []Statement
}

func (p *Program) String() string {
	var b strings.Builder
	for _, s := range p.Statements {
		b.WriteString(s.String())
	}
	return b.String()
}

// Statements

type AssignStatement struct {
	Name  Expression // Can be *Variable or *IndexExpression
	Value Expression
}

func (a *AssignStatement) stmtNode() {}
func (a *AssignStatement) String() string {
	return a.Name.String() + " = " + a.Value.String() + ";"
}

type EchoStatement struct {
	Value Expression
}

func (e *EchoStatement) stmtNode() {}
func (e *EchoStatement) String() string {
	return "echo " + e.Value.String() + ";"
}

type ReturnStatement struct {
	Value Expression
}

func (r *ReturnStatement) stmtNode() {}
func (r *ReturnStatement) String() string {
	if r.Value == nil {
		return "return;"
	}
	return "return " + r.Value.String() + ";"
}

type ExpressionStatement struct {
	Expression Expression
}

func (e *ExpressionStatement) stmtNode() {}
func (e *ExpressionStatement) String() string {
	return e.Expression.String() + ";"
}

type BlockStatement struct {
	Statements []Statement
}

func (b *BlockStatement) stmtNode() {}
func (b *BlockStatement) String() string {
	var sb strings.Builder
	sb.WriteString("{ ")
	for _, s := range b.Statements {
		sb.WriteString(s.String())
	}
	sb.WriteString(" }")
	return sb.String()
}

type IfStatement struct {
	Condition   Expression
	Consequence Statement
	Alternative Statement // nil, *IfStatement (else if), *BlockStatement, or any single Statement
}

func (i *IfStatement) stmtNode() {}
func (i *IfStatement) String() string {
	s := "if (" + i.Condition.String() + ") " + i.Consequence.String()
	if i.Alternative != nil {
		s += " else " + i.Alternative.String()
	}
	return s
}

type WhileStatement struct {
	Condition Expression
	Body      Statement
}

func (w *WhileStatement) stmtNode() {}
func (w *WhileStatement) String() string {
	return "while (" + w.Condition.String() + ") " + w.Body.String()
}

type ForStatement struct {
	Init      Statement
	Condition Expression
	Post      Statement
	Body      Statement
}

func (f *ForStatement) stmtNode() {}
func (f *ForStatement) String() string {
	return "for (...) " + f.Body.String()
}

// ForeachStatement represents:
//
//	foreach ($iterable as $value)            { ... }
//	foreach ($iterable as $key => $value)    { ... }
//
// KeyVar is nil for the value-only form.
type ForeachStatement struct {
	Iterable Expression
	KeyVar   *Variable
	ValueVar *Variable
	Body     Statement
}

func (f *ForeachStatement) stmtNode() {}
func (f *ForeachStatement) String() string {
	if f.KeyVar != nil {
		return "foreach (" + f.Iterable.String() + " as " + f.KeyVar.String() + " => " + f.ValueVar.String() + ") " + f.Body.String()
	}
	return "foreach (" + f.Iterable.String() + " as " + f.ValueVar.String() + ") " + f.Body.String()
}

type FunctionStatement struct {
	Name       string
	Parameters []*Variable
	Body       *BlockStatement
}

func (f *FunctionStatement) stmtNode() {}
func (f *FunctionStatement) String() string {
	parts := []string{}
	for _, p := range f.Parameters {
		parts = append(parts, p.String())
	}
	return "function " + f.Name + "(" + strings.Join(parts, ", ") + ") " + f.Body.String()
}

// Expressions

type Variable struct {
	Name string
}

func (v *Variable) exprNode()      {}
func (v *Variable) String() string { return v.Name } // Name already includes the leading '$'

type Identifier struct { // for function names
	Name string
}

func (i *Identifier) exprNode()      {}
func (i *Identifier) String() string { return i.Name }

type IntegerLiteral struct {
	Value int64
}

func (i *IntegerLiteral) exprNode()      {}
func (i *IntegerLiteral) String() string { return fmt.Sprintf("%d", i.Value) }

type FloatLiteral struct {
	Value float64
}

func (f *FloatLiteral) exprNode()      {}
func (f *FloatLiteral) String() string { return fmt.Sprintf("%g", f.Value) }

type StringLiteral struct {
	Value string
}

func (s *StringLiteral) exprNode()      {}
func (s *StringLiteral) String() string { return "\"" + s.Value + "\"" }

type BooleanLiteral struct {
	Value bool
}

func (b *BooleanLiteral) exprNode() {}
func (b *BooleanLiteral) String() string {
	if b.Value {
		return "true"
	}
	return "false"
}

type NullLiteral struct{}

func (n *NullLiteral) exprNode()      {}
func (n *NullLiteral) String() string { return "null" }

type PrefixExpression struct {
	Operator string
	Right    Expression
}

func (p *PrefixExpression) exprNode() {}
func (p *PrefixExpression) String() string {
	return "(" + p.Operator + p.Right.String() + ")"
}

type InfixExpression struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (i *InfixExpression) exprNode() {}
func (i *InfixExpression) String() string {
	return "(" + i.Left.String() + " " + i.Operator + " " + i.Right.String() + ")"
}

type TernaryExpression struct {
	Token       Token // The '?' token
	Condition   Expression
	Consequent  Expression
	Alternative Expression
}

func (te *TernaryExpression) exprNode() {}
func (te *TernaryExpression) String() string {
	return "(" + te.Condition.String() + " ? " + te.Consequent.String() + " : " + te.Alternative.String() + ")"
}

type CallExpression struct {
	Function  Expression
	Arguments []Expression
}

func (c *CallExpression) exprNode() {}
func (c *CallExpression) String() string {
	args := []string{}
	for _, a := range c.Arguments {
		args = append(args, a.String())
	}
	return c.Function.String() + "(" + strings.Join(args, ", ") + ")"
}

type ArrayLiteral struct {
	Elements []ArrayElement
}

type ArrayElement struct {
	Key   Expression // nil for indexed
	Value Expression
}

func (a *ArrayLiteral) exprNode() {}
func (a *ArrayLiteral) String() string {
	parts := []string{}
	for _, el := range a.Elements {
		if el.Key != nil {
			parts = append(parts, el.Key.String()+" => "+el.Value.String())
		} else {
			parts = append(parts, el.Value.String())
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

type IndexExpression struct {
	Left  Expression
	Index Expression
}

func (i *IndexExpression) exprNode() {}
func (i *IndexExpression) String() string {
	return "(" + i.Left.String() + "[" + i.Index.String() + "])"
}
