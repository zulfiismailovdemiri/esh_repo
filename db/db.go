package esh_vendors

import (
	"bytes"
	"crypto/rand"
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
		return &Integer{Value: n}
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
			if env.Writer != nil {
				http.SetCookie(env.Writer, &http.Cookie{
					Name:     "ESH_SESSION",
					Value:    id,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteLaxMode,
				})
			}
			saveSessionData(id, map[string]string{})
		}
		env.SessionID = id
		env.SessionData = loadSessionData(id)
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
		if env.SessionData == nil {
			env.SessionData = map[string]string{}
		}
		env.SessionData[key.Value] = args[1].Inspect()
		saveSessionData(env.SessionID, env.SessionData)
		return NULL_VALUE
	}}

	builtins["session_destroy"] = &Builtin{Name: "session_destroy", Fn: func(env *Environment, args ...Object) Object {
		if env.SessionID != "" {
			deleteSession(env.SessionID)
			env.SessionData = map[string]string{}
			env.SessionID = ""
		}
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
		s, ok := args[0].(*String)
		if !ok {
			return &Error{Message: "htmlspecialchars expects string"}
		}
		r := strings.NewReplacer(
			"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;",
		)
		return &String{Value: r.Replace(s.Value)}
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
		search, ok1 := args[0].(*String)
		replace, ok2 := args[1].(*String)
		subject, ok3 := args[2].(*String)
		if !ok1 || !ok2 || !ok3 {
			return &Error{Message: "str_replace expects string arguments"}
		}
		return &String{Value: strings.ReplaceAll(subject.Value, search.Value, replace.Value)}
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
