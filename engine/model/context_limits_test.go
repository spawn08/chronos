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

func TestContextLimitResolverTakesPrecedenceAndFallsBack(t *testing.T) {
	t.Cleanup(func() { SetContextLimitResolver(nil) })
	SetContextLimitResolver(func(name string) (int, bool) {
		switch name {
		case "gpt-4":
			return 16384, true // catalog overrides the built-in table
		case "catalog-only-model":
			return 262144, true
		case "zero-limit":
			return 0, true // non-positive values are ignored
		}
		return 0, false
	})
	for name, want := range map[string]int{"gpt-4": 16384, "catalog-only-model": 262144, "gpt-5-mini": 400000} {
		if got, ok := KnownContextLimit(name); !ok || got != want {
			t.Errorf("KnownContextLimit(%q) = (%d,%v), want (%d,true)", name, got, ok, want)
		}
	}
	if got, ok := KnownContextLimit("zero-limit"); ok || got != 0 {
		t.Errorf("KnownContextLimit(zero-limit) = (%d,%v), want unknown", got, ok)
	}
	if got := ContextLimit("catalog-only-model", 1024); got != 262144 {
		t.Errorf("ContextLimit(catalog-only-model) = %d, want 262144", got)
	}
	SetContextLimitResolver(nil)
	if got, _ := KnownContextLimit("gpt-4"); got != 8192 {
		t.Errorf("KnownContextLimit(gpt-4) after removing resolver = %d, want 8192", got)
	}
}
