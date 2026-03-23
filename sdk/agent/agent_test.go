package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage"
)

// testProvider is a mock model.Provider for testing.
type testProvider struct {
	response *model.ChatResponse
	err      error
	lastReq  *model.ChatRequest
}

func (p *testProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.lastReq = req
	if p.err != nil {
		return nil, p.err
	}
	return p.response, nil
}

func (p *testProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *testProvider) Name() string  { return "test" }
func (p *testProvider) Model() string { return "test-model" }

// testStorage implements storage.Storage in-memory for testing.
type testStorage struct {
	sessions    map[string]*storage.Session
	events      map[string][]*storage.Event
	memory      map[string]*storage.MemoryRecord
	checkpoints map[string]*storage.Checkpoint
	traces      map[string]*storage.Trace
	auditLogs   map[string]*storage.AuditLog
}

func newTestStorage() *testStorage {
	return &testStorage{
		sessions:    make(map[string]*storage.Session),
		events:      make(map[string][]*storage.Event),
		memory:      make(map[string]*storage.MemoryRecord),
		checkpoints: make(map[string]*storage.Checkpoint),
		traces:      make(map[string]*storage.Trace),
		auditLogs:   make(map[string]*storage.AuditLog),
	}
}

func (s *testStorage) CreateSession(_ context.Context, sess *storage.Session) error {
	s.sessions[sess.ID] = sess
	return nil
}

func (s *testStorage) GetSession(_ context.Context, id string) (*storage.Session, error) {
	if sess, ok := s.sessions[id]; ok {
		return sess, nil
	}
	return nil, errors.New("session not found")
}

func (s *testStorage) UpdateSession(_ context.Context, sess *storage.Session) error {
	s.sessions[sess.ID] = sess
	return nil
}

