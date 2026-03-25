// Package a2a implements the Agent-to-Agent (A2A) protocol for cross-framework
// agent communication. It provides both server and client implementations.
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Task represents an A2A task.
type Task struct {
	ID        string         `json:"id"`
	Status    TaskStatus     `json:"status"`
	Input     string         `json:"input"`
	Output    string         `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// TaskStatus represents the status of an A2A task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled" //nolint:misspell // wire value; external clients may expect British spelling
)

// AgentCard describes an A2A agent's capabilities.
type AgentCard struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	InputSchema  any      `json:"input_schema,omitempty"`
	OutputSchema any      `json:"output_schema,omitempty"`
}

// Handler processes A2A tasks.
type Handler func(ctx context.Context, task *Task) error

// Server exposes an agent as an A2A endpoint.
type Server struct {
	card    AgentCard
	handler Handler
	mu      sync.RWMutex
	tasks   map[string]*Task
	counter int64
}

// NewServer creates an A2A server for the given agent.
func NewServer(card AgentCard, handler Handler) *Server {
	return &Server{
		card:    card,
		handler: handler,
		tasks:   make(map[string]*Task),
	}
}

// ServeHTTP handles A2A protocol requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/a2a")

	switch {
	case path == "/agent" && r.Method == http.MethodGet:
		s.handleAgentCard(w, r)
	case path == "/tasks" && r.Method == http.MethodPost:
		s.handleCreateTask(w, r)
	case strings.HasPrefix(path, "/tasks/") && r.Method == http.MethodGet:
		taskID := strings.TrimPrefix(path, "/tasks/")
		s.handleGetTask(w, r, taskID)
	case strings.HasPrefix(path, "/tasks/") && r.Method == http.MethodDelete:
		taskID := strings.TrimPrefix(path, "/tasks/")
		s.handleCancelTask(w, r, taskID)
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func (s *Server) handleAgentCard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.card)
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Input    string         `json:"input"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.counter++
	task := &Task{
		ID:        fmt.Sprintf("task_%d", s.counter),
		Status:    TaskStatusPending,
		Input:     req.Input,
		Metadata:  req.Metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.tasks[task.ID] = task

	// Snapshot for the response — executeTask will mutate task concurrently.
	snapshot := *task
	s.mu.Unlock()

	go s.executeTask(task)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(&snapshot)
}

func (s *Server) handleGetTask(w http.ResponseWriter, _ *http.Request, taskID string) {
	s.mu.RLock()
	task, ok := s.tasks[taskID]
	var snapshot Task
	if ok {
		snapshot = *task
	}
	s.mu.RUnlock()

	if !ok {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(&snapshot)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, _ *http.Request, taskID string) {
	s.mu.Lock()
	task, ok := s.tasks[taskID]
	if ok && (task.Status == TaskStatusPending || task.Status == TaskStatusRunning) {
		task.Status = TaskStatusCancelled
		task.UpdatedAt = time.Now()
	}
	var snapshot Task
	if ok {
		snapshot = *task
	}
	s.mu.Unlock()

	if !ok {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(&snapshot)
}

func (s *Server) executeTask(task *Task) {
	s.mu.Lock()
	task.Status = TaskStatusRunning
	task.UpdatedAt = time.Now()
	s.mu.Unlock()

	err := s.handler(context.Background(), task)

	s.mu.Lock()
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
	} else {
		task.Status = TaskStatusCompleted
	}
	task.UpdatedAt = time.Now()
	s.mu.Unlock()
}

// Client connects to an external A2A agent.
type Client struct {
	baseURL string
	client  *http.Client
}

// NewClient creates an A2A client for connecting to an external agent.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// GetAgentCard retrieves the agent's capability card.
func (c *Client) GetAgentCard(ctx context.Context) (*AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/a2a/agent", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("a2a agent card: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a agent card: %w", err)
	}
	defer resp.Body.Close()

	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("a2a agent card decode: %w", err)
	}
	return &card, nil
}

// CreateTask submits a task to the remote agent.
func (c *Client) CreateTask(ctx context.Context, input string, metadata map[string]any) (*Task, error) {
	body := map[string]any{"input": input}
	if metadata != nil {
		body["metadata"] = metadata
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/a2a/tasks", strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("a2a create task: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a create task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("a2a create task: HTTP %d: %s", resp.StatusCode, errBody)
	}

	var task Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, fmt.Errorf("a2a create task decode: %w", err)
	}
	return &task, nil
}

// GetTask polls the status of a task.
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/a2a/tasks/"+taskID, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("a2a get task: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("a2a get task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("a2a task %q not found", taskID)
	}

	var task Task
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		return nil, fmt.Errorf("a2a get task decode: %w", err)
	}
	return &task, nil
}

// WaitForCompletion polls a task until it reaches a terminal state.
func (c *Client) WaitForCompletion(ctx context.Context, taskID string, pollInterval time.Duration) (*Task, error) {
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			task, err := c.GetTask(ctx, taskID)
			if err != nil {
				return nil, err
			}
			switch task.Status {
			case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
				return task, nil
			}
		}
	}
}
