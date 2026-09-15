package model

import "testing"

func TestKnownContextLimit(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"gpt-4", 8192},
		{"gpt-5-mini", 400000},
		{"gpt-5-nano", 400000},
		{"llama-3.3-70b-versatile", 128000},
		{"meta-llama/llama-3.1-405b", 128000},
		{"llama3.3", 131072},
		{"custom-deployment", 0},
		{"gpt-5-mini-custom", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, known := KnownContextLimit(tt.name)
			if got != tt.want || known != (tt.want > 0) {
				t.Fatalf("KnownContextLimit(%q) = (%d,%v), want (%d,%v)", tt.name, got, known, tt.want, tt.want > 0)
			}
			for _, fallback := range []int{-1, 0, 1024, 512000} {
				want := tt.want
				if want == 0 {
					want = fallback
					if want <= 0 {
						want = 8192
					}
				}
				if got := ContextLimit(tt.name, fallback); got != want {
					t.Errorf("ContextLimit(%q, %d) = %d, want %d", tt.name, fallback, got, want)
				}
			}
		})
	}
}
