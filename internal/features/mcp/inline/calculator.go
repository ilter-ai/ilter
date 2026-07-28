package inline

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
)

func init() {
	if err := RegisterTools("calculator", calculatorHandler, calculatorTools); err != nil {
		slog.Error("failed to register inline tool", "tool", "calculator", "error", err)
	}
}

var calculatorTools = []ToolDef{
	{
		Name:        "calculator",
		Description: "Evaluate a mathematical expression. Supports +, -, *, /, ** (pow), sqrt, sin, cos, tan, log, ln, abs, round, ceil, floor. Use standard math notation, e.g. \"sqrt(25) + 3 * 2\".",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "The mathematical expression to evaluate",
				},
			},
			"required": []any{"expression"},
		},
	},
}

func calculatorHandler(_ context.Context, args map[string]any) (any, error) {
	expr, ok := args["expression"].(string)
	if !ok || expr == "" {
		return nil, fmt.Errorf("missing required parameter: expression")
	}

	result, err := evalMath(expr)
	if err != nil {
		return nil, fmt.Errorf("evaluation error: %v", err)
	}

	return map[string]any{
		"expression": expr,
		"result":     result,
	}, nil
}

func evalMath(expr string) (float64, error) {
	tokens := tokenise(expr)
	if len(tokens) == 0 {
		return 0, fmt.Errorf("empty expression")
	}
	p := &mathParser{tokens: tokens}
	return p.parseExpr()
}

type tokenType int

const (
	tokNumber tokenType = iota
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokPow
	tokLParen
	tokRParen
	tokFunc
	tokEOF
)

type token struct {
	typ tokenType
	val string
	num float64
}

func tokenise(s string) []token {
	var toks []token
	i := 0
	for i < len(s) {
		ch := s[i]
		if ch == ' ' || ch == '\t' {
			i++
			continue
		}
		if ch >= '0' && ch <= '9' || ch == '.' {
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			n, err := strconv.ParseFloat(s[i:j], 64)
			if err != nil {
				i = j
				continue
			}
			toks = append(toks, token{typ: tokNumber, num: n})
			i = j
			continue
		}
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
			j := i
			for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= 'A' && s[j] <= 'Z') {
				j++
			}
			name := s[i:j]
			switch name {
			case "sqrt", "sin", "cos", "tan", "log", "ln", "abs", "round", "ceil", "floor":
				toks = append(toks, token{typ: tokFunc, val: name})
			default:
				return nil
			}
			i = j
			continue
		}
		switch ch {
		case '+':
			toks = append(toks, token{typ: tokPlus})
		case '-':
			toks = append(toks, token{typ: tokMinus})
		case '*':
			if i+1 < len(s) && s[i+1] == '*' {
				toks = append(toks, token{typ: tokPow})
				i += 2
				continue
			}
			toks = append(toks, token{typ: tokStar})
		case '/':
			toks = append(toks, token{typ: tokSlash})
		case '(':
			toks = append(toks, token{typ: tokLParen})
		case ')':
			toks = append(toks, token{typ: tokRParen})
		}
		i++
	}
	toks = append(toks, token{typ: tokEOF})
	return toks
}

type mathParser struct {
	tokens []token
	pos    int
}

func (p *mathParser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{typ: tokEOF}
	}
	return p.tokens[p.pos]
}

func (p *mathParser) advance() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *mathParser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.peek().typ == tokPlus || p.peek().typ == tokMinus {
		op := p.advance()
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op.typ == tokPlus {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

func (p *mathParser) parseTerm() (float64, error) {
	left, err := p.parsePower()
	if err != nil {
		return 0, err
	}
	for p.peek().typ == tokStar || p.peek().typ == tokSlash {
		op := p.advance()
		right, err := p.parsePower()
		if err != nil {
			return 0, err
		}
		if op.typ == tokStar {
			left *= right
		} else {
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		}
	}
	return left, nil
}

func (p *mathParser) parsePower() (float64, error) {
	left, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	if p.peek().typ == tokPow {
		p.advance()
		right, err := p.parsePower()
		if err != nil {
			return 0, err
		}
		left = math.Pow(left, right)
	}
	return left, nil
}

func (p *mathParser) parseUnary() (float64, error) {
	if p.peek().typ == tokMinus {
		p.advance()
		v, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return -v, nil
	}
	return p.parseAtom()
}

func (p *mathParser) parseAtom() (float64, error) {
	t := p.peek()
	switch t.typ {
	case tokNumber:
		p.advance()
		return t.num, nil
	case tokLParen:
		p.advance()
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek().typ != tokRParen {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.advance()
		return v, nil
	case tokFunc:
		fn := p.advance()
		if p.peek().typ != tokLParen {
			return 0, fmt.Errorf("expected ( after %s", fn.val)
		}
		p.advance()
		arg, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if p.peek().typ != tokRParen {
			return 0, fmt.Errorf("missing closing parenthesis in %s", fn.val)
		}
		p.advance()
		return applyFunc(fn.val, arg)
	case tokEOF:
		return 0, fmt.Errorf("unexpected end of expression")
	default:
		return 0, fmt.Errorf("unexpected token")
	}
}

func applyFunc(name string, arg float64) (float64, error) {
	switch name {
	case "sqrt":
		if arg < 0 {
			return 0, fmt.Errorf("sqrt of negative number")
		}
		return math.Sqrt(arg), nil
	case "sin":
		return math.Sin(arg), nil
	case "cos":
		return math.Cos(arg), nil
	case "tan":
		return math.Tan(arg), nil
	case "log":
		if arg <= 0 {
			return 0, fmt.Errorf("log of non-positive number")
		}
		return math.Log10(arg), nil
	case "ln":
		if arg <= 0 {
			return 0, fmt.Errorf("ln of non-positive number")
		}
		return math.Log(arg), nil
	case "abs":
		return math.Abs(arg), nil
	case "round":
		return math.Round(arg), nil
	case "ceil":
		return math.Ceil(arg), nil
	case "floor":
		return math.Floor(arg), nil
	default:
		return 0, fmt.Errorf("unknown function: %s", name)
	}
}