func (s *testStorage) ListSessions(_ context.Context, agentID string, limit, _ int) ([]*storage.Session, error) {
	var result []*storage.Session
	for _, sess := range s.sessions {
		if sess.AgentID == agentID {
			result = append(result, sess)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (s *testStorage) PutMemory(_ context.Context, m *storage.MemoryRecord) error {
	s.memory[m.ID] = m
	return nil
}

func (s *testStorage) GetMemory(_ context.Context, _, key string) (*storage.MemoryRecord, error) {
	for _, m := range s.memory {
		if m.Key == key {
			return m, nil
		}
	}
	return nil, errors.New("memory not found")
}

func (s *testStorage) ListMemory(_ context.Context, _ string, _ string) ([]*storage.MemoryRecord, error) {
	var result []*storage.MemoryRecord
	for _, m := range s.memory {
		result = append(result, m)
	}
	return result, nil
}

func (s *testStorage) DeleteMemory(_ context.Context, id string) error {
	delete(s.memory, id)
	return nil
}

func (s *testStorage) AppendAuditLog(_ context.Context, log *storage.AuditLog) error {
	s.auditLogs[log.ID] = log
	return nil
}

func (s *testStorage) ListAuditLogs(_ context.Context, _ string, _, _ int) ([]*storage.AuditLog, error) {
	return nil, nil
}

func (s *testStorage) InsertTrace(_ context.Context, t *storage.Trace) error {
	s.traces[t.ID] = t
	return nil
}

func (s *testStorage) GetTrace(_ context.Context, id string) (*storage.Trace, error) {
	if t, ok := s.traces[id]; ok {
		return t, nil
	}
	return nil, errors.New("trace not found")
}

func (s *testStorage) ListTraces(_ context.Context, _ string) ([]*storage.Trace, error) {
	return nil, nil
}

func (s *testStorage) AppendEvent(_ context.Context, e *storage.Event) error {
	s.events[e.SessionID] = append(s.events[e.SessionID], e)
	return nil
}

func (s *testStorage) ListEvents(_ context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	var result []*storage.Event
	for _, e := range s.events[sessionID] {
		if e.SeqNum > afterSeq {
			result = append(result, e)
		}
	}
	return result, nil
}

func (s *testStorage) SaveCheckpoint(_ context.Context, cp *storage.Checkpoint) error {
	s.checkpoints[cp.ID] = cp
	return nil
}

func (s *testStorage) GetCheckpoint(_ context.Context, id string) (*storage.Checkpoint, error) {
	if cp, ok := s.checkpoints[id]; ok {
		return cp, nil
	}
	return nil, errors.New("checkpoint not found")
}

func (s *testStorage) GetLatestCheckpoint(_ context.Context, _ string) (*storage.Checkpoint, error) {
	return nil, errors.New("no checkpoint")
}

func (s *testStorage) ListCheckpoints(_ context.Context, _ string) ([]*storage.Checkpoint, error) {
	return nil, nil
}

func (s *testStorage) Migrate(_ context.Context) error { return nil }
func (s *testStorage) Close() error                    { return nil }

// newTestAgent creates an Agent with all required fields initialized.
func newTestAgent(id string, provider model.Provider) *Agent {
	return &Agent{
		ID:         id,
		Model:      provider,
		Tools:      tool.NewRegistry(),
		Guardrails: guardrails.NewEngine(),
	}
}

// --- P0-004: NumHistoryRuns Tests ---

func TestChat_WithNumHistoryRuns(t *testing.T) {
	store := newTestStorage()
	ctx := context.Background()

	store.CreateSession(ctx, &storage.Session{
		ID: "prev-session", AgentID: "test-agent", Status: "completed",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	store.AppendEvent(ctx, &storage.Event{
		ID: "e1", SessionID: "prev-session", SeqNum: 1,
		Type: "chat_message",
		Payload: map[string]any{
			"role": "user", "content": "What is Go?",
		},
	})
	store.AppendEvent(ctx, &storage.Event{
		ID: "e2", SessionID: "prev-session", SeqNum: 2,
		Type: "chat_message",
		Payload: map[string]any{
			"role": "assistant", "content": "Go is a programming language.",
		},
	})

	provider := &testProvider{
		response: &model.ChatResponse{Content: "I remember our previous conversation."},
	}

	agent := newTestAgent("test-agent", provider)
	agent.NumHistoryRuns = 3
	agent.Storage = store

	resp, err := agent.Chat(ctx, "Do you remember our chat?")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "I remember our previous conversation." {
		t.Errorf("unexpected content: %q", resp.Content)
	}

	foundHistory := false
	for _, msg := range provider.lastReq.Messages {
		if msg.Role == model.RoleSystem && len(msg.Content) > 0 {
			if contains(msg.Content, "Previous conversation history") {
				foundHistory = true
				if !contains(msg.Content, "What is Go?") {
					t.Error("history should contain the user message")
				}
				if !contains(msg.Content, "Go is a programming language") {
					t.Error("history should contain the assistant response")
				}
			}
		}
	}
	if !foundHistory {
		t.Error("expected history messages to be injected into context")
	}
}

func TestChat_WithoutNumHistoryRuns(t *testing.T) {
	provider := &testProvider{
		response: &model.ChatResponse{Content: "response"},
	}

	agent := newTestAgent("test-agent", provider)

	_, err := agent.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	for _, msg := range provider.lastReq.Messages {
		if msg.Role == model.RoleSystem && contains(msg.Content, "Previous conversation history") {
			t.Error("should not inject history when NumHistoryRuns is 0")
		}
	}
}

func TestChat_NumHistoryRunsWithoutStorage(t *testing.T) {
	provider := &testProvider{
		response: &model.ChatResponse{Content: "response"},
	}

	agent := newTestAgent("test-agent", provider)
	agent.NumHistoryRuns = 5

	_, err := agent.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Chat should succeed without storage: %v", err)
	}
}

func TestLoadHistoryRuns_NoSessions(t *testing.T) {
	store := newTestStorage()
	agent := &Agent{
		ID:             "test-agent",
		Storage:        store,
		NumHistoryRuns: 3,
	}

	msgs := agent.loadHistoryRuns(context.Background())
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestLoadHistoryRuns_FiltersRoles(t *testing.T) {
	store := newTestStorage()
	ctx := context.Background()

	store.CreateSession(ctx, &storage.Session{
		ID: "s1", AgentID: "a1", Status: "completed",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	store.AppendEvent(ctx, &storage.Event{
		ID: "e1", SessionID: "s1", SeqNum: 1,
		Type:    "chat_message",
		Payload: map[string]any{"role": "system", "content": "You are helpful."},
	})
	store.AppendEvent(ctx, &storage.Event{
		ID: "e2", SessionID: "s1", SeqNum: 2,
		Type:    "chat_message",
		Payload: map[string]any{"role": "user", "content": "Hello"},
	})
	store.AppendEvent(ctx, &storage.Event{
		ID: "e3", SessionID: "s1", SeqNum: 3,
		Type:    "chat_message",
		Payload: map[string]any{"role": "assistant", "content": "Hi there"},
	})

	agent := &Agent{ID: "a1", Storage: store, NumHistoryRuns: 5}
	msgs := agent.loadHistoryRuns(ctx)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(msgs))
	}
	if msgs[0].Role != model.RoleUser {
		t.Errorf("msgs[0].Role = %q, want %q", msgs[0].Role, model.RoleUser)
	}
	if msgs[1].Role != model.RoleAssistant {
		t.Errorf("msgs[1].Role = %q, want %q", msgs[1].Role, model.RoleAssistant)
	}
}

// --- P0-005: OutputSchema Tests ---

func TestChat_WithOutputSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
		"required": []any{"name", "age"},
	}

	provider := &testProvider{
		response: &model.ChatResponse{Content: `{"name":"Alice","age":30}`},
	}

	agent := newTestAgent("test-agent", provider)
	agent.OutputSchema = schema

	resp, err := agent.Chat(context.Background(), "Who are you?")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != `{"name":"Alice","age":30}` {
		t.Errorf("unexpected content: %q", resp.Content)
	}

	if provider.lastReq.ResponseFormat != "json_schema" {
		t.Errorf("ResponseFormat = %q, want %q", provider.lastReq.ResponseFormat, "json_schema")
	}
	if provider.lastReq.Metadata == nil {
		t.Fatal("expected Metadata to be set on request")
	}
	if _, ok := provider.lastReq.Metadata["json_schema"]; !ok {
		t.Error("expected json_schema to be set in Metadata")
	}
}

func TestChat_OutputSchemaValidation_MissingRequired(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
		"required": []any{"name", "age"},
	}

	provider := &testProvider{
		response: &model.ChatResponse{Content: `{"name":"Alice"}`},
	}

	agent := newTestAgent("test-agent", provider)
	agent.OutputSchema = schema

	_, err := agent.Chat(context.Background(), "Who are you?")
	if err == nil {
		t.Fatal("expected validation error for missing required field 'age'")
	}
	if !contains(err.Error(), "required field") {
		t.Errorf("error should mention 'required field', got: %v", err)
	}
}

func TestChat_OutputSchemaValidation_WrongType(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "number"},
		},
	}

	provider := &testProvider{
		response: &model.ChatResponse{Content: `{"name":"Alice","age":"not a number"}`},
	}

	agent := newTestAgent("test-agent", provider)
	agent.OutputSchema = schema

	_, err := agent.Chat(context.Background(), "Who are you?")
	if err == nil {
		t.Fatal("expected validation error for wrong type")
	}
	if !contains(err.Error(), "expected number") {
		t.Errorf("error should mention type mismatch, got: %v", err)
	}
}

