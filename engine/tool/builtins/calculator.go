package builtins

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/spawn08/chronos/engine/tool"
)

// NewCalculatorTool creates a tool that evaluates mathematical expressions.
func NewCalculatorTool() *tool.Definition {
	return &tool.Definition{
		Name:        "calculator",
		Description: "Evaluate mathematical expressions. Supports +, -, *, /, ^, (), sqrt, sin, cos, log.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expression": map[string]any{
					"type":        "string",
					"description": "Mathematical expression to evaluate (e.g., '2 + 3 * 4', 'sqrt(16)', 'sin(3.14)')",
				},
			},
			"required": []any{"expression"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			expr, ok := args["expression"].(string)
			if !ok || expr == "" {
				return nil, fmt.Errorf("calculator: expression must be a non-empty string")
			}
			result, err := evaluate(expr)
			if err != nil {
				return nil, fmt.Errorf("calculator: %w", err)
			}
			return map[string]any{"result": result, "expression": expr}, nil
		},
	}
}

func evaluate(expr string) (float64, error) {
	expr = strings.TrimSpace(expr)
	p := &parser{input: expr}
	result, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	if p.pos < len(p.input) {
		return 0, fmt.Errorf("unexpected character at position %d", p.pos)
	}
	return result, nil
}

type parser struct {
	input string
	pos   int
}

func (p *parser) parseExpr() (float64, error) {
	return p.parseAddSub()
}

func (p *parser) parseAddSub() (float64, error) {
	left, err := p.parseMulDiv()
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.input) {
		p.skipSpaces()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right, err := p.parseMulDiv()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, nil
}

func (p *parser) parseMulDiv() (float64, error) {
	left, err := p.parsePower()
	if err != nil {
		return 0, err
	}
	for p.pos < len(p.input) {
		p.skipSpaces()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '*' && op != '/' {
			break
		}
		p.pos++
		right, err := p.parsePower()
		if err != nil {
			return 0, err
		}
		if op == '*' {
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

func (p *parser) parsePower() (float64, error) {
	base, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.pos < len(p.input) && p.input[p.pos] == '^' {
		p.pos++
		exp, err := p.parseUnary()
		if err != nil {
			return 0, err
		}
		return math.Pow(base, exp), nil
	}
	return base, nil
}

func (p *parser) parseUnary() (float64, error) {
	p.skipSpaces()
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
		val, err := p.parsePrimary()
		if err != nil {
			return 0, err
		}
		return -val, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (float64, error) {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		return 0, fmt.Errorf("unexpected end of expression")
	}

	// Functions
	for _, fn := range []string{"sqrt", "sin", "cos", "log", "abs", "ceil", "floor"} {
		if strings.HasPrefix(p.input[p.pos:], fn+"(") {
			p.pos += len(fn)
			return p.parseFunction(fn)
		}
	}

	// Parenthesized expression
	if p.input[p.pos] == '(' {
		p.pos++
		val, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return val, nil
	}

	// Constants
	if strings.HasPrefix(p.input[p.pos:], "pi") {
		p.pos += 2
		return math.Pi, nil
	}
	if p.pos+1 < len(p.input) && p.input[p.pos] == 'e' && !unicode.IsLetter(rune(p.input[p.pos+1])) {
		p.pos++
		return math.E, nil
	}
	if p.pos == len(p.input)-1 && p.input[p.pos] == 'e' {
		p.pos++
		return math.E, nil
	}

	return p.parseNumber()
}

func (p *parser) parseFunction(name string) (float64, error) {
	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return 0, fmt.Errorf("expected '(' after %s", name)
	}
	p.pos++
	arg, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpaces()
	if p.pos >= len(p.input) || p.input[p.pos] != ')' {
		return 0, fmt.Errorf("missing closing parenthesis for %s", name)
	}
	p.pos++

	switch name {
	case "sqrt":
		return math.Sqrt(arg), nil
	case "sin":
		return math.Sin(arg), nil
	case "cos":
		return math.Cos(arg), nil
	case "log":
		if arg <= 0 {
			return 0, fmt.Errorf("log of non-positive number")
		}
		return math.Log(arg), nil
	case "abs":
		return math.Abs(arg), nil
	case "ceil":
		return math.Ceil(arg), nil
	case "floor":
		return math.Floor(arg), nil
	default:
		return 0, fmt.Errorf("unknown function: %s", name)
	}
}

func (p *parser) parseNumber() (float64, error) {
	p.skipSpaces()
	start := p.pos
	for p.pos < len(p.input) && (p.input[p.pos] >= '0' && p.input[p.pos] <= '9' || p.input[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected number at position %d", p.pos)
	}
	return strconv.ParseFloat(p.input[start:p.pos], 64)
}

func (p *parser) skipSpaces() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}
