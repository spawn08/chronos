package builtins

import (
	"context"
	"math"
	"testing"
)

func evalCalcDeep(t *testing.T, expr string) float64 {
	t.Helper()
	def := NewCalculatorTool()
	out, err := def.Handler(context.Background(), map[string]any{"expression": expr})
	if err != nil {
		t.Fatalf("%s: %v", expr, err)
	}
	m := out.(map[string]any)
	return m["result"].(float64)
}

func TestCalculator_UnaryMinus_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "-3+5"); math.Abs(v-2) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_NegativeInParens_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "(-3)^2"); math.Abs(v-9) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_Power_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "2^10"); math.Abs(v-1024) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_Parentheses_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "(1+2)*(3+4)"); math.Abs(v-21) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_FunctionSqrt_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "sqrt(16)"); math.Abs(v-4) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_FunctionSin_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "sin(0)"); math.Abs(v) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_MixedPrecedence_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "2+3*4"); math.Abs(v-14) > 1e-9 {
		t.Fatalf("got %v", v)
	}
}

func TestCalculator_Float_Deep(t *testing.T) {
	if v := evalCalcDeep(t, "0.1+0.2"); math.Abs(v-0.3) > 0.0001 {
		t.Fatalf("got %v", v)
	}
}
