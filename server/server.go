package esh_vendors

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func Serve(dir, addr string, version string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("invalid dir: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("cannot read dir: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", absDir)
	}

	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		if err := InitDB(dsn); err != nil {
			fmt.Fprintf(os.Stderr, "warning: db connect failed: %v\n", err)
		} else {
			fmt.Println("esh: database connected")
		}
	}

	fmt.Printf("esh %s — serving %s on http://localhost%s\n", version, absDir, addr)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleRequest(w, r, absDir)
	})
	return http.ListenAndServe(addr, mux)
}

func handleRequest(w http.ResponseWriter, r *http.Request, root string) {
	urlPath := r.URL.Path
	if urlPath == "/" {
		urlPath = "/index.es"
	}
	if urlPath == "/admin" || urlPath == "/admin/" {
		urlPath = "/admin/index.es"
	}

	// Resolve and ensure the request stays inside root
	clean := filepath.Clean(urlPath)
	target := filepath.Join(root, clean)
	if !strings.HasPrefix(target, root) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	info, err := os.Stat(target)
	if err != nil {
		// Try appending .es (clean URL: /about → /about.es)
		withExt := target + ".es"
		if _, err2 := os.Stat(withExt); err2 == nil {
			target = withExt
		} else {
			// Fall back to front controller (index.es) for DB-backed routes
			target = filepath.Join(root, "index.es")
			if _, err3 := os.Stat(target); err3 != nil {
				http.NotFound(w, r)
				return
			}
		}
	} else if info.IsDir() {
		// try <dir>/index.es
		idx := filepath.Join(target, "index.es")
		if _, err := os.Stat(idx); err == nil {
			target = idx
		} else {
			http.NotFound(w, r)
			return
		}
	}

	// Static (non-.es) files served as-is
	ext := filepath.Ext(target)
	if ext != ".es" {
		http.ServeFile(w, r, target)
		return
	}

	src, err := os.ReadFile(target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Each request gets a fresh environment whose output is the response
	env := NewEnvironment()
	env.Out = w
	env.Writer = w
	env.Request = r
	env.BaseDir = root

	// Inject HTTP request data
	post := NewArray()
	filesArr := NewArray()
	if r.Method == "POST" {
		ct := r.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "multipart/form-data") {
			if err := r.ParseMultipartForm(32 << 20); err == nil && r.MultipartForm != nil {
				for k, v := range r.MultipartForm.Value {
					setFormField(post, k, v)
				}
				for fieldName, fhs := range r.MultipartForm.File {
					if len(fhs) == 0 {
						continue
					}
					fh := fhs[0]
					if fh.Filename == "" {
						continue
					}
					f, err := fh.Open()
					if err != nil {
						continue
					}
					tmp, err := os.CreateTemp("", "esh-upload-*")
					if err != nil {
						f.Close()
						continue
					}
					io.Copy(tmp, f)
					f.Close()
					tmp.Close()
					info := NewArray()
					info.Set(ArrayKey{IsString: true, StrVal: "name"}, &String{Value: fh.Filename})
					info.Set(ArrayKey{IsString: true, StrVal: "tmp_name"}, &String{Value: tmp.Name()})
					info.Set(ArrayKey{IsString: true, StrVal: "type"}, &String{Value: fh.Header.Get("Content-Type")})
					info.Set(ArrayKey{IsString: true, StrVal: "size"}, &Integer{Value: fh.Size})
					info.Set(ArrayKey{IsString: true, StrVal: "error"}, &Integer{Value: 0})
					filesArr.Set(ArrayKey{IsString: true, StrVal: fieldName}, info)
				}
			}
		} else {
			r.ParseForm()
			for k, v := range r.PostForm {
				setFormField(post, k, v)
			}
		}
	}
	env.Set("$_POST", post)
	env.Set("$_FILES", filesArr)

	// Inject $_GET query parameters
	getArr := NewArray()
	for k, v := range r.URL.Query() {
		setFormField(getArr, k, v)
	}
	env.Set("$_GET", getArr)

	cookieArr := NewArray()
	for _, c := range r.Cookies() {
		cookieArr.Set(ArrayKey{IsString: true, StrVal: c.Name}, &String{Value: c.Value})
	}
	env.Set("$_COOKIE", cookieArr)

	method := &String{Value: r.Method}
	env.Set("$_METHOD", method)

	// Inject $_SERVER
	serverArr := NewArray()
	requestURI := r.URL.RequestURI()
	if xOriginalURI := r.Header.Get("X-Original-URI"); xOriginalURI != "" {
		requestURI = xOriginalURI
	}
	serverArr.Set(ArrayKey{IsString: true, StrVal: "REQUEST_URI"}, &String{Value: requestURI})
	serverArr.Set(ArrayKey{IsString: true, StrVal: "PATH_INFO"}, &String{Value: r.URL.Path})
	serverArr.Set(ArrayKey{IsString: true, StrVal: "SCRIPT_NAME"}, &String{Value: r.URL.Path})
	serverArr.Set(ArrayKey{IsString: true, StrVal: "REQUEST_METHOD"}, &String{Value: r.Method})
	serverArr.Set(ArrayKey{IsString: true, StrVal: "REMOTE_ADDR"}, &String{Value: r.RemoteAddr})
	env.Set("$_SERVER", serverArr)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
		// Previously this injected an HTML banner into every response, but
		// that leaked into pages that had already committed headers/redirects.
		// Logging is still emitted to stderr by the request logger.
	}

	if !runSilent(string(src), env) {
		// Errors already written to stderr by run(); write a hint to the
		// browser too, but only if we haven't streamed any body yet (best-effort)
		if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
			fmt.Fprint(w, "\n<!-- ESH ERROR: see server log for details -->")
		} else {
			fmt.Fprint(w, "\n<!-- esh: parse or runtime error, see server log -->")
		}
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	logRequest(r, target)
}

