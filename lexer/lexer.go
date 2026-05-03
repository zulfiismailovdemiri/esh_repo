package esh_vendors

type Lexer struct {
	input        string
	position     int
	readPosition int
	ch           byte
	line         int
}

func NewLexer(input string) *Lexer {
	l := &Lexer{input: input, line: 1}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
}

func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	var tok Token
	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{EQ, "==", l.line}
		} else if l.peekChar() == '>' {
			l.readChar()
			tok = Token{ARROW, "=>", l.line}
		} else {
			tok = Token{ASSIGN, "=", l.line}
		}
	case '+':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{PLUS_ASSIGN, "+=", l.line}
		} else {
			tok = Token{PLUS, "+", l.line}
		}
	case '-':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{MINUS_ASSIGN, "-=", l.line}
		} else {
			tok = Token{MINUS, "-", l.line}
		}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{NOT_EQ, "!=", l.line}
		} else {
			tok = Token{BANG, "!", l.line}
		}
	case '*':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{MUL_ASSIGN, "*=", l.line}
		} else {
			tok = Token{ASTERISK, "*", l.line}
		}
	case '/':
		if l.peekChar() == '/' {
			for l.ch != '\n' && l.ch != 0 {
				l.readChar()
			}
			return l.NextToken()
		} else if l.peekChar() == '=' {
			l.readChar()
			tok = Token{DIV_ASSIGN, "/=", l.line}
		} else {
			tok = Token{SLASH, "/", l.line}
		}
	case '%':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{MOD_ASSIGN, "%=", l.line}
		} else {
			tok = Token{PERCENT, "%", l.line}
		}
	case '.':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{DOT_ASSIGN, ".=", l.line}
		} else {
			tok = Token{DOT, ".", l.line}
		}
	case '<':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{LE, "<=", l.line}
		} else {
			tok = Token{LT, "<", l.line}
		}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{GE, ">=", l.line}
		} else {
			tok = Token{GT, ">", l.line}
		}
	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok = Token{AND, "&&", l.line}
		} else {
			tok = Token{ILLEGAL, string(l.ch), l.line}
		}
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok = Token{OR, "||", l.line}
		} else {
			tok = Token{ILLEGAL, string(l.ch), l.line}
		}
	case ',':
		tok = Token{COMMA, ",", l.line}
	case ';':
		tok = Token{SEMICOLON, ";", l.line}
	case '(':
		tok = Token{LPAREN, "(", l.line}
	case ')':
		tok = Token{RPAREN, ")", l.line}
	case '{':
		tok = Token{LBRACE, "{", l.line}
	case '}':
		tok = Token{RBRACE, "}", l.line}
	case '[':
		tok = Token{LBRACKET, "[", l.line}
	case ']':
		tok = Token{RBRACKET, "]", l.line}
	case '?':
		tok = Token{QUESTION, "?", l.line}
	case ':':
		tok = Token{COLON, ":", l.line}
	case '"':
		tok = Token{STRING, l.readString(), l.line}
	case '#':
		for l.ch != '\n' && l.ch != 0 {
			l.readChar()
		}
		return l.NextToken()
	case '$':
		l.readChar()
		if isLetter(l.ch) {
			lit := l.readIdentifier()
			return Token{VAR, lit, l.line}
		}
		tok = Token{ILLEGAL, "$", l.line}
	case 0:
		tok = Token{EOF, "", l.line}
	default:
		if isLetter(l.ch) {
			lit := l.readIdentifier()
			return Token{LookupIdent(lit), lit, l.line}
		} else if isDigit(l.ch) {
			return l.readNumber()
		}
		tok = Token{ILLEGAL, string(l.ch), l.line}
	}

	l.readChar()
	return tok
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		if l.ch == '\n' {
			l.line++
		}
		l.readChar()
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.position]
}

func (l *Lexer) readNumber() Token {
	start := l.position
	isFloat := false
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		isFloat = true
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	lit := l.input[start:l.position]
	if isFloat {
		return Token{FLOAT, lit, l.line}
	}
	return Token{INT, lit, l.line}
}

func (l *Lexer) readString() string {
	var sb []byte
	for {
		l.readChar()
		if l.ch == 0 || l.ch == '"' {
			break
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				sb = append(sb, '\n')
			case 't':
				sb = append(sb, '\t')
			case 'r':
				sb = append(sb, '\r')
			case '\\':
				sb = append(sb, '\\')
			case '"':
				sb = append(sb, '"')
			case '$':
				sb = append(sb, '$')
			default:
				// If we don't recognize the escape, keep the backslash
				sb = append(sb, '\\')
				sb = append(sb, l.ch)
			}
			continue
		}
		sb = append(sb, l.ch)
	}
	return string(sb)
}

func isLetter(ch byte) bool {
	return ('a' <= ch && ch <= 'z') || ('A' <= ch && ch <= 'Z') || ch == '_'
}

func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}
