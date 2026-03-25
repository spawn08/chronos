package builtins

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestCalculatorTool_ExpressionsMax(t *testing.T) {
	tool := NewCalculatorTool()
	ctx := context.Background()

	tests := []struct {
		expr       string
		want       float64
		wantErr    bool
		errContain string
	}{
		{"2+3*4", 14, false, ""},
		{"(2+3)*4", 20, false, ""},
		{"2^3", 8, false, ""},
		{"-5", -5, false, ""},
		{"sqrt(16)", 4, false, ""},
		{"sin(0)", 0, false, ""},
		{"cos(0)", 1, false, ""},
		{"log(2.718281828459045)", 1, false, ""},
		{"log(0)", 0, true, "log of non-positive"},
		{"log(-1)", 0, true, "log of non-positive"},
		{"abs(-7)", 7, false, ""},
		{"ceil(2.1)", 3, false, ""},
		{"floor(2.9)", 2, false, ""},
		{"pi", math.Pi, false, ""},
		{"e", math.E, false, ""},
		{"10/2", 5, false, ""},
		{"10/0", 0, true, "division by zero"},
		{"1 + 2 + 3", 6, false, ""},
		{"2 * 3 * 4", 24, false, ""},
		{"(1", 0, true, "missing closing parenthesis"},
		{"sqrt(4", 0, true, "missing closing parenthesis"},
		{"", 0, true, "non-empty string"},
		{"   ", 0, true, "unexpected end of expression"},
		{"1+", 0, true, ""},
		{"1 2", 0, true, "unexpected character"},
		{"@", 0, true, "expected number"},
	}

	for _, tt := range tests {
		t.Run(strings.ReplaceAll(tt.expr, " ", "_"), func(t *testing.T) {
			_, err := tool.Handler(ctx, map[string]any{"expression": tt.expr})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("err %q should contain %q", err.Error(), tt.errContain)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCalculatorTool_NonStringExpression(t *testing.T) {
	tool := NewCalculatorTool()
	_, err := tool.Handler(context.Background(), map[string]any{"expression": 42})
	if err == nil {
		t.Fatal("expected error for non-string expression")
	}
}
