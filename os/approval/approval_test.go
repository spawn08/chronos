package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	svc := NewService()
	if svc == nil {
		t.Fatal("NewService returned nil")
		return
	}
	if svc.pending == nil {
		t.Fatal("pending map is nil")
	}
}

func TestHandlePendingEmpty(t *testing.T) {
	svc := NewService()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approve/pending", http.NoBody)
	svc.HandlePending(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	list, ok := resp["pending"].([]any)
	if !ok {
		t.Fatalf("pending not a list: %T", resp["pending"])
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d items", len(list))
	}
}

func TestHandleRespondNotFound(t *testing.T) {
	svc := NewService()
	body := `{"id":"nonexistent","approved":true}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/approve/respond", bytes.NewBufferString(body))
	svc.HandleRespond(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleRespondBadJSON(t *testing.T) {
	svc := NewService()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/approve/respond", bytes.NewBufferString("notjson"))
	svc.HandleRespond(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// waitForPending blocks until exactly one request is registered and returns its
// ID, failing the test on timeout.
func waitForPending(t *testing.T, svc *Service) string {
	t.Helper()
	for i := 0; i < 500; i++ {
		svc.mu.Lock()
		for k := range svc.pending {
			svc.mu.Unlock()
			return k
		}
		svc.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no pending request registered")
	return ""
}

func respond(t *testing.T, svc *Service, id string, approved bool) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"id": id, "approved": approved})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/approve/respond", bytes.NewBuffer(body))
	svc.HandleRespond(w, req)
	return w.Code
}

func TestRequestApproval_Decisions(t *testing.T) {
	tests := []struct {
		name     string
		approved bool
	}{
		{"approved", true},
		{"denied", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService()
			done := make(chan bool, 1)
			go func() {
				approved, err := svc.RequestApproval(context.Background(), "my_tool", map[string]any{"arg": "val"})
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				done <- approved
			}()

			id := waitForPending(t, svc)
			if code := respond(t, svc, id, tt.approved); code != http.StatusOK {
				t.Fatalf("respond code = %d, want 200", code)
			}

			select {
			case got := <-done:
				if got != tt.approved {
					t.Fatalf("approved = %v, want %v", got, tt.approved)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for decision")
			}
		})
	}
}

func TestRequestApproval_ContextCancel(t *testing.T) {
	svc := NewService()
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := svc.RequestApproval(ctx, "slow_tool", nil)
		errCh <- err
	}()

	waitForPending(t, svc)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RequestApproval did not honor ctx cancellation")
	}

	svc.mu.Lock()
	n := len(svc.pending)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("pending leak: %d entries after cancel", n)
	}
}

func TestNextID_Unique(t *testing.T) {
	// Regression for the ID-collision bug: IDs were derived from len(pending), so
	// two requests for the same tool at the same size collided. nextID now uses a
	// monotonic counter, so IDs are unique even under concurrency.
	svc := NewService()
	const n = 2000
	var (
		mu   sync.Mutex
		seen = make(map[string]bool, n)
		wg   sync.WaitGroup
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			id := svc.nextID("tool")
			mu.Lock()
			if seen[id] {
				t.Errorf("duplicate ID: %q", id)
			}
			seen[id] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("expected %d unique IDs, got %d", n, len(seen))
	}
}

func TestHandlePendingWithRequests(t *testing.T) {
	svc := NewService()
	ch := make(chan bool, 1)
	req := &Request{ID: "test_id", ToolName: "test_tool", Args: map[string]any{"x": 1}, Status: StatusPending, Response: ch}
	svc.mu.Lock()
	svc.pending["test_id"] = req
	svc.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approve/pending", http.NoBody)
	svc.HandlePending(w, r)

	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	list := resp["pending"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(list))
	}
	ch <- false
}

// denyAll is an Authorizer that rejects every responder.
type denyAll struct{}

func (denyAll) Authorize(*http.Request, *Request) error { return errors.New("forbidden") }

func TestHandleRespond_Authorized(t *testing.T) {
	svc := NewService(WithAuthorizer(denyAll{}))
	ch := make(chan bool, 1)
	svc.mu.Lock()
	svc.pending["req1"] = &Request{ID: "req1", ToolName: "t", Status: StatusPending, Response: ch}
	svc.mu.Unlock()

	if code := respond(t, svc, "req1", true); code != http.StatusForbidden {
		t.Fatalf("code = %d, want 403", code)
	}
}
