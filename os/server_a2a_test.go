package chronosos

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/sdk/protocol/a2a"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

// tenantA2AStore is a tenant-scoped in-memory a2a.TaskStore used to prove the
// control plane threads the caller's tenant into the A2A handler: a task created
// under one tenant is invisible (ErrTaskNotFound → 404) to another.
type tenantA2AStore struct {
	mu    sync.Mutex
	tasks map[string]*a2a.Task // key: tenant + "/" + id
	n     int
}

func newTenantA2AStore() *tenantA2AStore {
	return &tenantA2AStore{tasks: make(map[string]*a2a.Task)}
}

func (s *tenantA2AStore) key(ctx context.Context, id string) string {
	return storage.TenantFromContext(ctx) + "/" + id
}

func (s *tenantA2AStore) Submit(ctx context.Context, input string, metadata map[string]any) (*a2a.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	now := time.Now()
	task := &a2a.Task{
		ID:        fmt.Sprintf("task_%d", s.n),
		Status:    a2a.TaskStatusCompleted,
		Input:     input,
		Output:    "echo: " + input,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.tasks[s.key(ctx, task.ID)] = task
	snapshot := *task
	return &snapshot, nil
}

func (s *tenantA2AStore) Get(ctx context.Context, id string) (*a2a.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[s.key(ctx, id)]
	if !ok {
		return nil, a2a.ErrTaskNotFound
	}
	snapshot := *task
	return &snapshot, nil
}

func (s *tenantA2AStore) Cancel(ctx context.Context, id string) (*a2a.Task, error) {
	return s.Get(ctx, id)
}

// TestA2AServedTenantIsolation proves the /a2a endpoint is behind the auth chain
// and scoped to the caller's tenant: tenant-b cannot read tenant-a's task (404),
// and an unauthenticated request is rejected (401).
func TestA2AServedTenantIsolation(t *testing.T) {
	a2aSrv := a2a.NewServerWithStore(a2a.AgentCard{Name: "chronos", Version: "1"}, newTenantA2AStore())

	const keyA, keyB = "key-a", "key-b"
	s := NewWithOptions(":0", memory.New(),
		WithAPIKeyAuth(auth.APIKeyConfig{
			HeaderName: "X-Api-Key",
			Keys: map[string]auth.APIKeyEntry{
				keyA: {Scope: "admin", UserID: "ua", TenantID: "tenant-a"},
				keyB: {Scope: "admin", UserID: "ub", TenantID: "tenant-b"},
			},
		}),
		WithA2A(a2aSrv),
	)

	do := func(key, method, target, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, target, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, target, http.NoBody)
		}
		if key != "" {
			r.Header.Set("X-Api-Key", key)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}

	// tenant-a creates a task.
	w := do(keyA, http.MethodPost, "/a2a/tasks", `{"input":"hello"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: status = %d, body=%s", w.Code, w.Body.String())
	}
	var created a2a.Task
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode task: %v", err)
	}

	t.Run("owner tenant reads its task", func(t *testing.T) {
		w := do(keyA, http.MethodGet, "/a2a/tasks/"+created.ID, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("cross-tenant read is 404 (IDOR)", func(t *testing.T) {
		w := do(keyB, http.MethodGet, "/a2a/tasks/"+created.ID, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("cross-tenant get: status = %d, want 404 (body=%s)", w.Code, w.Body.String())
		}
	})

	t.Run("unauthenticated request is rejected", func(t *testing.T) {
		w := do("", http.MethodGet, "/a2a/agent", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated: status = %d, want 401", w.Code)
		}
	})

	t.Run("agent card is reachable when authenticated", func(t *testing.T) {
		w := do(keyA, http.MethodGet, "/a2a/agent", "")
		if w.Code != http.StatusOK {
			t.Fatalf("agent card: status = %d, body=%s", w.Code, w.Body.String())
		}
	})
}

// TestA2ABodyLimitEnforced proves the A2A create endpoint is subject to the
// control plane's request-body cap even though the handler is mounted as an
// opaque http.Handler (the limit is applied in handleA2A).
func TestA2ABodyLimitEnforced(t *testing.T) {
	a2aSrv := a2a.NewServerWithStore(a2a.AgentCard{Name: "chronos", Version: "1"}, newTenantA2AStore())
	const key = "key-a"
	s := NewWithOptions(":0", memory.New(),
		WithAPIKeyAuth(auth.APIKeyConfig{
			HeaderName: "X-Api-Key",
			Keys:       map[string]auth.APIKeyEntry{key: {Scope: "admin", UserID: "ua", TenantID: "tenant-a"}},
		}),
		WithMaxBodyBytes(64), // tiny cap so a modest body trips it
		WithA2A(a2aSrv),
	)

	oversized := `{"input":"` + strings.Repeat("x", 512) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/a2a/tasks", strings.NewReader(oversized))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Api-Key", key)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)

	// MaxBytesReader makes the decode fail; the handler returns 400 (bad json).
	if w.Code == http.StatusCreated {
		t.Fatalf("oversized body was accepted (status %d); body cap not enforced", w.Code)
	}
}

// TestA2ADisabledByDefault confirms the endpoint is absent unless WithA2A is set.
func TestA2ADisabledByDefault(t *testing.T) {
	s := NewWithOptions(":0", memory.New())
	r := httptest.NewRequest(http.MethodGet, "/a2a/agent", http.NoBody)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when A2A is not configured", w.Code)
	}
}
