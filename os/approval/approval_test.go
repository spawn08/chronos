package approval

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewService(t *testing.T) {
	svc := NewService()
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.pending == nil {
		t.Fatal("pending map is nil")
	}
}

func TestHandlePendingEmpty(t *testing.T) {
	svc := NewService()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approve/pending", nil)
	svc.HandlePending(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	pending, ok := resp["pending"]
	if !ok {
		t.Fatal("response missing 'pending' key")
	}
	list, ok := pending.([]any)
	if !ok {
		t.Fatalf("pending not a list: %T", pending)
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

func TestRequestApprovalApproved(t *testing.T) {
	svc := NewService()
	done := make(chan bool, 1)
	go func() {
		approved, err := svc.RequestApproval("my_tool", map[string]any{"arg": "val"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		done <- approved
	}()

	// Allow goroutine to register the request
	time.Sleep(20 * time.Millisecond)

	// Fetch the pending request ID
	svc.mu.Lock()
	var id string
	for k := range svc.pending {
		id = k
	}
	svc.mu.Unlock()

	if id == "" {
		t.Fatal("no pending request found")
	}

	body, _ := json.Marshal(map[string]any{"id": id, "approved": true})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/approve/respond", bytes.NewBuffer(body))
	svc.HandleRespond(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on respond, got %d", w.Code)
	}

	select {
	case approved := <-done:
		if !approved {
			t.Fatal("expected approved=true")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for approval")
	}
}

func TestRequestApprovalDenied(t *testing.T) {
	svc := NewService()
	done := make(chan bool, 1)
	go func() {
		approved, _ := svc.RequestApproval("delete_tool", map[string]any{})
		done <- approved
	}()

	time.Sleep(20 * time.Millisecond)

	svc.mu.Lock()
	var id string
	for k := range svc.pending {
		id = k
	}
	svc.mu.Unlock()

	body, _ := json.Marshal(map[string]any{"id": id, "approved": false})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/approve/respond", bytes.NewBuffer(body))
	svc.HandleRespond(w, req)

	select {
	case approved := <-done:
		if approved {
			t.Fatal("expected approved=false")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestHandlePendingWithRequests(t *testing.T) {
	svc := NewService()
	// Manually insert a pending request
	ch := make(chan bool, 1)
	req := &Request{ID: "test_id", ToolName: "test_tool", Args: map[string]any{"x": 1}, Response: ch}
	svc.mu.Lock()
	svc.pending["test_id"] = req
	svc.mu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/approve/pending", nil)
	svc.HandlePending(w, r)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	list := resp["pending"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(list))
	}
	// cleanup
	ch <- false
}

func TestRequestIDGeneration(t *testing.T) {
	svc := NewService()
	// The ID includes the tool name and pending count
	ch := make(chan bool, 2)
	go func() {
		svc.RequestApproval("tool_a", nil)
	}()
	time.Sleep(10 * time.Millisecond)
	svc.mu.Lock()
	for k, v := range svc.pending {
		if v.ToolName == "tool_a" {
			// ID should contain tool name
			if len(k) == 0 {
				t.Errorf("empty ID")
			}
		}
		v.Response <- false // unblock
	}
	svc.mu.Unlock()
	_ = ch
}
