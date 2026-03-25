package builtins

import (
	"context"
	"math"
	"testing"
)

func TestCalculator_LogNonPositive(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "log(0)"})
	if err == nil {
		t.Fatal("expected error for log(0)")
	}
}

func TestCalculator_CeilFloorCos(t *testing.T) {
	calc := NewCalculatorTool()
	tests := []struct {
		expr string
		want float64
	}{
		{"ceil(2.1)", 3},
		{"floor(2.9)", 2},
		{"cos(0)", 1},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			res, err := calc.Handler(context.Background(), map[string]any{"expression": tt.expr})
			if err != nil {
				t.Fatal(err)
			}
			got := res.(map[string]any)["result"].(float64)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculator_MissingParenAfterFunction(t *testing.T) {
	calc := NewCalculatorTool()
	_, err := calc.Handler(context.Background(), map[string]any{"expression": "sqrt 9"})
	if err == nil {
		t.Fatal("expected error for malformed function call")
	}
}
