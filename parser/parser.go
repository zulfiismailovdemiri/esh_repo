package esh_vendors

import (
	"fmt"
	"strconv"
)

const (
	_ int = iota
	LOWEST
	TERNARY     // ? :
	OR_PREC     // ||
	AND_PREC    // &&
	EQUALS      // == !=
	LESSGREATER // < > <= >=
	SUM         // + - .
	PRODUCT     // * / %
	PREFIX      // -X !X
	CALL        // foo(x)
	INDEX       // arr[x]
)

var precedences = map[TokenType]int{
	QUESTION: TERNARY,
	OR:       OR_PREC,
	AND:      AND_PREC,
	EQ:       EQUALS,
	NOT_EQ:   EQUALS,
	LT:       LESSGREATER,
	GT:       LESSGREATER,
	LE:       LESSGREATER,
	GE:       LESSGREATER,
	PLUS:     SUM,
	MINUS:    SUM,
	DOT:      SUM,
	SLASH:    PRODUCT,
	ASTERISK: PRODUCT,
	PERCENT:  PRODUCT,
	LPAREN:   CALL,
	LBRACKET: INDEX,
}

type Parser struct {
	l         *Lexer
	curToken  Token
	peekToken Token
	errors    []string

	prefixParseFns map[TokenType]func() Expression
	infixParseFns  map[TokenType]func(Expression) Expression
}

func NewParser(l *Lexer) *Parser {
	p := &Parser{l: l}

	p.prefixParseFns = map[TokenType]func() Expression{
		VAR:      p.parseVariable,
		IDENT:    p.parseIdentifier,
		INT:      p.parseIntegerLiteral,
		FLOAT:    p.parseFloatLiteral,
		STRING:   p.parseStringLiteral,
		TRUE:     p.parseBoolean,
		FALSE:    p.parseBoolean,
		NULL:     p.parseNull,
		BANG:     p.parsePrefixExpression,
		MINUS:    p.parsePrefixExpression,
		LPAREN:   p.parseGroupedExpression,
		LBRACKET: p.parseArrayLiteral,
	}

	p.infixParseFns = map[TokenType]func(Expression) Expression{
		PLUS:     p.parseInfixExpression,
		MINUS:    p.parseInfixExpression,
		SLASH:    p.parseInfixExpression,
		ASTERISK: p.parseInfixExpression,
		PERCENT:  p.parseInfixExpression,
		DOT:      p.parseInfixExpression,
		EQ:       p.parseInfixExpression,
		NOT_EQ:   p.parseInfixExpression,
		LT:       p.parseInfixExpression,
		GT:       p.parseInfixExpression,
		LE:       p.parseInfixExpression,
		GE:       p.parseInfixExpression,
		AND:      p.parseInfixExpression,
		OR:       p.parseInfixExpression,
		QUESTION: p.parseTernaryExpression,
		LPAREN:   p.parseCallExpression,
		LBRACKET: p.parseIndexExpression,
	}

	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) Errors() []string { return p.errors }

func (p *Parser) addError(msg string) {
	p.errors = append(p.errors, fmt.Sprintf("line %d: %s", p.curToken.Line, msg))
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) curTokenIs(t TokenType) bool  { return p.curToken.Type == t }
func (p *Parser) peekTokenIs(t TokenType) bool { return p.peekToken.Type == t }

func (p *Parser) expectPeek(t TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.addError(fmt.Sprintf("expected %s but got %s", t, p.peekToken.Type))
	return false
}

func (p *Parser) peekPrecedence() int {
	if pr, ok := precedences[p.peekToken.Type]; ok {
		return pr
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if pr, ok := precedences[p.curToken.Type]; ok {
		return pr
	}
	return LOWEST
}

func (p *Parser) ParseProgram() *Program {
	prog := &Program{}
	for !p.curTokenIs(EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			prog.Statements = append(prog.Statements, stmt)
		}
		p.nextToken()
	}
	return prog
}

// compoundAssignOp maps a compound-assignment token to its underlying operator.
// Used to desugar `$x += 5` into `$x = $x + 5`.
var compoundAssignOp = map[TokenType]string{
	PLUS_ASSIGN:  "+",
	MINUS_ASSIGN: "-",
	MUL_ASSIGN:   "*",
	DIV_ASSIGN:   "/",
	MOD_ASSIGN:   "%",
	DOT_ASSIGN:   ".",
}

