package esh_vendors

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// DB is the global PostgreSQL connection shared across requests.
var DB *sql.DB

// InitDB opens and pings a PostgreSQL connection, then ensures system tables exist.
func InitDB(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("db: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	DB = db
	ensureSessionsTable()
	return nil
}

func ensureSessionsTable() {
	if DB == nil {
		return
	}
	DB.Exec(`CREATE TABLE IF NOT EXISTS _esh_sessions (
		id TEXT PRIMARY KEY,
		data TEXT NOT NULL DEFAULT '{}',
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`)
}

// convertQuery converts ? placeholders to $1, $2, ... for PostgreSQL,
// and extracts the params array from the second argument.
func convertQuery(sqlStr string, secondArg Object) (string, []interface{}) {
	var sqlParams []interface{}
	if secondArg != nil {
		if arr, ok := secondArg.(*Array); ok {
			for _, k := range arr.Order {
				sqlParams = append(sqlParams, objectToSQLVal(arr.Items[k]))
			}
		}
	}
	var b strings.Builder
	n := 1
	for _, c := range sqlStr {
		if c == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteRune(c)
		}
	}
	return b.String(), sqlParams
}

func sqlValToObject(v interface{}) Object {
	if v == nil {
		return NULL_VALUE
	}
	switch val := v.(type) {
	case int64:
		return &Integer{Value: val}
	case float64:
		return &Float{Value: val}
	case bool:
		return boolToObj(val)
	case []byte:
		return &String{Value: string(val)}
	case string:
		return &String{Value: val}
	case time.Time:
		return &String{Value: val.Format("2006-01-02 15:04:05")}
	default:
		return &String{Value: fmt.Sprintf("%v", val)}
	}
}

func objectToSQLVal(obj Object) interface{} {
	switch v := obj.(type) {
	case *Integer:
		return v.Value
	case *Float:
		return v.Value
	case *Boolean:
		return v.Value
	case *Null:
		return nil
	default:
		return obj.Inspect()
	}
}

// ---- session helpers ----

func generateSessionID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func sessionExists(id string) bool {
	if DB == nil {
		return false
	}
	var exists bool
	DB.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM _esh_sessions WHERE id=$1 AND expires_at>NOW())",
		id,
	).Scan(&exists)
	return exists
}

func loadSessionData(id string) map[string]string {
	data := map[string]string{}
	if DB == nil {
		return data
	}
	var raw string
	err := DB.QueryRow(
		"SELECT data FROM _esh_sessions WHERE id=$1 AND expires_at>NOW()",
		id,
	).Scan(&raw)
	if err != nil {
		return data
	}
	json.Unmarshal([]byte(raw), &data)
	return data
}

func saveSessionData(id string, data map[string]string) {
	if DB == nil {
		return
	}
	raw, _ := json.Marshal(data)
	expires := time.Now().Add(24 * time.Hour)
	DB.Exec(`INSERT INTO _esh_sessions (id, data, expires_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO UPDATE SET data=$2, expires_at=$3`,
		id, string(raw), expires,
	)
}

func deleteSession(id string) {
	if DB == nil {
		return
	}
	DB.Exec("DELETE FROM _esh_sessions WHERE id=$1", id)
}

