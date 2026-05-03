package esh_vendors

import (
	"fmt"
	"strings"
)

type ObjectType string

const (
	OBJ_INT      ObjectType = "INTEGER"
	OBJ_FLOAT    ObjectType = "FLOAT"
	OBJ_STRING   ObjectType = "STRING"
	OBJ_BOOL     ObjectType = "BOOLEAN"
	OBJ_NULL     ObjectType = "NULL"
	OBJ_ARRAY    ObjectType = "ARRAY"
	OBJ_RETURN   ObjectType = "RETURN_VALUE"
	OBJ_FUNCTION ObjectType = "FUNCTION"
	OBJ_BUILTIN  ObjectType = "BUILTIN"
	OBJ_ERROR    ObjectType = "ERROR"
)

type Object interface {
	Type() ObjectType
	Inspect() string
}

type Integer struct{ Value int64 }

func (i *Integer) Type() ObjectType { return OBJ_INT }
func (i *Integer) Inspect() string  { return fmt.Sprintf("%d", i.Value) }

type Float struct{ Value float64 }

func (f *Float) Type() ObjectType { return OBJ_FLOAT }
func (f *Float) Inspect() string  { return fmt.Sprintf("%g", f.Value) }

type String struct{ Value string }

func (s *String) Type() ObjectType { return OBJ_STRING }
func (s *String) Inspect() string  { return s.Value }

type Boolean struct{ Value bool }

func (b *Boolean) Type() ObjectType { return OBJ_BOOL }
func (b *Boolean) Inspect() string {
	if b.Value {
		return "true"
	}
	return "false"
}

type Null struct{}

func (n *Null) Type() ObjectType { return OBJ_NULL }
func (n *Null) Inspect() string  { return "null" }

// Array is PHP-style: ordered key/value pairs. Keys can be int or string.
// Indexed elements without explicit keys auto-assign sequential integer keys.
type Array struct {
	Order []ArrayKey
	Items map[ArrayKey]Object
}

type ArrayKey struct {
	IsString bool
	IntVal   int64
	StrVal   string
}

func (k ArrayKey) String() string {
	if k.IsString {
		return k.StrVal
	}
	return fmt.Sprintf("%d", k.IntVal)
}

func (a *Array) Type() ObjectType { return OBJ_ARRAY }
func (a *Array) Inspect() string {
	parts := []string{}
	for _, k := range a.Order {
		v := a.Items[k]
		if k.IsString {
			parts = append(parts, fmt.Sprintf("%q => %s", k.StrVal, v.Inspect()))
		} else {
			parts = append(parts, fmt.Sprintf("%d => %s", k.IntVal, v.Inspect()))
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func NewArray() *Array {
	return &Array{Items: map[ArrayKey]Object{}}
}

func (a *Array) Set(k ArrayKey, v Object) {
	if _, exists := a.Items[k]; !exists {
		a.Order = append(a.Order, k)
	}
	a.Items[k] = v
}

type ReturnValue struct{ Value Object }

func (r *ReturnValue) Type() ObjectType { return OBJ_RETURN }
func (r *ReturnValue) Inspect() string  { return r.Value.Inspect() }

type Function struct {
	Parameters []*Variable
	Body       *BlockStatement
	Env        *Environment
}

func (f *Function) Type() ObjectType { return OBJ_FUNCTION }
func (f *Function) Inspect() string {
	parts := []string{}
	for _, p := range f.Parameters {
		parts = append(parts, p.String())
	}
	return "function(" + strings.Join(parts, ", ") + ") {...}"
}

type BuiltinFn func(env *Environment, args ...Object) Object

type Builtin struct {
	Name string
	Fn   BuiltinFn
}

func (b *Builtin) Type() ObjectType { return OBJ_BUILTIN }
func (b *Builtin) Inspect() string  { return "builtin " + b.Name }

type Error struct{ Message string }

func (e *Error) Type() ObjectType { return OBJ_ERROR }
func (e *Error) Inspect() string  { return "ERROR: " + e.Message }

var (
	NULL_VALUE  = &Null{}
	TRUE_VALUE  = &Boolean{Value: true}
	FALSE_VALUE = &Boolean{Value: false}
)
