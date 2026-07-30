package storage

import "testing"

func TestPrefixRange(t *testing.T) {
	tests := []struct {
		prefix       string
		wantLo       string
		wantHi       string
		wantHasUpper bool
	}{
		{"", "", "", false},                  // empty: no upper bound, matches all
		{"notes/", "notes/", "notes0", true}, // '/' (0x2F) -> '0' (0x30)
		{"a", "a", "b", true},                // 'a' -> 'b'
		{"az", "az", "a{", true},             // last byte 'z' (0x7A) -> '{' (0x7B)
		{"a%", "a%", "a&", true},             // '%' (0x25) -> '&' (0x26); metachars are literal bytes
		{"a_", "a_", "a`", true},             // '_' (0x5F) -> '`' (0x60)
		{"\xff", "\xff", "", false},          // all 0xFF: no upper bound
		{"a\xff", "a\xff", "b", true},        // trailing 0xFF rolls to the previous byte: 'a'->'b'
	}
	for _, tt := range tests {
		lo, hi, hasUpper := PrefixRange(tt.prefix)
		if lo != tt.wantLo || hasUpper != tt.wantHasUpper || (hasUpper && hi != tt.wantHi) {
			t.Errorf("PrefixRange(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.prefix, lo, hi, hasUpper, tt.wantLo, tt.wantHi, tt.wantHasUpper)
		}
	}
}
