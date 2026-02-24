// Package approval provides human-in-the-loop approval workflows.
package approval

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Request represents a pending approval request.
type Request struct {
	ID       string         `json:"id"`
	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args"`
	Response chan bool      `json:"-"`
}

// Service manages pending approval requests.
type Service struct {
	mu      sync.Mutex
	pending map[string]*Request
}

func NewService() *Service {
	return &Service{pending: make(map[string]*Request)}
}

// RequestApproval submits a tool call for human approval and blocks until resolved.
func (s *Service) RequestApproval(toolName string, args map[string]any) (bool, error) {
	id := fmt.Sprintf("approval_%s_%d", toolName, len(s.pending))
	ch := make(chan bool, 1)
	req := &Request{ID: id, ToolName: toolName, Args: args, Response: ch}

	s.mu.Lock()
	s.pending[id] = req
	s.mu.Unlock()

	approved := <-ch

	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()

	return approved, nil
}

// HandlePending returns all pending approval requests.
func (s *Service) HandlePending(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := make([]*Request, 0, len(s.pending))
	for _, r := range s.pending {
		reqs = append(reqs, r)
	}
	json.NewEncoder(w).Encode(map[string]any{"pending": reqs})
}

// HandleRespond processes an approval response.
func (s *Service) HandleRespond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Approved bool   `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	req, ok := s.pending[body.ID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	req.Response <- body.Approved
	w.WriteHeader(http.StatusOK)
}
