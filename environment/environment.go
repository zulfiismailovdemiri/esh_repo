package esh_vendors

import (
	"io"
	"net/http"
	"os"
)

type Environment struct {
	store       map[string]Object
	outer       *Environment
	Out         io.Writer
	Writer      http.ResponseWriter
	Request     *http.Request
	BaseDir     string // root for resolving include/render paths
	SessionID   string
	SessionData map[string]string
}

func NewEnvironment() *Environment {
	return &Environment{store: map[string]Object{}, Out: os.Stdout}
}

func NewEnclosedEnvironment(outer *Environment) *Environment {
	env := NewEnvironment()
	env.outer = outer
	env.Out = outer.Out
	env.Writer = outer.Writer
	env.Request = outer.Request
	env.BaseDir = outer.BaseDir
	env.SessionID = outer.SessionID
	env.SessionData = outer.SessionData
	return env
}

func (e *Environment) Get(name string) (Object, bool) {
	v, ok := e.store[name]
	if !ok && e.outer != nil {
		return e.outer.Get(name)
	}
	return v, ok
}

func (e *Environment) Set(name string, v Object) Object {
	e.store[name] = v
	return v
}
