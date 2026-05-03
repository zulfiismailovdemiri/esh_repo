package esh_vendors

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	if r.Method == "POST" {
		r.ParseForm()
		for k, v := range r.PostForm {
			if len(v) > 0 {
				post.Set(ArrayKey{IsString: true, StrVal: k}, &String{Value: v[0]})
			}
		}
	}
	env.Set("_POST", post)

	// Inject $_GET query parameters
	getArr := NewArray()
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			getArr.Set(ArrayKey{IsString: true, StrVal: k}, &String{Value: v[0]})
		}
	}
	env.Set("_GET", getArr)

	cookieArr := NewArray()
	for _, c := range r.Cookies() {
		cookieArr.Set(ArrayKey{IsString: true, StrVal: c.Name}, &String{Value: c.Value})
	}
	env.Set("_COOKIE", cookieArr)

	method := &String{Value: r.Method}
	env.Set("_METHOD", method)

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
	env.Set("_SERVER", serverArr)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if os.Getenv("ESH_LOG_INFO_ENABLED") == "true" {
		fmt.Fprintf(w, "<!-- ESH LOGGING ENABLED: %s -->\n", time.Now().Format(time.RFC3339))
		fmt.Fprintf(w, "<div style=\"position:fixed;bottom:10px;right:10px;background:#ffeb3b;color:#000;padding:5px 10px;border:1px solid #fbc02d;border-radius:4px;font-family:sans-serif;font-size:12px;z-index:9999;box-shadow:0 2px 5px rgba(0,0,0,0.2);\">ESH Logging Active (View Source)</div>\n")
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
