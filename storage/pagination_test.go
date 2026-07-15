package storage

import (
	"testing"
)

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults", 0, DefaultPageLimit},
		{"negative defaults", -5, DefaultPageLimit},
		{"within range", 50, 50},
		{"at max", MaxPageLimit, MaxPageLimit},
		{"over max clamps", MaxPageLimit + 1, MaxPageLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClampLimit(tt.in); got != tt.want {
				t.Errorf("ClampLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestSeqCursorRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		seq  int64
	}{
		{"zero", 0},
		{"small", 42},
		{"large", 9_000_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := EncodeSeqCursor(tt.seq)
			got, err := DecodeSeqCursor(c)
			if err != nil {
				t.Fatalf("DecodeSeqCursor: %v", err)
			}
			if got != tt.seq {
				t.Errorf("round trip = %d, want %d", got, tt.seq)
			}
		})
	}
}

func TestDecodeSeqCursor(t *testing.T) {
	t.Run("empty is zero", func(t *testing.T) {
		got, err := DecodeSeqCursor("")
		if err != nil || got != 0 {
			t.Fatalf("DecodeSeqCursor(\"\") = %d, %v", got, err)
		}
	})
	t.Run("garbage errors", func(t *testing.T) {
		if _, err := DecodeSeqCursor("!!!not-base64!!!"); err == nil {
			t.Fatal("expected error for invalid cursor")
		}
	})
}

func TestStrCursorRoundTrip(t *testing.T) {
	c := EncodeStrCursor("trace-9|weird chars")
	got, err := DecodeStrCursor(c)
	if err != nil {
		t.Fatalf("DecodeStrCursor: %v", err)
	}
	if got != "trace-9|weird chars" {
		t.Errorf("round trip = %q, want %q", got, "trace-9|weird chars")
	}

	if got, err := DecodeStrCursor(""); err != nil || got != "" {
		t.Errorf("empty cursor = %q, %v", got, err)
	}
	if _, err := DecodeStrCursor("###"); err == nil {
		t.Error("expected error for invalid cursor")
	}
}
