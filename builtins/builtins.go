package esh_vendors

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RegisterBuiltin lets extension packages (from esh_vendors/) add new
// built-in functions without editing this file directly.
func RegisterBuiltin(name string, fn func(*Environment, ...Object) Object) {
	builtins[name] = &Builtin{Name: name, Fn: fn}
}

func GetBuiltin(name string) *Builtin {
	return builtins[name]
}

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
		var sVal string
		switch v := args[1].(type) {
		case *String:
			sVal = v.Value
		case *Null:
			sVal = ""
		default:
			return &Error{Message: "explode second arg must be string"}
		}
		parts := strings.Split(sVal, sep.Value)
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

		subject := "Message"
		if s, exists := data.Items[ArrayKey{IsString: true, StrVal: "subject"}]; exists {
			subject = toString(s)
		}

		body := ""
		if b, exists := data.Items[ArrayKey{IsString: true, StrVal: "body"}]; exists {
			body = toString(b)
		}

		contentType := "text/plain; charset=UTF-8"
		if t, exists := data.Items[ArrayKey{IsString: true, StrVal: "type"}]; exists {
			if toString(t) == "html" {
				contentType = "text/html; charset=UTF-8"
			}
		}

		msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n%s",
			from, to.Value, subject, contentType, body)

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
	"urlencode": {Name: "urlencode", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "urlencode expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "urlencode expects string"}
		}
		return &String{Value: url.QueryEscape(s.Value)}
	}},
	"urldecode": {Name: "urldecode", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "urldecode expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "urldecode expects string"}
		}
		val, err := url.QueryUnescape(s.Value)
		if err != nil {
			return &Error{Message: "urldecode error: " + err.Error()}
		}
		return &String{Value: val}
	}},
	"sort": {Name: "sort", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "sort expects 1 argument"}
		}
		arr, ok := args[0].(*Array)
		if !ok {
			return &Error{Message: "sort expects array"}
		}
		slices.SortFunc(arr.Order, func(a, b ArrayKey) int {
			va := arr.Items[a]
			vb := arr.Items[b]
			return cmp.Compare(va.Inspect(), vb.Inspect())
		})
		return NULL_VALUE
	}},
	"array_merge": {Name: "array_merge", Fn: func(env *Environment, args ...Object) Object {
		out := NewArray()
		for _, arg := range args {
			arr, ok := arg.(*Array)
			if !ok {
				return &Error{Message: "array_merge expects arrays"}
			}
			for _, k := range arr.Order {
				nextIdx := nextArrayIntKey(out)
				out.Set(ArrayKey{IntVal: nextIdx}, arr.Items[k])
			}
		}
		return out
	}},
	"array_fill": {Name: "array_fill", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 3 {
			return &Error{Message: "array_fill expects (start_index, num, value)"}
		}
		start, ok1 := args[0].(*Integer)
		num, ok2 := args[1].(*Integer)
		if !ok1 || !ok2 {
			return &Error{Message: "array_fill expects integer for first two args"}
		}
		val := args[2]
		out := NewArray()
		for i := int64(0); i < num.Value; i++ {
			out.Set(ArrayKey{IntVal: start.Value + i}, val)
		}
		return out
	}},
	"floatval": {Name: "floatval", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "floatval expects 1 argument"}
		}
		return &Float{Value: toFloat(args[0])}
	}},
	"intval": {Name: "intval", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "intval expects 1 argument"}
		}
		switch v := args[0].(type) {
		case *Integer:
			return v
		case *Float:
			return &Integer{Value: int64(v.Value)}
		case *String:
			i, _ := strconv.ParseInt(v.Value, 10, 64)
			return &Integer{Value: i}
		default:
			return &Integer{Value: 0}
		}
	}},
	"rand_int": {Name: "rand_int", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "rand_int expects 2 arguments (min, max)"}
		}
		min, ok1 := args[0].(*Integer)
		max, ok2 := args[1].(*Integer)
		if !ok1 || !ok2 {
			return &Error{Message: "rand_int arguments must be integers"}
		}
		if min.Value > max.Value {
			return &Error{Message: "rand_int: min cannot be greater than max"}
		}
		diff := max.Value - min.Value + 1
		n, err := rand.Int(rand.Reader, big.NewInt(diff))
		if err != nil {
			return &Error{Message: "rand_int error: " + err.Error()}
		}
		return &Integer{Value: n.Int64() + min.Value}
	}},
	"rand": {Name: "rand", Fn: func(env *Environment, args ...Object) Object {
		n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
		if err != nil {
			return &Error{Message: "rand error: " + err.Error()}
		}
		return &Integer{Value: n.Int64()}
	}},
	"is_error": {Name: "is_error", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "is_error expects 1 argument"}
		}
		_, ok := args[0].(*Error)
		return boolToObj(ok)
	}},
	"sprintf": {Name: "sprintf", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 1 {
			return &Error{Message: "sprintf expects at least 1 argument"}
		}
		format, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "sprintf expects first argument to be string"}
		}
		if len(args) == 1 {
			return format
		}
		var goArgs []any
		for _, arg := range args[1:] {
			switch v := arg.(type) {
			case *Integer:
				goArgs = append(goArgs, v.Value)
			case *Float:
				goArgs = append(goArgs, v.Value)
			case *String:
				goArgs = append(goArgs, v.Value)
			case *Boolean:
				goArgs = append(goArgs, v.Value)
			case *Null:
				goArgs = append(goArgs, nil)
			default:
				goArgs = append(goArgs, v.Inspect())
			}
		}
		return &String{Value: fmt.Sprintf(format.Value, goArgs...)}
	}},
	"number_format": {Name: "number_format", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 1 {
			return &Error{Message: "number_format expects at least 1 argument"}
		}
		val := toFloat(args[0])
		decimals := int64(0)
		if len(args) >= 2 {
			if d, ok := args[1].(*Integer); ok {
				decimals = d.Value
			}
		}
		decPoint := "."
		if len(args) >= 3 {
			if s, ok := args[2].(*String); ok {
				decPoint = s.Value
			}
		}
		thousandsSep := ","
		if len(args) >= 4 {
			if s, ok := args[3].(*String); ok {
				thousandsSep = s.Value
			}
		}
		format := "%." + fmt.Sprintf("%d", decimals) + "f"
		s := fmt.Sprintf(format, val)
		if decPoint != "." || thousandsSep != "" {
			parts := strings.Split(s, ".")
			intPart := parts[0]
			decPart := ""
			if len(parts) > 1 {
				decPart = parts[1]
			}
			var res strings.Builder
			for i, r := range intPart {
				if i > 0 && (len(intPart)-i)%3 == 0 {
					res.WriteString(thousandsSep)
				}
				res.WriteRune(r)
			}
			if decPart != "" || decimals > 0 {
				res.WriteString(decPoint)
				res.WriteString(decPart)
			}
			s = res.String()
		}
		return &String{Value: s}
	}},
	"file_save": {Name: "file_save", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "file_save expects 2 arguments: tmp_path, dest_path"}
		}
		tmpPath, ok1 := args[0].(*String)
		destRel, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return &Error{Message: "file_save expects string arguments"}
		}
		dest := destRel.Value
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(env.BaseDir, dest)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return &Error{Message: "file_save: mkdir: " + err.Error()}
		}
		src, err := os.Open(tmpPath.Value)
		if err != nil {
			return &Error{Message: "file_save: open: " + err.Error()}
		}
		defer src.Close()
		dst, err := os.Create(dest)
		if err != nil {
			return &Error{Message: "file_save: create: " + err.Error()}
		}
		defer dst.Close()
		if _, err := io.Copy(dst, src); err != nil {
			return &Error{Message: "file_save: copy: " + err.Error()}
		}
		os.Remove(tmpPath.Value)
		return &String{Value: destRel.Value}
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
				// Variable names in the env are stored with their leading '$',
				// matching the lexer/parser. The caller writes
				// render("partial.es", ["username" => $u]) and the partial
				// reads $username, so we must register the local under "$key".
				name := k.StrVal
				if !strings.HasPrefix(name, "$") {
					name = "$" + name
				}
				inner.Set(name, vars.Items[k])
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