func TestChat_OutputSchemaValidation_InvalidJSON(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}

	provider := &testProvider{
		response: &model.ChatResponse{Content: "not json at all"},
	}

	agent := newTestAgent("test-agent", provider)
	agent.OutputSchema = schema

	_, err := agent.Chat(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected validation error for invalid JSON")
	}
	if !contains(err.Error(), "not valid JSON") {
		t.Errorf("error should mention invalid JSON, got: %v", err)
	}
}

func TestChat_WithoutOutputSchema(t *testing.T) {
	provider := &testProvider{
		response: &model.ChatResponse{Content: "plain text response"},
	}

	agent := newTestAgent("test-agent", provider)

	resp, err := agent.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "plain text response" {
		t.Errorf("content = %q", resp.Content)
	}
	if provider.lastReq.ResponseFormat != "" {
		t.Errorf("ResponseFormat should be empty, got %q", provider.lastReq.ResponseFormat)
	}
}

func TestApplyOutputSchema(t *testing.T) {
	tests := []struct {
		name           string
		schema         map[string]any
		wantFormat     string
		wantHasSchema  bool
	}{
		{
			name:           "nil schema",
			schema:         nil,
			wantFormat:     "",
			wantHasSchema:  false,
		},
		{
			name: "with schema",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
			wantFormat:    "json_schema",
			wantHasSchema: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.ChatRequest{}
			applyOutputSchema(req, tt.schema)
			if req.ResponseFormat != tt.wantFormat {
				t.Errorf("ResponseFormat = %q, want %q", req.ResponseFormat, tt.wantFormat)
			}
			if tt.wantHasSchema {
				if req.Metadata == nil {
					t.Fatal("expected Metadata to be set")
				}
				if _, ok := req.Metadata["json_schema"]; !ok {
					t.Error("expected json_schema in Metadata")
				}
			}
		})
	}
}

