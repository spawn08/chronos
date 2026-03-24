package storage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionJSONRoundtrip(t *testing.T) {
	sess := Session{
		ID:        "s1",
		AgentID:   "agent-1",
		Status:    "running",
		Metadata:  map[string]any{"key": "val"},
		CreatedAt: time.Now().Truncate(time.Second),
		UpdatedAt: time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Session
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != sess.ID {
		t.Errorf("ID: got %q, want %q", out.ID, sess.ID)
	}
	if out.Status != sess.Status {
		t.Errorf("Status: got %q, want %q", out.Status, sess.Status)
	}
}

func TestMemoryRecordJSONRoundtrip(t *testing.T) {
	m := MemoryRecord{
		ID:        "m1",
		SessionID: "s1",
		AgentID:   "a1",
		UserID:    "u1",
		Kind:      "short_term",
		Key:       "mykey",
		Value:     "myvalue",
		CreatedAt: time.Now().Truncate(time.Second),
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out MemoryRecord
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Key != m.Key {
		t.Errorf("Key: got %q, want %q", out.Key, m.Key)
	}
}

func TestAuditLogJSONRoundtrip(t *testing.T) {
	log := AuditLog{
		ID:        "l1",
		SessionID: "s1",
		Actor:     "user",
		Action:    "delete",
		Resource:  "item/1",
		Detail:    map[string]any{"reason": "test"},
		CreatedAt: time.Now().Truncate(time.Second),
	}
	data, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AuditLog
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Action != log.Action {
		t.Errorf("Action: got %q, want %q", out.Action, log.Action)
	}
}

func TestTraceJSONRoundtrip(t *testing.T) {
	tr := Trace{
		ID:        "t1",
		SessionID: "s1",
		ParentID:  "p1",
		Name:      "my_node",
		Kind:      "node",
		Error:     "",
		StartedAt: time.Now().Truncate(time.Second),
		EndedAt:   time.Now().Truncate(time.Second).Add(time.Second),
	}
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Trace
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Name != tr.Name {
		t.Errorf("Name: got %q, want %q", out.Name, tr.Name)
	}
}

func TestEventJSONRoundtrip(t *testing.T) {
	ev := Event{
		ID:        "e1",
		SessionID: "s1",
		SeqNum:    42,
		Type:      "node_completed",
		Payload:   map[string]any{"output": "done"},
		CreatedAt: time.Now().Truncate(time.Second),
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Event
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.SeqNum != ev.SeqNum {
		t.Errorf("SeqNum: got %d, want %d", out.SeqNum, ev.SeqNum)
	}
}

func TestCheckpointJSONRoundtrip(t *testing.T) {
	cp := Checkpoint{
		ID:        "cp1",
		SessionID: "s1",
		RunID:     "run-1",
		NodeID:    "node-a",
		State:     map[string]any{"step": 3},
		SeqNum:    7,
		CreatedAt: time.Now().Truncate(time.Second),
	}
	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Checkpoint
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.NodeID != cp.NodeID {
		t.Errorf("NodeID: got %q, want %q", out.NodeID, cp.NodeID)
	}
	if out.SeqNum != cp.SeqNum {
		t.Errorf("SeqNum: got %d, want %d", out.SeqNum, cp.SeqNum)
	}
}

func TestEmbeddingJSONRoundtrip(t *testing.T) {
	emb := Embedding{
		ID:       "emb-1",
		Vector:   []float32{0.1, 0.2, 0.3},
		Metadata: map[string]any{"src": "doc"},
		Content:  "some text",
	}
	data, err := json.Marshal(emb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Embedding
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Vector) != 3 {
		t.Errorf("Vector length: got %d", len(out.Vector))
	}
	if out.Content != emb.Content {
		t.Errorf("Content: got %q", out.Content)
	}
}

func TestSearchResultJSONRoundtrip(t *testing.T) {
	sr := SearchResult{
		Embedding: Embedding{ID: "e1", Vector: []float32{1.0}, Content: "text"},
		Score:     0.99,
	}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out SearchResult
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Score != sr.Score {
		t.Errorf("Score: got %v, want %v", out.Score, sr.Score)
	}
	if out.ID != "e1" {
		t.Errorf("ID: got %q", out.ID)
	}
}

func TestConfigFields(t *testing.T) {
	cfg := Config{
		Backend:       "sqlite",
		DSN:           ":memory:",
		VectorBackend: "qdrant",
		VectorDSN:     "http://localhost:6333",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Backend != "sqlite" {
		t.Errorf("Backend: got %q", out.Backend)
	}
	if out.VectorBackend != "qdrant" {
		t.Errorf("VectorBackend: got %q", out.VectorBackend)
	}
}
