package pgvector

import (
	"testing"
)

func TestNew(t *testing.T) {
	// New accepts a *sql.DB; passing nil is structurally valid but Close() would panic.
	// We only test that New returns a non-nil Store.
	s := New(nil)
	if s == nil {
		t.Fatal("New(nil) returned nil")
	}
}

func TestVectorToString(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
		want string
	}{
		{"empty", []float32{}, "[]"},
		{"single", []float32{1.5}, "[1.5]"},
		{"multiple", []float32{0.1, 0.2, 0.3}, "[0.1,0.2,0.3]"},
		{"integer values", []float32{1, 2, 3}, "[1,2,3]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vectorToString(tt.vec)
			if got != tt.want {
				t.Errorf("vectorToString(%v) = %q, want %q", tt.vec, got, tt.want)
			}
		})
	}
}

func TestSanitizeTableName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"alphanumeric", "my_collection", "my_collection"},
		{"uppercase", "MyCollection", "MyCollection"},
		{"with spaces", "my collection", "mycollection"},
		{"with dashes", "my-collection", "mycollection"},
		{"with dots", "my.collection", "mycollection"},
		{"empty", "", "default_collection"},
		{"all invalid", "---", "default_collection"},
		{"mixed", "col-1_test", "col1_test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeTableName(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeTableName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
