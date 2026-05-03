package esh_vendors

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

var builtins = map[string]*Builtin{
	"print": {Name: "print", Fn: func(env *Environment, args ...Object) Object {
		parts := []string{}
		for _, a := range args {
			parts = append(parts, a.Inspect())
		}
		fmt.Fprint(env.Out, strings.Join(parts, ""))
		return NULL_VALUE
	}},
	"println": {Name: "println", Fn: func(env *Environment, args ...Object) Object {
		parts := []string{}
		for _, a := range args {
			parts = append(parts, a.Inspect())
		}
		fmt.Fprintln(env.Out, strings.Join(parts, ""))
		return NULL_VALUE
	}},
	"count": {Name: "count", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "count expects 1 argument"}
		}
		arr, ok := args[0].(*Array)
		if !ok {
			return &Error{Message: "count expects array"}
		}
		return &Integer{Value: int64(len(arr.Order))}
	}},
	"strlen": {Name: "strlen", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "strlen expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "strlen expects string"}
		}
		return &Integer{Value: int64(len(s.Value))}
	}},
	"strtoupper": {Name: "strtoupper", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "strtoupper expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "strtoupper expects string"}
		}
		return &String{Value: strings.ToUpper(s.Value)}
	}},
	"strtolower": {Name: "strtolower", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "strtolower expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "strtolower expects string"}
		}
		return &String{Value: strings.ToLower(s.Value)}
	}},
	"array_push": {Name: "array_push", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 2 {
			return &Error{Message: "array_push expects at least 2 arguments"}
		}
		arr, ok := args[0].(*Array)
		if !ok {
			return &Error{Message: "array_push expects array as first arg"}
		}
		for _, v := range args[1:] {
			nextIdx := nextArrayIntKey(arr)
			arr.Set(ArrayKey{IntVal: nextIdx}, v)
		}
		return &Integer{Value: int64(len(arr.Order))}
	}},
	"array_keys": {Name: "array_keys", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "array_keys expects 1 argument"}
		}
		arr, ok := args[0].(*Array)
		if !ok {
			return &Error{Message: "array_keys expects array"}
		}
		out := NewArray()
		for i, k := range arr.Order {
			if k.IsString {
				out.Set(ArrayKey{IntVal: int64(i)}, &String{Value: k.StrVal})
			} else {
				out.Set(ArrayKey{IntVal: int64(i)}, &Integer{Value: k.IntVal})
			}
		}
		return out
	}},
	"array_values": {Name: "array_values", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "array_values expects 1 argument"}
		}
		arr, ok := args[0].(*Array)
		if !ok {
			return &Error{Message: "array_values expects array"}
		}
		out := NewArray()
		for i, k := range arr.Order {
			out.Set(ArrayKey{IntVal: int64(i)}, arr.Items[k])
		}
		return out
	}},
	"range": {Name: "range", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 2 || len(args) > 3 {
			return &Error{Message: "range expects (start, end) or (start, end, step)"}
		}
		start, ok1 := args[0].(*Integer)
		end, ok2 := args[1].(*Integer)
		if !ok1 || !ok2 {
			return &Error{Message: "range expects integer args"}
		}
		step := int64(1)
		if len(args) == 3 {
			s, ok := args[2].(*Integer)
			if !ok || s.Value == 0 {
				return &Error{Message: "range step must be a non-zero integer"}
			}
			step = s.Value
		}
		out := NewArray()
		idx := int64(0)
		if step > 0 {
			for v := start.Value; v <= end.Value; v += step {
				out.Set(ArrayKey{IntVal: idx}, &Integer{Value: v})
				idx++
			}
		} else {
			for v := start.Value; v >= end.Value; v += step {
				out.Set(ArrayKey{IntVal: idx}, &Integer{Value: v})
				idx++
			}
		}
		return out
	}},
	"substr": {Name: "substr", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 2 || len(args) > 3 {
			return &Error{Message: "substr expects (string, start) or (string, start, length)"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "substr expects string"}
		}
		start, ok := args[1].(*Integer)
		if !ok {
			return &Error{Message: "substr start must be integer"}
		}
		runes := []rune(s.Value)
		from := int(start.Value)
		if from < 0 {
			from = len(runes) + from
		}
		if from < 0 {
			from = 0
		}
		if from > len(runes) {
			return &String{Value: ""}
		}
		to := len(runes)
		if len(args) == 3 {
			length, ok := args[2].(*Integer)
			if !ok {
				return &Error{Message: "substr length must be integer"}
			}
			to = from + int(length.Value)
			if to > len(runes) {
				to = len(runes)
			}
			if to < from {
				to = from
			}
		}
		return &String{Value: string(runes[from:to])}
	}},
	"implode": {Name: "implode", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "implode expects (separator, array)"}
		}
		sep, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "implode separator must be string"}
		}
		arr, ok := args[1].(*Array)
		if !ok {
			return &Error{Message: "implode second arg must be array"}
		}
		parts := []string{}
		for _, k := range arr.Order {
			parts = append(parts, toString(arr.Items[k]))
		}
		return &String{Value: strings.Join(parts, sep.Value)}
	}},
	"explode": {Name: "explode", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "explode expects (separator, string)"}
		}
		sep, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "explode separator must be string"}
		}
		s, ok := args[1].(*String)
		if !ok {
			return &Error{Message: "explode second arg must be string"}
		}
		parts := strings.Split(s.Value, sep.Value)
		out := NewArray()
		for i, p := range parts {
			out.Set(ArrayKey{IntVal: int64(i)}, &String{Value: p})
		}
		return out
	}},
	"is_int": {Name: "is_int", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "is_int expects 1 argument"}
		}
		return boolToObj(args[0].Type() == OBJ_INT)
	}},
	"is_string": {Name: "is_string", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "is_string expects 1 argument"}
		}
		return boolToObj(args[0].Type() == OBJ_STRING)
	}},
	"is_array": {Name: "is_array", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "is_array expects 1 argument"}
		}
		return boolToObj(args[0].Type() == OBJ_ARRAY)
	}},
	"floor": {Name: "floor", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "floor expects 1 argument"}
		}
		return &Integer{Value: int64(math.Floor(toFloat(args[0])))}
	}},
	"abs": {Name: "abs", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "abs expects 1 argument"}
		}
		switch v := args[0].(type) {
		case *Integer:
			if v.Value < 0 {
				return &Integer{Value: -v.Value}
			}
			return v
		case *Float:
			return &Float{Value: math.Abs(v.Value)}
		}
		return &Error{Message: "abs expects number"}
	}},
	"env": {Name: "env", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 1 || len(args) > 2 {
			return &Error{Message: "env expects (name) or (name, default)"}
		}
		name, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "env name must be string"}
		}
		val := os.Getenv(name.Value)
		if val == "" {
			if len(args) == 2 {
				return args[1]
			}
			return NULL_VALUE
		}
		return &String{Value: val}
	}},
	"log_info": {Name: "log_info", Fn: func(env *Environment, args ...Object) Object {
		enabled := os.Getenv("ESH_LOG_INFO_ENABLED")
		if enabled != "true" && enabled != "1" {
			return NULL_VALUE
		}
		parts := []string{}
		for _, a := range args {
			parts = append(parts, a.Inspect())
		}
		msg := strings.Join(parts, "")
		fmt.Fprintf(env.Out, "\n<!-- [LOG] %s -->\n", msg)
		return NULL_VALUE
	}},
	"var_dump": {Name: "var_dump", Fn: func(env *Environment, args ...Object) Object {
		parts := []string{}
		for _, a := range args {
			parts = append(parts, a.Inspect())
		}
		msg := strings.Join(parts, ", ")
		if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
			fmt.Fprintf(env.Out, "\n<!-- [VAR_DUMP] %s -->\n", msg)
		} else {
			fmt.Fprintln(env.Out, msg)
		}
		return NULL_VALUE
	}},
	"mail": {Name: "mail", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "mail expects (to, data)"}
		}
		to, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "mail to address must be string"}
		}
		data, ok := args[1].(*Array)
		if !ok {
			return &Error{Message: "mail data must be array"}
		}

		// SMTP Config from env
		host := os.Getenv("SMTP_HOST")
		port := os.Getenv("SMTP_PORT")
		user := os.Getenv("SMTP_USER")
		pass := os.Getenv("SMTP_PASS")
		from := os.Getenv("SMTP_FROM")

		if host == "" {
			host = "localhost"
		}
		if port == "" {
			port = "1025" // Mailpit default
		}

		subject := "Contact Form"
		if s, exists := data.Items[ArrayKey{IsString: true, StrVal: "subject"}]; exists {
			subject = toString(s)
		}

		body := ""
		if b, exists := data.Items[ArrayKey{IsString: true, StrVal: "body"}]; exists {
			body = toString(b)
		}

		msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", from, to.Value, subject, body)

		var auth smtp.Auth
		if user != "" || pass != "" {
			auth = smtp.PlainAuth("", user, pass, host)
		}

		err := smtp.SendMail(host+":"+port, auth, from, []string{to.Value}, []byte(msg))
		if err != nil {
			return &Error{Message: "mail: " + err.Error()}
		}

		return TRUE_VALUE
	}},
	"setcookie": {Name: "setcookie", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 2 || len(args) > 4 {
			return &Error{Message: "setcookie expects (name, value) or (name, value, expire) or (name, value, expire, path)"}
		}
		if env.Writer == nil {
			return &Error{Message: "setcookie only available in HTTP context"}
		}
		name, ok1 := args[0].(*String)
		val, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return &Error{Message: "setcookie name and value must be strings"}
		}

		cookie := &http.Cookie{
			Name:  name.Value,
			Value: val.Value,
		}

		if len(args) >= 3 {
			expire, ok := args[2].(*Integer)
			if !ok {
				return &Error{Message: "setcookie expire must be integer (unix timestamp)"}
			}
			cookie.Expires = time.Unix(expire.Value, 0)
		}

		if len(args) == 4 {
			path, ok := args[3].(*String)
			if !ok {
				return &Error{Message: "setcookie path must be string"}
			}
			cookie.Path = path.Value
		} else {
			cookie.Path = "/"
		}

		http.SetCookie(env.Writer, cookie)
		return TRUE_VALUE
	}},
	"time": {Name: "time", Fn: func(env *Environment, args ...Object) Object {
		return &Integer{Value: time.Now().Unix()}
	}},
}

