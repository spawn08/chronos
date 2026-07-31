package a2a

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// TestMemStoreTenantIsolation proves the in-memory default is safe-by-default
// behind the tenant-scoped served endpoint: a task created under one tenant is
// invisible (ErrTaskNotFound) to another.
func TestMemStoreTenantIsolation(t *testing.T) {
	m := newMemStore(echoHandler)

	ctxA := storage.WithTenant(context.Background(), "tenant-a")
	ctxB := storage.WithTenant(context.Background(), "tenant-b")

	created, err := m.Submit(ctxA, "secret", nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	if _, err := m.Get(ctxA, created.ID); err != nil {
		t.Fatalf("owner tenant should see its task: %v", err)
	}
	if _, err := m.Get(ctxB, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("cross-tenant Get: want ErrTaskNotFound, got %v", err)
	}
	if _, err := m.Cancel(ctxB, created.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("cross-tenant Cancel: want ErrTaskNotFound, got %v", err)
	}
}
