package esh_vendors

type TokenType string

const (
	ILLEGAL TokenType = "ILLEGAL"
	EOF     TokenType = "EOF"

	IDENT  TokenType = "IDENT"
	VAR    TokenType = "VAR"
	INT    TokenType = "INT"
	FLOAT  TokenType = "FLOAT"
	STRING TokenType = "STRING"

	ASSIGN   TokenType = "="
	PLUS     TokenType = "+"
	MINUS    TokenType = "-"
	BANG     TokenType = "!"
	ASTERISK TokenType = "*"
	SLASH    TokenType = "/"
	PERCENT  TokenType = "%"
	DOT      TokenType = "."

	PLUS_ASSIGN  TokenType = "+="
	MINUS_ASSIGN TokenType = "-="
	MUL_ASSIGN   TokenType = "*="
	DIV_ASSIGN   TokenType = "/="
	MOD_ASSIGN   TokenType = "%="
	DOT_ASSIGN   TokenType = ".="

	LT     TokenType = "<"
	GT     TokenType = ">"
	LE     TokenType = "<="
	GE     TokenType = ">="
	EQ     TokenType = "=="
	NOT_EQ TokenType = "!="

	QUESTION TokenType = "?"
	COLON    TokenType = ":"

	AND TokenType = "&&"
	OR  TokenType = "||"

	ARROW TokenType = "=>"

	COMMA     TokenType = ","
	SEMICOLON TokenType = ";"
	LPAREN    TokenType = "("
	RPAREN    TokenType = ")"
	LBRACE    TokenType = "{"
	RBRACE    TokenType = "}"
	LBRACKET  TokenType = "["
	RBRACKET  TokenType = "]"

	FUNCTION TokenType = "FUNCTION"
	RETURN   TokenType = "RETURN"
	IF       TokenType = "IF"
	ELSE     TokenType = "ELSE"
	WHILE    TokenType = "WHILE"
	FOR      TokenType = "FOR"
	FOREACH  TokenType = "FOREACH"
	AS       TokenType = "AS"
	TRUE     TokenType = "TRUE"
	FALSE    TokenType = "FALSE"
	NULL     TokenType = "NULL"
	ECHO     TokenType = "ECHO"
)

type Token struct {
	Type    TokenType
	Literal string
	Line    int
}

var keywords = map[string]TokenType{
	"function": FUNCTION,
	"return":   RETURN,
	"if":       IF,
	"else":     ELSE,
	"while":    WHILE,
	"for":      FOR,
	"foreach":  FOREACH,
	"as":       AS,
	"true":     TRUE,
	"false":    FALSE,
	"null":     NULL,
	"echo":     ECHO,
}

func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}