// include and render are registered in init() rather than the map literal
// because their closures reference loadAndEval, which transitively references
// `builtins` itself — Go's package init refuses to compile that cycle even
// though it's safe at runtime.
func init() {
	// include(path) — runs another .es file in the CURRENT scope and writes
	// its output to env.Out. Like PHP's `include`. Use for shared partials.
	builtins["include"] = &Builtin{Name: "include", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "include expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "include path must be string"}
		}
		target, err := resolveIncludePath(env.BaseDir, s.Value)
		if err != nil {
			return &Error{Message: "include: " + err.Error()}
		}
		if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
			fmt.Fprintf(env.Out, "\n<!-- [INCLUDE] %s -->\n", s.Value)
		}
		if e := loadAndEval(target, env); e != nil {
			return e
		}
		return NULL_VALUE
	}}

	// render(path [, vars]) — runs another .es file in a FRESH scope (with
	// the parent as outer for function/builtin lookup), passes in vars as
	// locals, captures all output to a string and returns it.
	builtins["render"] = &Builtin{Name: "render", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 1 || len(args) > 2 {
			return &Error{Message: "render expects (path) or (path, vars)"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "render path must be string"}
		}
		target, err := resolveIncludePath(env.BaseDir, s.Value)
		if err != nil {
			return &Error{Message: "render: " + err.Error()}
		}

		inner := NewEnclosedEnvironment(env)
		var buf bytes.Buffer
		inner.Out = &buf

		if len(args) == 2 {
			vars, ok := args[1].(*Array)
			if !ok {
				return &Error{Message: "render vars must be array"}
			}
			for _, k := range vars.Order {
				if !k.IsString {
					continue // skip numeric keys
				}
				inner.Set(k.StrVal, vars.Items[k])
			}
		}

		if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
			fmt.Fprintf(inner.Out, "\n<!-- [RENDER] %s -->\n", s.Value)
		}
		if e := loadAndEval(target, inner); e != nil {
			return e
		}
		return &String{Value: buf.String()}
	}}
}

func nextArrayIntKey(a *Array) int64 {
	max := int64(-1)
	for _, k := range a.Order {
		if !k.IsString && k.IntVal > max {
			max = k.IntVal
		}
	}
	return max + 1
}
