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
