package query

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse parses the supported PromQL subset back into typed expressions
// (P2-05 task 10). The subset is exactly what Render emits: selectors
// with controlled matchers, rate, sum by, histogram_quantile, scalar
// literals and division. Anything else — unknown functions, aggregations
// beyond sum, unquoted values, comments — is rejected. The parser is
// total: arbitrary input never panics.
func Parse(text string) (Expression, error) {
	tokens, err := lex(text)
	if err != nil {
		return nil, err
	}
	parser := &exprParser{tokens: tokens}
	expression, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if !parser.done() {
		return nil, parser.errorf("unexpected trailing input")
	}
	return expression, nil
}

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdent
	tokenNumber
	tokenString
	tokenLeftBrace
	tokenRightBrace
	tokenLeftParen
	tokenRightParen
	tokenLeftBracket
	tokenRightBracket
	tokenComma
	tokenEqual
	tokenNotEqual
	tokenRegex
	tokenSlash
)

type token struct {
	kind  tokenKind
	value string
}

// lex splits the input into the closed token set. Unknown bytes and
// comments are lexical errors.
func lex(text string) ([]token, error) {
	var tokens []token
	for position := 0; position < len(text); {
		character := text[position]
		switch {
		case character == ' ' || character == '\t' || character == '\n' || character == '\r':
			position++
		case character == '{':
			tokens = append(tokens, token{kind: tokenLeftBrace})
			position++
		case character == '}':
			tokens = append(tokens, token{kind: tokenRightBrace})
			position++
		case character == '(':
			tokens = append(tokens, token{kind: tokenLeftParen})
			position++
		case character == ')':
			tokens = append(tokens, token{kind: tokenRightParen})
			position++
		case character == '[':
			tokens = append(tokens, token{kind: tokenLeftBracket})
			position++
		case character == ']':
			tokens = append(tokens, token{kind: tokenRightBracket})
			position++
		case character == ',':
			tokens = append(tokens, token{kind: tokenComma})
			position++
		case character == '/':
			tokens = append(tokens, token{kind: tokenSlash})
			position++
		case character == '"':
			value, end, err := lexString(text, position)
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, token{kind: tokenString, value: value})
			position = end
		case character == '=':
			if position+1 < len(text) && text[position+1] == '~' {
				tokens = append(tokens, token{kind: tokenRegex})
				position += 2
			} else {
				tokens = append(tokens, token{kind: tokenEqual})
				position++
			}
		case character == '!':
			if position+1 < len(text) && text[position+1] == '=' {
				tokens = append(tokens, token{kind: tokenNotEqual})
				position += 2
			} else {
				tokens = append(tokens, token{kind: tokenIdent, value: "!"})
			}
		case isIdentStart(character):
			start := position
			for position < len(text) && isIdentPart(text[position]) {
				position++
			}
			tokens = append(tokens, token{kind: tokenIdent, value: text[start:position]})
		case character >= '0' && character <= '9' || character == '.':
			start := position
			for position < len(text) && (text[position] >= '0' && text[position] <= '9' || text[position] == '.') {
				position++
			}
			tokens = append(tokens, token{kind: tokenNumber, value: text[start:position]})
		default:
			// Any other byte is outside the supported subset.
			return nil, fmt.Errorf("query: parse: unsupported character %q", string(character))
		}
	}
	return tokens, nil
}

// lexString parses a quoted string from position of the opening quote.
// Escape sequences are rejected: the renderer only emits strconv.Quote of
// charset-validated values, so a backslash inside a string is never
// produced by the planner and never accepted from input.
func lexString(text string, position int) (string, int, error) {
	position++ // opening quote
	var builder strings.Builder
	for position < len(text) {
		character := text[position]
		if character == '\\' {
			return "", position, fmt.Errorf("query: parse: escape sequences are not supported")
		}
		if character == '"' {
			return builder.String(), position + 1, nil
		}
		if character == '\n' || character == '\r' {
			return "", position, fmt.Errorf("query: parse: unterminated string")
		}
		builder.WriteByte(character)
		position++
	}
	return "", position, fmt.Errorf("query: parse: unterminated string")
}