// runSilent is like run() but never prints values (no REPL), used by server.
func runSilent(src string, env *Environment) bool {
	src = PreprocessTemplate(src)
	l := NewLexer(src)
	p := NewParser(l)
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
			for _, e := range errs {
				fmt.Fprintf(env.Out, "\n<!-- [PARSE_ERROR] %s -->\n", e)
			}
		}
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "parse error:", e)
		}
		return false
	}
	result := Eval(prog, env)
	if IsError(result) {
		fmt.Fprintln(os.Stderr, result.Inspect())
		return false
	}
	return true
}

func logRequest(r *http.Request, target string) {
	fmt.Printf("  %s %s -> %s\n", r.Method, r.URL.Path, target)
}

// setFormField stores values into a target Array using PHP-style semantics:
//   - "key[]" appends every value to a sub-array under "key"
//   - if a single key occurs with multiple values it is also collected into an array
//   - otherwise the single value is stored as a string under the key
func setFormField(target *Array, k string, v []string) {
	if len(v) == 0 {
		return
	}
	if strings.HasSuffix(k, "[]") {
		baseKey := strings.TrimSuffix(k, "[]")
		arrKey := ArrayKey{IsString: true, StrVal: baseKey}
		var sub *Array
		if existing, ok := target.Items[arrKey]; ok && existing.Type() == OBJ_ARRAY {
			sub = existing.(*Array)
		} else {
			sub = NewArray()
			target.Set(arrKey, sub)
		}
		for _, val := range v {
			sub.Set(ArrayKey{IntVal: int64(len(sub.Order))}, &String{Value: val})
		}
		return
	}
	if len(v) > 1 {
		sub := NewArray()
		for _, val := range v {
			sub.Set(ArrayKey{IntVal: int64(len(sub.Order))}, &String{Value: val})
		}
		target.Set(ArrayKey{IsString: true, StrVal: k}, sub)
		return
	}
	target.Set(ArrayKey{IsString: true, StrVal: k}, &String{Value: v[0]})
}