// HashPassword bcrypt-hashes a plain-text password (for CLI use).
func HashPassword(pw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// ---- builtin registration ----

var reDupHyphen = regexp.MustCompile(`-{2,}`)

func init() {
	builtins["db_connect"] = &Builtin{Name: "db_connect", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "db_connect expects 1 argument (dsn)"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "db_connect expects string dsn"}
		}
		if err := InitDB(s.Value); err != nil {
			return &Error{Message: err.Error()}
		}
		return TRUE_VALUE
	}}

	builtins["db_query"] = &Builtin{Name: "db_query", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 1 || len(args) > 2 {
			return &Error{Message: "db_query expects (sql) or (sql, params)"}
		}
		if DB == nil {
			return &Error{Message: "db_query: no database connection (set DATABASE_URL or call db_connect)"}
		}
		query, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "db_query: first argument must be a string"}
		}
		var second Object
		if len(args) == 2 {
			second = args[1]
		}
		sqlStr, sqlParams := convertQuery(query.Value, second)

		rows, err := DB.Query(sqlStr, sqlParams...)
		if err != nil {
			return &Error{Message: "db_query: " + err.Error()}
		}
		defer rows.Close()

		cols, err := rows.Columns()
		if err != nil {
			return &Error{Message: "db_query: " + err.Error()}
		}

		result := NewArray()
		rowIdx := int64(0)
		for rows.Next() {
			vals := make([]interface{}, len(cols))
			ptrs := make([]interface{}, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return &Error{Message: "db_query: scan: " + err.Error()}
			}
			row := NewArray()
			for i, col := range cols {
				row.Set(ArrayKey{IsString: true, StrVal: col}, sqlValToObject(vals[i]))
			}
			result.Set(ArrayKey{IntVal: rowIdx}, row)
			rowIdx++
		}
		if err := rows.Err(); err != nil {
			return &Error{Message: "db_query: " + err.Error()}
		}
		return result
	}}

	builtins["db_exec"] = &Builtin{Name: "db_exec", Fn: func(env *Environment, args ...Object) Object {
		if len(args) < 1 || len(args) > 2 {
			return &Error{Message: "db_exec expects (sql) or (sql, params)"}
		}
		if DB == nil {
			return &Error{Message: "db_exec: no database connection"}
		}
		query, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "db_exec: first argument must be a string"}
		}
		var second Object
		if len(args) == 2 {
			second = args[1]
		}
		sqlStr, sqlParams := convertQuery(query.Value, second)
		res, err := DB.Exec(sqlStr, sqlParams...)
		if err != nil {
			return &Error{Message: "db_exec: " + err.Error()}
		}
		n, _ := res.RowsAffected()
		// last_insert_id is 0 on PostgreSQL (lib/pq doesn't support it);
		// templates should use INSERT ... RETURNING id with db_query_one.
		lastId, _ := res.LastInsertId()

		result := NewArray()
		result.Set(ArrayKey{IsString: true, StrVal: "rows_affected"}, &Integer{Value: n})
		result.Set(ArrayKey{IsString: true, StrVal: "last_insert_id"}, &Integer{Value: lastId})
		return result
	}}

	// db_execute is an alias for db_exec
	builtins["db_execute"] = builtins["db_exec"]

	builtins["db_query_one"] = &Builtin{Name: "db_query_one", Fn: func(env *Environment, args ...Object) Object {
		res := builtins["db_query"].Fn(env, args...)
		if err, ok := res.(*Error); ok {
			return err
		}
		arr, ok := res.(*Array)
		if !ok || len(arr.Order) == 0 {
			return NULL_VALUE
		}
		return arr.Items[arr.Order[0]]
	}}

	builtins["db_last_insert_id"] = &Builtin{Name: "db_last_insert_id", Fn: func(env *Environment, args ...Object) Object {
		// PostgreSQL doesn't support sql.Result.LastInsertId(); templates
		// should normally use INSERT ... RETURNING id via db_query_one.
		// As a fallback we accept a sequence name and read its last value.
		if DB == nil {
			return &Error{Message: "db_last_insert_id: no database connection"}
		}
		var seqName string
		if len(args) > 0 {
			if s, ok := args[0].(*String); ok {
				seqName = s.Value
			}
		}
		if seqName == "" {
			return &Error{Message: "db_last_insert_id: sequence name required for PostgreSQL"}
		}
		var id int64
		if err := DB.QueryRow("SELECT last_value FROM " + seqName).Scan(&id); err != nil {
			return &Error{Message: "db_last_insert_id: " + err.Error()}
		}
		return &Integer{Value: id}
	}}

	builtins["session_start"] = &Builtin{Name: "session_start", Fn: func(env *Environment, args ...Object) Object {
		if env.Request == nil {
			return &Error{Message: "session_start only available in HTTP context"}
		}
		if env.SessionID != "" {
			return NULL_VALUE // already started
		}
		id := ""
		if c, err := env.Request.Cookie("ESH_SESSION"); err == nil {
			id = c.Value
		}
		if id == "" || !sessionExists(id) {
			id = generateSessionID()
			saveSessionData(id, map[string]string{})
		}
		env.SessionID = id
		env.SessionData = loadSessionData(id)

		// Inject _SESSION superglobal so templates can read session data
		// directly (e.g. $_SESSION["user_id"]) in addition to session_get().
		sessionArr := NewArray()
		for k, v := range env.SessionData {
			sessionArr.Set(ArrayKey{IsString: true, StrVal: k}, &String{Value: v})
		}
		env.Set("$_SESSION", sessionArr)

		// Always (re)issue the cookie with a TTL so the session sticks across
		// browser tabs / restarts during the day. Previously only emitted on
		// session creation, so refreshing extended nothing.
		if env.Writer != nil {
			http.SetCookie(env.Writer, &http.Cookie{
				Name:     "ESH_SESSION",
				Value:    id,
				Path:     "/",
				HttpOnly: true,
				MaxAge:   86400,
				SameSite: http.SameSiteLaxMode,
			})
		}
		return NULL_VALUE
	}}

	builtins["session_get"] = &Builtin{Name: "session_get", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "session_get expects 1 argument"}
		}
		key, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "session_get expects string key"}
		}
		if env.SessionData == nil {
			return NULL_VALUE
		}
		if val, exists := env.SessionData[key.Value]; exists {
			return &String{Value: val}
		}
		return NULL_VALUE
	}}

	builtins["session_set"] = &Builtin{Name: "session_set", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "session_set expects (key, value)"}
		}
		key, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "session_set key must be string"}
		}
		if env.SessionID == "" {
			return &Error{Message: "session_set: session not started"}
		}
		if env.SessionData == nil {
			env.SessionData = map[string]string{}
		}
		env.SessionData[key.Value] = args[1].Inspect()
		// Keep _SESSION in sync so subsequent reads in the same request see it
		if sess, ok := env.Get("$_SESSION"); ok {
			if sessArr, ok := sess.(*Array); ok {
				sessArr.Set(ArrayKey{IsString: true, StrVal: key.Value}, args[1])
			}
		}
		saveSessionData(env.SessionID, env.SessionData)
		return NULL_VALUE
	}}

	builtins["session_destroy"] = &Builtin{Name: "session_destroy", Fn: func(env *Environment, args ...Object) Object {
		if env.SessionID != "" {
			deleteSession(env.SessionID)
			env.SessionData = map[string]string{}
			env.SessionID = ""
		}
		env.Set("$_SESSION", NewArray())
		if env.Writer != nil {
			http.SetCookie(env.Writer, &http.Cookie{
				Name:    "ESH_SESSION",
				Value:   "",
				Path:    "/",
				Expires: time.Unix(0, 0),
				MaxAge:  -1,
			})
		}
		return NULL_VALUE
	}}

	builtins["csrf_token"] = &Builtin{Name: "csrf_token", Fn: func(env *Environment, args ...Object) Object {
		if env.SessionID == "" {
			return &Error{Message: "csrf_token: session not started (call session_start() first)"}
		}
		if env.SessionData == nil {
			env.SessionData = map[string]string{}
		}
		token, ok := env.SessionData["_csrf"]
		if !ok || token == "" {
			token = generateSessionID()
			env.SessionData["_csrf"] = token
			saveSessionData(env.SessionID, env.SessionData)
			if sess, ok := env.Get("$_SESSION"); ok {
				if sessArr, ok := sess.(*Array); ok {
					sessArr.Set(ArrayKey{IsString: true, StrVal: "_csrf"}, &String{Value: token})
				}
			}
		}
		return &String{Value: token}
	}}

	// csrf_verify does a constant-time comparison against the token stored in
	// the session, so a delete/edit action can require it was rendered by us
	// rather than forged as a bare link on another page.
	builtins["csrf_verify"] = &Builtin{Name: "csrf_verify", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "csrf_verify expects 1 argument"}
		}
		given, ok := args[0].(*String)
		if !ok || env.SessionData == nil {
			return FALSE_VALUE
		}
		expected, ok := env.SessionData["_csrf"]
		if !ok || expected == "" {
			return FALSE_VALUE
		}
		return boolToObj(subtle.ConstantTimeCompare([]byte(given.Value), []byte(expected)) == 1)
	}}

	builtins["password_hash"] = &Builtin{Name: "password_hash", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "password_hash expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "password_hash expects string"}
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(s.Value), bcrypt.DefaultCost)
		if err != nil {
			return &Error{Message: "password_hash: " + err.Error()}
		}
		return &String{Value: string(hash)}
	}}

	builtins["password_verify"] = &Builtin{Name: "password_verify", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "password_verify expects (password, hash)"}
		}
		pw, ok1 := args[0].(*String)
		h, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return &Error{Message: "password_verify expects string arguments"}
		}
		return boolToObj(bcrypt.CompareHashAndPassword([]byte(h.Value), []byte(pw.Value)) == nil)
	}}

	builtins["redirect"] = &Builtin{Name: "redirect", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "redirect expects 1 argument (url)"}
		}
		if env.Writer == nil {
			return &Error{Message: "redirect only available in HTTP context"}
		}
		u, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "redirect expects string url"}
		}
		http.Redirect(env.Writer, env.Request, u.Value, http.StatusFound)
		return NULL_VALUE
	}}

	builtins["header"] = &Builtin{Name: "header", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "header expects 1 argument"}
		}
		if env.Writer == nil {
			return &Error{Message: "header only available in HTTP context"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "header expects string"}
		}
		parts := strings.SplitN(s.Value, ":", 2)
		if len(parts) != 2 {
			return &Error{Message: "header expects 'Name: Value' format"}
		}
		name := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if strings.ToLower(name) == "location" {
			http.Redirect(env.Writer, env.Request, value, http.StatusFound)
		} else {
			env.Writer.Header().Set(name, value)
		}
		return NULL_VALUE
	}}

	// exit() returns a sentinel error that the evaluator recognises and
	// swallows, so the script terminates cleanly without bubbling up as
	// a "real" runtime error.
	builtins["exit"] = &Builtin{Name: "exit", Fn: func(env *Environment, args ...Object) Object {
		return &Error{Message: "EXIT_SENTINEL"}
	}}

	builtins["http_response_code"] = &Builtin{Name: "http_response_code", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "http_response_code expects 1 argument"}
		}
		if env.Writer == nil {
			return &Error{Message: "http_response_code only available in HTTP context"}
		}
		code, ok := args[0].(*Integer)
		if !ok {
			return &Error{Message: "http_response_code expects integer"}
		}
		env.Writer.WriteHeader(int(code.Value))
		return NULL_VALUE
	}}

	builtins["http_header"] = &Builtin{Name: "http_header", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "http_header expects (name, value)"}
		}
		if env.Writer == nil {
			return &Error{Message: "http_header only available in HTTP context"}
		}
		name, ok1 := args[0].(*String)
		val, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return &Error{Message: "http_header expects string arguments"}
		}
		env.Writer.Header().Set(name.Value, val.Value)
		return NULL_VALUE
	}}

	builtins["htmlspecialchars"] = &Builtin{Name: "htmlspecialchars", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "htmlspecialchars expects 1 argument"}
		}
		var raw string
		switch v := args[0].(type) {
		case *String:
			raw = v.Value
		case *Null:
			raw = ""
		case *Integer, *Float, *Boolean:
			raw = v.Inspect()
		default:
			return &Error{Message: "htmlspecialchars expects string"}
		}
		r := strings.NewReplacer(
			"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;",
		)
		return &String{Value: r.Replace(raw)}
	}}

	builtins["trim"] = &Builtin{Name: "trim", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "trim expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "trim expects string"}
		}
		return &String{Value: strings.TrimSpace(s.Value)}
	}}

	builtins["str_replace"] = &Builtin{Name: "str_replace", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 3 {
			return &Error{Message: "str_replace expects (search, replace, subject)"}
		}
		getStrings := func(obj Object) ([]string, bool) {
			if s, ok := obj.(*String); ok {
				return []string{s.Value}, true
			}
			if a, ok := obj.(*Array); ok {
				var res []string
				for _, k := range a.Order {
					v := a.Items[k]
					if s, ok := v.(*String); ok {
						res = append(res, s.Value)
					} else {
						return nil, false
					}
				}
				return res, true
			}
			return nil, false
		}
		searchArr, ok1 := getStrings(args[0])
		replaceArr, ok2 := getStrings(args[1])
		subject, ok3 := args[2].(*String)
		if !ok1 || !ok2 || !ok3 {
			return &Error{Message: "str_replace expects string or array of strings arguments"}
		}
		res := subject.Value
		if len(searchArr) == 1 && len(replaceArr) == 1 {
			res = strings.ReplaceAll(res, searchArr[0], replaceArr[0])
		} else {
			for i, s := range searchArr {
				r := ""
				if i < len(replaceArr) {
					r = replaceArr[i]
				}
				res = strings.ReplaceAll(res, s, r)
			}
		}
		return &String{Value: res}
	}}

	builtins["str_contains"] = &Builtin{Name: "str_contains", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 2 {
			return &Error{Message: "str_contains expects (haystack, needle)"}
		}
		h, ok1 := args[0].(*String)
		n, ok2 := args[1].(*String)
		if !ok1 || !ok2 {
			return &Error{Message: "str_contains expects string arguments"}
		}
		return boolToObj(strings.Contains(h.Value, n.Value))
	}}

	builtins["slugify"] = &Builtin{Name: "slugify", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "slugify expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "slugify expects string"}
		}
		var b strings.Builder
		for _, r := range strings.ToLower(s.Value) {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				b.WriteRune(r)
			} else if unicode.IsSpace(r) || r == '-' || r == '_' {
				b.WriteRune('-')
			}
		}
		result := reDupHyphen.ReplaceAllString(b.String(), "-")
		return &String{Value: strings.Trim(result, "-")}
	}}

	builtins["intval"] = &Builtin{Name: "intval", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "intval expects 1 argument"}
		}
		switch v := args[0].(type) {
		case *Integer:
			return v
		case *Float:
			return &Integer{Value: int64(v.Value)}
		case *String:
			n := int64(0)
			fmt.Sscanf(v.Value, "%d", &n)
			return &Integer{Value: n}
		case *Boolean:
			if v.Value {
				return &Integer{Value: 1}
			}
			return &Integer{Value: 0}
		default:
			return &Integer{Value: 0}
		}
	}}

	builtins["json_encode"] = &Builtin{Name: "json_encode", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "json_encode expects 1 argument"}
		}
		b, err := objectToJSON(args[0])
		if err != nil {
			return &Error{Message: "json_encode: " + err.Error()}
		}
		return &String{Value: string(b)}
	}}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithUnsafe()),
	)
	builtins["markdown"] = &Builtin{Name: "markdown", Fn: func(env *Environment, args ...Object) Object {
		if len(args) != 1 {
			return &Error{Message: "markdown expects 1 argument"}
		}
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "markdown expects string"}
		}
		var buf bytes.Buffer
		if err := md.Convert([]byte(s.Value), &buf); err != nil {
			return &Error{Message: "markdown: " + err.Error()}
		}
		return &String{Value: buf.String()}
	}}
}

func objectToJSON(obj Object) ([]byte, error) {
	switch v := obj.(type) {
	case *Null:
		return []byte("null"), nil
	case *Boolean:
		if v.Value {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case *Integer:
		return []byte(fmt.Sprintf("%d", v.Value)), nil
	case *Float:
		return []byte(fmt.Sprintf("%g", v.Value)), nil
	case *String:
		return json.Marshal(v.Value)
	case *Array:
		isList := true
		for i, k := range v.Order {
			if k.IsString || k.IntVal != int64(i) {
				isList = false
				break
			}
		}
		if isList {
			var arr []json.RawMessage
			for _, k := range v.Order {
				b, err := objectToJSON(v.Items[k])
				if err != nil {
					return nil, err
				}
				arr = append(arr, b)
			}
			return json.Marshal(arr)
		}
		m := map[string]json.RawMessage{}
		for _, k := range v.Order {
			b, err := objectToJSON(v.Items[k])
			if err != nil {
				return nil, err
			}
			m[k.String()] = b
		}
		return json.Marshal(m)
	}
	return []byte("null"), nil
}