func isIdentStart(character byte) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character == '_' || character == '$'
}

func isIdentPart(character byte) bool {
	return isIdentStart(character) || character >= '0' && character <= '9' || character == '-'
}

type exprParser struct {
	tokens []token
	index  int
}

func (parser *exprParser) done() bool { return parser.index >= len(parser.tokens) }

func (parser *exprParser) peek() token {
	if parser.done() {
		return token{kind: tokenEOF}
	}
	return parser.tokens[parser.index]
}

func (parser *exprParser) advance() token {
	current := parser.peek()
	if current.kind != tokenEOF {
		parser.index++
	}
	return current
}

func (parser *exprParser) errorf(format string, args ...any) error {
	return fmt.Errorf("query: parse: %s", fmt.Sprintf(format, args...))
}

// parseExpression parses a binary expression (division only).
func (parser *exprParser) parseExpression() (Expression, error) {
	left, err := parser.parseTerm()
	if err != nil {
		return nil, err
	}
	for parser.peek().kind == tokenSlash {
		parser.advance()
		right, err := parser.parseTerm()
		if err != nil {
			return nil, err
		}
		left = &BinaryExpression{Op: BinaryDivide, Left: left, Right: right}
	}
	return left, nil
}

func (parser *exprParser) parseTerm() (Expression, error) {
	current := parser.peek()
	switch current.kind {
	case tokenLeftParen:
		parser.advance()
		inner, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := parser.expect(tokenRightParen, ")"); err != nil {
			return nil, err
		}
		return inner, nil
	case tokenIdent:
		switch current.value {
		case "sum":
			return parser.parseAggregation()
		case "rate":
			return parser.parseRate()
		case "histogram_quantile":
			return parser.parseQuantile()
		default:
			if parser.peekAt(1).kind == tokenLeftParen {
				return nil, parser.errorf("unsupported function %q", current.value)
			}
			return parser.parseSelector()
		}
	case tokenNumber:
		parser.advance()
		value, err := strconv.ParseFloat(current.value, 64)
		if err != nil {
			return nil, parser.errorf("invalid number %q", current.value)
		}
		return &ScalarExpression{Value: value}, nil
	case tokenLeftBrace:
		return nil, parser.errorf("bare selector without a metric name")
	default:
		return nil, parser.errorf("unexpected token")
	}
}

func (parser *exprParser) peekAt(offset int) token {
	position := parser.index + offset
	if position >= len(parser.tokens) {
		return token{kind: tokenEOF}
	}
	return parser.tokens[position]
}

func (parser *exprParser) expect(kind tokenKind, what string) (token, error) {
	current := parser.peek()
	if current.kind != kind {
		return token{}, parser.errorf("expected %s", what)
	}
	parser.advance()
	return current, nil
}

func (parser *exprParser) parseSelector() (Expression, error) {
	name := parser.advance() // tokenIdent
	if name.kind != tokenIdent {
		return nil, parser.errorf("expected a metric name")
	}
	selector := &MetricSelector{MetricName: name.value}
	if parser.peek().kind != tokenLeftBrace {
		return selector, nil
	}
	parser.advance()
	for {
		label, err := parser.expect(tokenIdent, "a label name")
		if err != nil {
			return nil, err
		}
		operator := parser.advance()
		var op MatcherOp
		switch operator.kind {
		case tokenEqual:
			op = MatchEqual
		case tokenNotEqual:
			op = MatchNotEqual
		case tokenRegex:
			op = MatchRegex
		default:
			return nil, parser.errorf("expected a matcher operator")
		}
		value, err := parser.expect(tokenString, "a quoted matcher value")
		if err != nil {
			return nil, err
		}
		selector.Matchers = append(selector.Matchers, LabelMatcher{Label: label.value, Op: op, Value: value.value})
		if parser.peek().kind != tokenComma {
			break
		}
		parser.advance()
	}
	if _, err := parser.expect(tokenRightBrace, "closing brace"); err != nil {
		return nil, err
	}
	return selector, nil
}