func TestValidateAgainstSchema(t *testing.T) {
	tests := []struct {
		name    string
		content string
		schema  map[string]any
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid object",
			content: `{"name":"Alice","age":30}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
					"age":  map[string]any{"type": "number"},
				},
				"required": []any{"name", "age"},
			},
			wantErr: false,
		},
		{
			name:    "missing required field",
			content: `{"name":"Alice"}`,
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
				"required":   []any{"name", "age"},
			},
			wantErr: true,
			errMsg:  "required field",
		},
		{
			name:    "wrong type - string instead of number",
			content: `{"value":"abc"}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"value": map[string]any{"type": "number"},
				},
			},
			wantErr: true,
			errMsg:  "expected number",
		},
		{
			name:    "wrong type - number instead of string",
			content: `{"name":42}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			wantErr: true,
			errMsg:  "expected string",
		},
		{
			name:    "wrong type - boolean expected",
			content: `{"active":"yes"}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"active": map[string]any{"type": "boolean"},
				},
			},
			wantErr: true,
			errMsg:  "expected boolean",
		},
		{
			name:    "wrong type - array expected",
			content: `{"items":"not-array"}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{"type": "array"},
				},
			},
			wantErr: true,
			errMsg:  "expected array",
		},
		{
			name:    "wrong type - object expected",
			content: `{"config":"not-object"}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"config": map[string]any{"type": "object"},
				},
			},
			wantErr: true,
			errMsg:  "expected object",
		},
		{
			name:    "valid with extra fields",
			content: `{"name":"Alice","extra":"field"}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
				"required": []any{"name"},
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			content: "not json",
			schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			wantErr: true,
			errMsg:  "not valid JSON",
		},
		{
			name:    "schema without properties",
			content: `{"anything":"goes"}`,
			schema: map[string]any{
				"type": "object",
			},
			wantErr: false,
		},
		{
			name:    "valid with boolean",
			content: `{"active":true}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"active": map[string]any{"type": "boolean"},
				},
			},
			wantErr: false,
		},
		{
			name:    "valid with array",
			content: `{"tags":["a","b"]}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tags": map[string]any{"type": "array"},
				},
			},
			wantErr: false,
		},
		{
			name:    "valid with nested object",
			content: `{"meta":{"key":"val"}}`,
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"meta": map[string]any{"type": "object"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAgainstSchema(tt.content, tt.schema)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckJSONType(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		val      any
		expected string
		wantErr  bool
	}{
		{"string match", "f", "hello", "string", false},
		{"string mismatch", "f", 42.0, "string", true},
		{"number match", "f", 42.0, "number", false},
		{"integer match", "f", 42.0, "integer", false},
		{"number mismatch", "f", "abc", "number", true},
		{"boolean match", "f", true, "boolean", false},
		{"boolean mismatch", "f", "yes", "boolean", true},
		{"array match", "f", []any{1, 2}, "array", false},
		{"array mismatch", "f", "not array", "array", true},
		{"object match", "f", map[string]any{"k": "v"}, "object", false},
		{"object mismatch", "f", 42.0, "object", true},
		{"unknown type", "f", "anything", "custom_type", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkJSONType(tt.field, tt.val, tt.expected)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestFormatHistoryMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "Hello"},
		{Role: model.RoleAssistant, Content: "Hi there"},
		{Role: "custom", Content: "Custom role"},
	}

	result := formatHistoryMessages(msgs)
	if !contains(result, "User: Hello") {
		t.Error("should contain user message")
	}
	if !contains(result, "Assistant: Hi there") {
		t.Error("should contain assistant message")
	}
	if !contains(result, "custom: Custom role") {
		t.Error("should contain custom role message")
	}
}

// --- Chat integration tests ---

func TestChat_NoModel(t *testing.T) {
	agent := &Agent{ID: "no-model"}
	_, err := agent.Chat(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error for agent without model")
	}
	if !contains(err.Error(), "no model") {
		t.Errorf("error should mention 'no model', got: %v", err)
	}
}

func TestChat_WithSystemPrompt(t *testing.T) {
	provider := &testProvider{
		response: &model.ChatResponse{Content: "response"},
	}

	agent := newTestAgent("test", provider)
	agent.SystemPrompt = "You are a test agent."
	agent.Instructions = []string{"Be concise", "Use Go"}

	_, err := agent.Chat(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	msgs := provider.lastReq.Messages
	if len(msgs) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "You are a test agent." {
		t.Errorf("first message should be system prompt, got: %q", msgs[0].Content)
	}
	if msgs[1].Content != "Be concise" {
		t.Errorf("second message should be first instruction, got: %q", msgs[1].Content)
	}
}

func TestChat_RetryHookIntegration(t *testing.T) {
	callCount := 0
	provider := &testProvider{}
	provider.err = errors.New("transient")

	retryProvider := &testProvider{
		response: &model.ChatResponse{Content: "retried"},
	}

	hook := hooks.NewRetryHook(2)
	hook.SleepFn = func(_ time.Duration) {}

	agent := newTestAgent("test", provider)
	agent.Hooks = hooks.Chain{hook}

	_ = callCount
	_ = retryProvider
	// The retry hook needs provider in metadata to work.
	// In the agent.Chat flow, we now pass the provider.
	// Since testProvider always returns an error, the retry will also fail
	// and the chat will return an error.
	_, err := agent.Chat(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error when provider always fails")
	}
}

func TestChat_InputGuardrailBlocks(t *testing.T) {
	provider := &testProvider{
		response: &model.ChatResponse{Content: "response"},
	}

	engine := guardrails.NewEngine()
	engine.AddRule(guardrails.Rule{
		Name:      "block-test",
		Position:  guardrails.Input,
		Guardrail: &guardrails.BlocklistGuardrail{Blocklist: []string{"forbidden"}},
	})

	agent := newTestAgent("test", provider)
	agent.Guardrails = engine

	_, err := agent.Chat(context.Background(), "this is forbidden content")
	if err == nil {
		t.Fatal("expected guardrail to block input")
	}
	if !contains(err.Error(), "guardrail") {
		t.Errorf("error should mention guardrail: %v", err)
	}
}

// --- Helper ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify the JSON schema is properly structured
func TestOutputSchema_JSONRoundtrip(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sentiment": map[string]any{"type": "string"},
			"score":     map[string]any{"type": "number"},
			"keywords":  map[string]any{"type": "array"},
		},
		"required": []any{"sentiment", "score"},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}

	var roundtripped map[string]any
	if err := json.Unmarshal(data, &roundtripped); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	props := roundtripped["properties"].(map[string]any)
	if len(props) != 3 {
		t.Errorf("expected 3 properties, got %d", len(props))
	}
}