func (p *Parser) parseStatement() Statement {
	switch p.curToken.Type {
	case SEMICOLON:
		// empty statement (e.g. stray ';' from template preprocessor)
		return nil
	case VAR:
		if p.peekTokenIs(ASSIGN) {
			return p.parseAssignStatement()
		}
		if _, ok := compoundAssignOp[p.peekToken.Type]; ok {
			return p.parseCompoundAssignStatement()
		}
		return p.parseExpressionStatement()
	case ECHO:
		return p.parseEchoStatement()
	case RETURN:
		return p.parseReturnStatement()
	case IF:
		return p.parseIfStatement()
	case WHILE:
		return p.parseWhileStatement()
	case FOR:
		return p.parseForStatement()
	case FOREACH:
		return p.parseForeachStatement()
	case FUNCTION:
		return p.parseFunctionStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseAssignStatement() *AssignStatement {
	stmt := &AssignStatement{Name: &Variable{Name: p.curToken.Literal}}
	if !p.expectPeek(ASSIGN) {
		return nil
	}
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)
	if p.peekTokenIs(SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

//	parseCompoundAssignStatement turns `$x += rhs;` into AssignStatement{
//	  Name: $x, Value: InfixExpression{Left: $x, Op: "+", Right: rhs}
//	}
//
// — i.e. it desugars new syntax into existing AST nodes.
func (p *Parser) parseCompoundAssignStatement() *AssignStatement {
	name := p.curToken.Literal
	p.nextToken() // move onto compound token (e.g. +=)
	op := compoundAssignOp[p.curToken.Type]
	p.nextToken() // move onto first token of rhs
	rhs := p.parseExpression(LOWEST)
	if p.peekTokenIs(SEMICOLON) {
		p.nextToken()
	}
	return &AssignStatement{
		Name: &Variable{Name: name},
		Value: &InfixExpression{
			Left:     &Variable{Name: name},
			Operator: op,
			Right:    rhs,
		},
	}
}

func (p *Parser) parseEchoStatement() *EchoStatement {
	stmt := &EchoStatement{}
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)
	if p.peekTokenIs(SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseReturnStatement() *ReturnStatement {
	stmt := &ReturnStatement{}
	if p.peekTokenIs(SEMICOLON) {
		p.nextToken()
		return stmt
	}
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)
	if p.peekTokenIs(SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseIfStatement() *IfStatement {
	stmt := &IfStatement{}
	if !p.expectPeek(LPAREN) {
		return nil
	}
	p.nextToken()
	stmt.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(RPAREN) {
		return nil
	}
	if !p.expectPeek(LBRACE) {
		return nil
	}
	stmt.Consequence = p.parseBlockStatement()
	if p.peekTokenIs(ELSE) {
		p.nextToken()
		if p.peekTokenIs(IF) {
			p.nextToken()
			stmt.Alternative = p.parseIfStatement()
		} else if p.peekTokenIs(LBRACE) {
			p.nextToken()
			stmt.Alternative = p.parseBlockStatement()
		} else {
			p.addError("expected '{' or 'if' after 'else'")
			return nil
		}
	}
	return stmt
}

func (p *Parser) parseWhileStatement() *WhileStatement {
	stmt := &WhileStatement{}
	if !p.expectPeek(LPAREN) {
		return nil
	}
	p.nextToken()
	stmt.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(RPAREN) {
		return nil
	}
	if !p.expectPeek(LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseForStatement() *ForStatement {
	stmt := &ForStatement{}
	if !p.expectPeek(LPAREN) {
		return nil
	}
	p.nextToken() // start of init
	stmt.Init = p.parseStatement()
	// init parser already consumed semicolon
	p.nextToken() // start of condition
	stmt.Condition = p.parseExpression(LOWEST)
	if !p.expectPeek(SEMICOLON) {
		return nil
	}
	p.nextToken() // start of post
	stmt.Post = p.parseSimpleStatement()
	if !p.expectPeek(RPAREN) {
		return nil
	}
	if !p.expectPeek(LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

// parseForeachStatement parses:
//
//	foreach ($expr as $value) { ... }
//	foreach ($expr as $key => $value) { ... }
func (p *Parser) parseForeachStatement() *ForeachStatement {
	stmt := &ForeachStatement{}
	if !p.expectPeek(LPAREN) {
		return nil
	}
	p.nextToken() // first token of iterable expression
	stmt.Iterable = p.parseExpression(LOWEST)
	if !p.expectPeek(AS) {
		return nil
	}
	if !p.expectPeek(VAR) {
		return nil
	}
	first := &Variable{Name: p.curToken.Literal}
	if p.peekTokenIs(ARROW) {
		// $key => $value form
		p.nextToken() // =>
		if !p.expectPeek(VAR) {
			return nil
		}
		stmt.KeyVar = first
		stmt.ValueVar = &Variable{Name: p.curToken.Literal}
	} else {
		stmt.ValueVar = first
	}
	if !p.expectPeek(RPAREN) {
		return nil
	}
	if !p.expectPeek(LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

// parseSimpleStatement parses a statement WITHOUT expecting a trailing
// semicolon (used for the post-expression in for loops).
func (p *Parser) parseSimpleStatement() Statement {
	if p.curTokenIs(VAR) && p.peekTokenIs(ASSIGN) {
		stmt := &AssignStatement{Name: &Variable{Name: p.curToken.Literal}}
		p.nextToken() // =
		p.nextToken()
		stmt.Value = p.parseExpression(LOWEST)
		return stmt
	}
	if p.curTokenIs(VAR) {
		if _, ok := compoundAssignOp[p.peekToken.Type]; ok {
			name := p.curToken.Literal
			p.nextToken() // compound token
			op := compoundAssignOp[p.curToken.Type]
			p.nextToken()
			rhs := p.parseExpression(LOWEST)
			return &AssignStatement{
				Name: &Variable{Name: name},
				Value: &InfixExpression{
					Left:     &Variable{Name: name},
					Operator: op,
					Right:    rhs,
				},
			}
		}
	}
	expr := p.parseExpression(LOWEST)
	return &ExpressionStatement{Expression: expr}
}

func (p *Parser) parseFunctionStatement() *FunctionStatement {
	stmt := &FunctionStatement{}
	if !p.expectPeek(IDENT) {
		return nil
	}
	stmt.Name = p.curToken.Literal
	if !p.expectPeek(LPAREN) {
		return nil
	}
	stmt.Parameters = p.parseFunctionParameters()
	if !p.expectPeek(LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseFunctionParameters() []*Variable {
	params := []*Variable{}
	if p.peekTokenIs(RPAREN) {
		p.nextToken()
		return params
	}
	p.nextToken()
	if !p.curTokenIs(VAR) {
		p.addError("function parameters must be variables")
		return params
	}
	params = append(params, &Variable{Name: p.curToken.Literal})
	for p.peekTokenIs(COMMA) {
		p.nextToken()
		p.nextToken()
		if !p.curTokenIs(VAR) {
			p.addError("function parameters must be variables")
			return params
		}
		params = append(params, &Variable{Name: p.curToken.Literal})
	}
	if !p.expectPeek(RPAREN) {
		return nil
	}
	return params
}

func (p *Parser) parseBlockStatement() *BlockStatement {
	block := &BlockStatement{}
	p.nextToken()
	for !p.curTokenIs(RBRACE) && !p.curTokenIs(EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}
	return block
}

func (p *Parser) parseExpressionStatement() *ExpressionStatement {
	stmt := &ExpressionStatement{Expression: p.parseExpression(LOWEST)}
	if p.peekTokenIs(SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseExpression(precedence int) Expression {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.addError(fmt.Sprintf("no parse function for %s (literal %q)", p.curToken.Type, p.curToken.Literal))
		return nil
	}
	leftExp := prefix()

	// Special case: if we have an identifier followed by something that isn't a known operator,
	// and we are at LOWEST precedence, it might be a command-style call: include "foo"
	if p.curTokenIs(IDENT) && precedence == LOWEST && !p.peekTokenIs(SEMICOLON) && !p.peekTokenIs(EOF) {
		if _, ok := precedences[p.peekToken.Type]; !ok {
			// Not an operator, try parsing as a call without parentheses
			return p.parseCommandStyleCall(leftExp)
		}
	}

	for !p.peekTokenIs(SEMICOLON) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = infix(leftExp)
	}
	return leftExp
}

// Prefix parse functions

func (p *Parser) parseVariable() Expression {
	return &Variable{Name: p.curToken.Literal}
}

func (p *Parser) parseCommandStyleCall(fn Expression) Expression {
	exp := &CallExpression{Function: fn}
	p.nextToken()
	exp.Arguments = append(exp.Arguments, p.parseExpression(LOWEST))
	for p.peekTokenIs(COMMA) {
		p.nextToken() // ,
		p.nextToken() // next arg
		exp.Arguments = append(exp.Arguments, p.parseExpression(LOWEST))
	}
	return exp
}

func (p *Parser) parseIdentifier() Expression {
	return &Identifier{Name: p.curToken.Literal}
}

func (p *Parser) parseIntegerLiteral() Expression {
	v, err := strconv.ParseInt(p.curToken.Literal, 10, 64)
	if err != nil {
		p.addError(fmt.Sprintf("could not parse %q as integer", p.curToken.Literal))
		return nil
	}
	return &IntegerLiteral{Value: v}
}

func (p *Parser) parseFloatLiteral() Expression {
	v, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		p.addError(fmt.Sprintf("could not parse %q as float", p.curToken.Literal))
		return nil
	}
	return &FloatLiteral{Value: v}
}

func (p *Parser) parseStringLiteral() Expression {
	return &StringLiteral{Value: p.curToken.Literal}
}

func (p *Parser) parseBoolean() Expression {
	return &BooleanLiteral{Value: p.curTokenIs(TRUE)}
}

func (p *Parser) parseNull() Expression {
	return &NullLiteral{}
}

func (p *Parser) parsePrefixExpression() Expression {
	expr := &PrefixExpression{Operator: p.curToken.Literal}
	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)
	return expr
}

func (p *Parser) parseGroupedExpression() Expression {
	p.nextToken()
	expr := p.parseExpression(LOWEST)
	if !p.expectPeek(RPAREN) {
		return nil
	}
	return expr
}

func (p *Parser) parseArrayLiteral() Expression {
	arr := &ArrayLiteral{}
	if p.peekTokenIs(RBRACKET) {
		p.nextToken()
		return arr
	}
	p.nextToken()
	arr.Elements = append(arr.Elements, p.parseArrayElement())
	for p.peekTokenIs(COMMA) {
		p.nextToken()
		if p.peekTokenIs(RBRACKET) { // trailing comma
			break
		}
		p.nextToken()
		arr.Elements = append(arr.Elements, p.parseArrayElement())
	}
	if !p.expectPeek(RBRACKET) {
		return nil
	}
	return arr
}

func (p *Parser) parseArrayElement() ArrayElement {
	first := p.parseExpression(LOWEST)
	if p.peekTokenIs(ARROW) {
		p.nextToken() // =>
		p.nextToken()
		val := p.parseExpression(LOWEST)
		return ArrayElement{Key: first, Value: val}
	}
	return ArrayElement{Value: first}
}

// Infix parse functions

func (p *Parser) parseInfixExpression(left Expression) Expression {
	expr := &InfixExpression{Left: left, Operator: p.curToken.Literal}
	prec := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(prec)
	return expr
}

func (p *Parser) parseTernaryExpression(left Expression) Expression {
	expr := &TernaryExpression{
		Token:     p.curToken,
		Condition: left,
	}

	p.nextToken() // skip '?'
	expr.Consequent = p.parseExpression(LOWEST)

	if !p.expectPeek(COLON) {
		return nil
	}

	p.nextToken() // skip ':'
	expr.Alternative = p.parseExpression(LOWEST)

	return expr
}

func (p *Parser) parseCallExpression(fn Expression) Expression {
	call := &CallExpression{Function: fn}
	call.Arguments = p.parseExpressionList(RPAREN)
	return call
}

func (p *Parser) parseIndexExpression(left Expression) Expression {
	expr := &IndexExpression{Left: left}
	p.nextToken()
	expr.Index = p.parseExpression(LOWEST)
	if !p.expectPeek(RBRACKET) {
		return nil
	}
	return expr
}

func (p *Parser) parseExpressionList(end TokenType) []Expression {
	list := []Expression{}
	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}
	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))
	for p.peekTokenIs(COMMA) {
		p.nextToken()
		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
	}
	if !p.expectPeek(end) {
		return nil
	}
	return list
}