func (parser *exprParser) parseAggregation() (Expression, error) {
	parser.advance() // "sum"
	aggregation := &Aggregation{}
	if parser.peek().kind == tokenIdent && parser.peek().value == "by" {
		parser.advance()
		if _, err := parser.expect(tokenLeftParen, "("); err != nil {
			return nil, err
		}
		for {
			label, err := parser.expect(tokenIdent, "a group label")
			if err != nil {
				return nil, err
			}
			aggregation.By = append(aggregation.By, label.value)
			if parser.peek().kind != tokenComma {
				break
			}
			parser.advance()
		}
		if _, err := parser.expect(tokenRightParen, ")"); err != nil {
			return nil, err
		}
	}
	if _, err := parser.expect(tokenLeftParen, "("); err != nil {
		return nil, err
	}
	inner, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := parser.expect(tokenRightParen, ")"); err != nil {
		return nil, err
	}
	aggregation.Expr = inner
	return aggregation, nil
}

func (parser *exprParser) parseRate() (Expression, error) {
	parser.advance() // "rate"
	if _, err := parser.expect(tokenLeftParen, "("); err != nil {
		return nil, err
	}
	selector, err := parser.parseSelector()
	if err != nil {
		return nil, err
	}
	rateExpression, ok := selector.(*MetricSelector)
	if !ok {
		return nil, parser.errorf("rate() requires a selector")
	}
	if _, err := parser.expect(tokenLeftBracket, "["); err != nil {
		return nil, err
	}
	interval, err := parser.expect(tokenIdent, "a rate interval")
	if err != nil {
		return nil, err
	}
	if _, err := parser.expect(tokenRightBracket, "]"); err != nil {
		return nil, err
	}
	if _, err := parser.expect(tokenRightParen, ")"); err != nil {
		return nil, err
	}
	return &RateExpression{Selector: rateExpression, Interval: interval.value}, nil
}

func (parser *exprParser) parseQuantile() (Expression, error) {
	parser.advance() // "histogram_quantile"
	if _, err := parser.expect(tokenLeftParen, "("); err != nil {
		return nil, err
	}
	number, err := parser.expect(tokenNumber, "a quantile")
	if err != nil {
		return nil, err
	}
	quantile, err := strconv.ParseFloat(number.value, 64)
	if err != nil {
		return nil, parser.errorf("invalid quantile %q", number.value)
	}
	if _, err := parser.expect(tokenComma, ","); err != nil {
		return nil, err
	}
	inner, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := parser.expect(tokenRightParen, ")"); err != nil {
		return nil, err
	}
	return &HistogramQuantileExpression{Quantile: quantile, Expr: inner}, nil
}

// Equal reports semantic equality of two typed expressions: same node
// type, same fields, same matcher order.
func Equal(first, second Expression) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	switch left := first.(type) {
	case *MetricSelector:
		right, ok := second.(*MetricSelector)
		if !ok || left.MetricName != right.MetricName || len(left.Matchers) != len(right.Matchers) {
			return false
		}
		for index := range left.Matchers {
			if left.Matchers[index] != right.Matchers[index] {
				return false
			}
		}
		return true
	case *RateExpression:
		right, ok := second.(*RateExpression)
		return ok && left.Interval == right.Interval && Equal(left.Selector, right.Selector)
	case *Aggregation:
		right, ok := second.(*Aggregation)
		if !ok || len(left.By) != len(right.By) {
			return false
		}
		for index := range left.By {
			if left.By[index] != right.By[index] {
				return false
			}
		}
		return Equal(left.Expr, right.Expr)
	case *HistogramQuantileExpression:
		right, ok := second.(*HistogramQuantileExpression)
		return ok && left.Quantile == right.Quantile && Equal(left.Expr, right.Expr)
	case *BinaryExpression:
		right, ok := second.(*BinaryExpression)
		return ok && left.Op == right.Op && Equal(left.Left, right.Left) && Equal(left.Right, right.Right)
	case *ScalarExpression:
		right, ok := second.(*ScalarExpression)
		return ok && left.Value == right.Value
	default:
		return false
	}
}
