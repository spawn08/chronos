// Package a2a implements the Agent-to-Agent (A2A) protocol for cross-framework
// agent communication. It provides both server and client implementations.
package a2a

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spawn08/chronos/sdk/skill"
)

// maxStreamLine caps a single SSE data line the client will buffer (1 MiB),
// guarding against an unbounded server response.
const maxStreamLine = 1 << 20

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

// CardFromSkills builds an AgentCard whose Capabilities are the names of the
// skills installed in reg, so an A2A peer discovers exactly what the agent can
// do. A nil registry yields a card with no capabilities.
func CardFromSkills(name, description, version string, reg *skill.Registry) AgentCard {
	card := AgentCard{Name: name, Description: description, Version: version}
	if reg != nil {
		for _, s := range reg.List() {
			card.Capabilities = append(card.Capabilities, s.Name)
		}
	}
	return card
}

// Handler processes A2A tasks. It should be idempotent: on the durable backend a
// task may be executed more than once (queue retries and orphan recovery are
// at-least-once), and re-running simply overwrites the task's output.
type Handler func(ctx context.Context, task *Task) error

// Server exposes an agent as an A2A endpoint. Task lifecycle is delegated to a
// TaskStore, so the same HTTP surface serves both the in-memory default and the
// durable, restart-resumable queue backend (see NewDurableStore).
type Server struct {
	card  AgentCard
	store TaskStore
}

// NewServer creates an A2A server backed by the default in-memory store, which
// runs handler in a goroutine per task. Tasks do not survive a restart; use
// NewServerWithStore with a durable store for resumable tasks.
func NewServer(card AgentCard, handler Handler) *Server {
	return &Server{card: card, store: newMemStore(handler)}
}

// NewServerWithStore creates an A2A server backed by an explicit TaskStore
// (e.g. NewDurableStore for queue-backed, restart-resumable tasks).
func NewServerWithStore(card AgentCard, store TaskStore) *Server {
	return &Server{card: card, store: store}
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
		rest := strings.TrimPrefix(path, "/tasks/")
		if taskID, ok := strings.CutSuffix(rest, "/stream"); ok {
			s.handleStreamTask(w, r, taskID)
		} else {
			s.handleGetTask(w, r, rest)
		}
	case strings.HasPrefix(path, "/tasks/") && r.Method == http.MethodDelete:
		taskID := strings.TrimPrefix(path, "/tasks/")
		s.handleCancelTask(w, r, taskID)
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

// writeStoreError maps a TaskStore error to an HTTP response: ErrTaskNotFound
// (including a cross-tenant miss) becomes 404, anything else 500.
func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrTaskNotFound) {
		http.Error(w, `{"error":"task not found"}`, http.StatusNotFound)
		return
	}
	// Build the envelope with json.Marshal — %q is Go quoting, not JSON escaping,
	// so an exotic rune in the error could otherwise emit invalid JSON.
	body, _ := json.Marshal(map[string]string{"error": err.Error()})
	http.Error(w, string(body), http.StatusInternalServerError)
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

	task, err := s.store.Submit(r.Context(), req.Input, req.Metadata)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(task)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, taskID string) {
	task, err := s.store.Get(r.Context(), taskID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request, taskID string) {
	task, err := s.store.Cancel(r.Context(), taskID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(task)
}

// handleStreamTask streams task snapshots as Server-Sent Events until the task
// reaches a terminal state or the client disconnects. There is no server-imposed
// max stream duration: a task that stays running holds the connection (and its
// poll goroutine) until it terminates or the client's context is canceled.
func (s *Server) handleStreamTask(w http.ResponseWriter, r *http.Request, taskID string) {
	// Resolve first so an unknown/cross-tenant task is a clean 404 rather than an
	// empty stream.
	if _, err := s.store.Get(r.Context(), taskID); err != nil {
		writeStoreError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for task := range watch(r.Context(), s.store, taskID, 0) {
		data, err := json.Marshal(task)
		if err != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// Client connects to an external A2A agent.
type Client struct {
	baseURL string
	client  *http.Client
	// streamClient has no request timeout: an SSE task stream may stay open far
	// longer than a unary call, so it is bounded by the caller's context instead.
	streamClient *http.Client
}

// NewClient creates an A2A client for connecting to an external agent.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		client:       &http.Client{Timeout: 30 * time.Second},
		streamClient: &http.Client{},
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
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("a2a create task: marshal body: %w", err)
	}

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
			if isTerminal(task.Status) {
				return task, nil
			}
		}
	}
}

// StreamTask consumes the server's Server-Sent Events for a task, delivering a
// snapshot on each state transition until the task reaches a terminal state, the
// server closes the stream, or ctx is done. Exactly one of the two returned
// channels produces a value before both are closed: the task channel yields
// snapshots (the last being terminal), the error channel yields a single error.
func (c *Client) StreamTask(ctx context.Context, taskID string) (snapshots <-chan Task, errc <-chan error) {
	tasks := make(chan Task)
	errs := make(chan error, 1)

	go func() {
		defer close(tasks)
		defer close(errs)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			c.baseURL+"/a2a/tasks/"+taskID+"/stream", http.NoBody)
		if err != nil {
			errs <- fmt.Errorf("a2a stream task: %w", err)
			return
		}
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.streamClient.Do(req)
		if err != nil {
			errs <- fmt.Errorf("a2a stream task: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			errs <- fmt.Errorf("a2a stream task: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxStreamLine)
		for scanner.Scan() {
			line := scanner.Text()
			payload, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue // skip blank separators and non-data fields
			}
			var task Task
			if err := json.Unmarshal([]byte(payload), &task); err != nil {
				errs <- fmt.Errorf("a2a stream task decode: %w", err)
				return
			}
			select {
			case tasks <- task:
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			errs <- fmt.Errorf("a2a stream task read: %w", err)
		}
	}()

	return tasks, errs
}
