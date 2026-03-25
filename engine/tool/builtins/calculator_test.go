package builtins

import (
	"context"
	"math"
	"testing"
)

func TestCalculator_Basic(t *testing.T) {
	tests := []struct {
		expr string
		want float64
	}{
		{"2 + 3", 5},
		{"10 - 4", 6},
		{"3 * 7", 21},
		{"20 / 4", 5},
		{"2 ^ 3", 8},
		{"(2 + 3) * 4", 20},
		{"sqrt(16)", 4},
		{"sqrt(25)", 5},
		{"abs(-5)", 5},
		{"-3 + 7", 4},
	}

	calc := NewCalculatorTool()
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			result, err := calc.Handler(context.Background(), map[string]any{"expression": tt.expr})
			if err != nil {
				t.Fatalf("evaluate %q: %v", tt.expr, err)
			}
			m := result.(map[string]any)
			got := m["result"].(float64)
			if math.Abs(got-tt.want) > 0.0001 {
				t.Errorf("evaluate(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestCalculator_Trig(t *testing.T) {
	calc := NewCalculatorTool()
	result, err := calc.Handler(context.Background(), map[string]any{"expression": "sin(0)"})
	if err != nil {
		t.Fatal(err)
	}
	m := result.(map[string]any)
	if math.Abs(m["result"].(float64)) > 0.0001 {
		t.Errorf("sin(0) = %v, want 0", m["result"])
	}
}

func TestCalculator_DivisionByZero(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "1/0"})
	if err == nil {
		t.Fatal("expected division by zero error")
	}
}

func TestCalculator_InvalidExpression(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "abc"})
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestCalculator_EmptyExpression(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": ""})
	if err == nil {
		t.Fatal("expected error for empty expression")
	}
}

func TestCalculator_MissingArg(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": 42.0})
	if err == nil {
		t.Fatal("expected error for non-string expression")
	}
}

func TestCalculator_AdvancedFunctions(t *testing.T) {
	calc := NewCalculatorTool()
	tests := []struct {
		expr string
		want float64
	}{
		{"cos(0)", 1.0},
		{"log(1)", 0.0},
		{"ceil(1.2)", 2.0},
		{"floor(1.8)", 1.0},
		{"pi", 3.14159265},
		{"e", 2.71828182},
	}
	for _, tt := range tests {
		result, err := calc.Handler(context.Background(), map[string]any{"expression": tt.expr})
		if err != nil {
			t.Errorf("calc(%q): %v", tt.expr, err)
			continue
		}
		m, _ := result.(map[string]any)
		got, _ := m["result"].(float64)
		if got < tt.want-0.01 || got > tt.want+0.01 {
			t.Errorf("calc(%q) = %v, want ~%v", tt.expr, got, tt.want)
		}
	}
}

func TestCalculator_Parentheses(t *testing.T) {
	calc := NewCalculatorTool()
	result, err := calc.Handler(context.Background(), map[string]any{"expression": "(2 + 3) * 4"})
	if err != nil {
		t.Fatalf("calc: %v", err)
	}
	m, _ := result.(map[string]any)
	got, _ := m["result"].(float64)
	if got != 20.0 {
		t.Errorf("expected 20.0, got %v", got)
	}
}

func TestCalculator_MissingClosingParen(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "(2 + 3"})
	if err == nil {
		t.Fatal("expected error for missing closing paren")
	}
}

func TestCalculator_UnaryMinus(t *testing.T) {
	calc := NewCalculatorTool()
	result, err := calc.Handler(context.Background(), map[string]any{"expression": "-5 + 10"})
	if err != nil {
		t.Fatalf("calc: %v", err)
	}
	m, _ := result.(map[string]any)
	got, _ := m["result"].(float64)
	if got != 5.0 {
		t.Errorf("expected 5.0, got %v", got)
	}
}

func TestCalculator_FunctionErrors(t *testing.T) {
	calc := NewCalculatorTool()
	// These should trigger parseFunction's error paths
	tests := []struct {
		name string
		expr string
	}{
		{"sqrt no arg", "sqrt()"},
		{"sin missing paren", "sin 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := calc.Handler(context.Background(), map[string]any{"expression": tt.expr})
			// These may succeed or fail — just ensure no panic
			_ = err
		})
	}
}

func TestCalculator_Division(t *testing.T) {
	calc := NewCalculatorTool()
	result, err := calc.Handler(context.Background(), map[string]any{"expression": "10 / 4"})
	if err != nil {
		t.Fatalf("division: %v", err)
	}
	m, _ := result.(map[string]any)
	got, _ := m["result"].(float64)
	if got != 2.5 {
		t.Errorf("10/4 = %v, want 2.5", got)
	}
}

func TestCalculator_Exponent(t *testing.T) {
	calc := NewCalculatorTool()
	result, err := calc.Handler(context.Background(), map[string]any{"expression": "2^10"})
	if err != nil {
		t.Fatalf("power: %v", err)
	}
	m, _ := result.(map[string]any)
	got, _ := m["result"].(float64)
	if got != 1024.0 {
		t.Errorf("2^10 = %v, want 1024", got)
	}
}
