package builtins

import (
	"context"
	"math"
	"testing"
)

func TestCalculator_CosLogCeilFloor_Squeeze(t *testing.T) {
	t.Parallel()
	calc := NewCalculatorTool()
	tests := []struct {
		expr string
		want float64
	}{
		{"cos(0)", 1},
		{"log(2.718281828)", 1},
		{"ceil(1.1)", 2},
		{"floor(1.9)", 1},
		{"pi + 0", math.Pi},
		{"e * 0 + 1", 1},
		{"sqrt(2) * sqrt(2)", 2},
		{"((1+2)*(3+4))", 21},
	}
	for _, tt := range tests {
		res, err := calc.Handler(context.Background(), map[string]any{"expression": tt.expr})
		if err != nil {
			t.Fatalf("%q: %v", tt.expr, err)
		}
		got := res.(map[string]any)["result"].(float64)
		if math.Abs(got-tt.want) > 1e-5 {
			t.Errorf("%q: got %v want %v", tt.expr, got, tt.want)
		}
	}
}

func TestCalculator_LogNonPositive_Squeeze(t *testing.T) {
	t.Parallel()
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "log(0)"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCalculator_MissingClosingParen_Squeeze(t *testing.T) {
	t.Parallel()
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "(1+2"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCalculator_UnclosedFunction_Squeeze(t *testing.T) {
	t.Parallel()
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "sin(1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCalculator_TrailingJunk_Squeeze(t *testing.T) {
	t.Parallel()
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "1 + 2 xxx"})
	if err == nil {
		t.Fatal("expected error for trailing input")
	}
}
