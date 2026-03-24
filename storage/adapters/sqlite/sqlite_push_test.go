package sqlite

import (
	"context"
	"testing"
)

func TestMigrate_AfterClose_Push(t *testing.T) {
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := s.Migrate(context.Background()); err == nil {
		t.Fatal("expected Migrate error after Close")
	}
}

