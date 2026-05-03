package esh_vendors

func init() {
	RegisterBuiltin("hello_world", func(env *Environment, args ...Object) Object {
		return &String{Value: "Hello from esh_repo!"}
	})
}
