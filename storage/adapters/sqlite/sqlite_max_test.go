package sqlite

import (
	"context"
	"testing"
)

func TestStore_Migrate_ContextCancelled_Max(t *testing.T) {
	st, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := st.Migrate(ctx); err == nil {
		t.Fatal("expected migrate error when context already cancelled")
	}
}
