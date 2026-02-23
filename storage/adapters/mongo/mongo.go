// Package mongo provides a MongoDB-backed Storage adapter for Chronos.
// Uses the MongoDB wire protocol via net/http against the MongoDB Atlas Data API,
// or a local MongoDB instance via a simple REST wrapper.
package mongo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.Storage using MongoDB via its Data API.
type Store struct {
	baseURL    string
	apiKey     string
	database   string
	dataSource string
	client     *http.Client
}

// New creates a MongoDB storage adapter.
// baseURL should point to the MongoDB Atlas Data API (e.g. https://data.mongodb-api.com/app/<id>/endpoint/data/v1).
// For local dev, set up a Data API proxy or use the Atlas free tier.
func New(baseURL, apiKey, database, dataSource string) (*Store, error) {
	return &Store{
		baseURL:    baseURL,
		apiKey:     apiKey,
		database:   database,
		dataSource: dataSource,
		client:     &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *Store) do(ctx context.Context, action, collection string, body map[string]any) (json.RawMessage, error) {
	body["collection"] = collection
	body["database"] = s.database
	body["dataSource"] = s.dataSource

	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/action/"+action, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("mongo %s: %w", action, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mongo %s: %w", action, err)
	}
	defer resp.Body.Close()

	var result json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("mongo %s decode: %w", action, err)
	}
	return result, nil
}

func (s *Store) insertOne(ctx context.Context, collection string, doc any) error {
	_, err := s.do(ctx, "insertOne", collection, map[string]any{"document": doc})
	return err
}

func (s *Store) findOne(ctx context.Context, collection string, filter map[string]any, out any) error {
	raw, err := s.do(ctx, "findOne", collection, map[string]any{"filter": filter})
	if err != nil {
		return err
	}
	var wrapper struct {
		Document json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return fmt.Errorf("mongo findOne unmarshal: %w", err)
	}
	if wrapper.Document == nil {
		return fmt.Errorf("mongo: document not found")
	}
	return json.Unmarshal(wrapper.Document, out)
}

func (s *Store) find(ctx context.Context, collection string, filter map[string]any, sort map[string]any, limit int) (json.RawMessage, error) {
	body := map[string]any{"filter": filter}
	if sort != nil {
		body["sort"] = sort
	}
	if limit > 0 {
		body["limit"] = limit
	}
	raw, err := s.do(ctx, "find", collection, body)
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		Documents json.RawMessage `json:"documents"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Documents, nil
}

func (s *Store) updateOne(ctx context.Context, collection string, filter, update map[string]any) error {
	_, err := s.do(ctx, "updateOne", collection, map[string]any{"filter": filter, "update": update})
	return err
}

func (s *Store) deleteOne(ctx context.Context, collection string, filter map[string]any) error {
	_, err := s.do(ctx, "deleteOne", collection, map[string]any{"filter": filter})
	return err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	return s.insertOne(ctx, "sessions", sess)
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	var sess storage.Session
	if err := s.findOne(ctx, "sessions", map[string]any{"id": id}, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	return s.updateOne(ctx, "sessions", map[string]any{"id": sess.ID}, map[string]any{"$set": sess})
}

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	filter := map[string]any{}
	if agentID != "" {
		filter["agent_id"] = agentID
	}
	raw, err := s.find(ctx, "sessions", filter, map[string]any{"created_at": -1}, limit)
	if err != nil {
		return nil, err
	}
	var sessions []*storage.Session
	_ = json.Unmarshal(raw, &sessions)
	return sessions, nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	_ = s.deleteOne(ctx, "memory", map[string]any{"id": m.ID})
	return s.insertOne(ctx, "memory", m)
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	var m storage.MemoryRecord
	if err := s.findOne(ctx, "memory", map[string]any{"agent_id": agentID, "key": key}, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	raw, err := s.find(ctx, "memory", map[string]any{"agent_id": agentID, "kind": kind}, nil, 0)
	if err != nil {
		return nil, err
	}
	var mems []*storage.MemoryRecord
	_ = json.Unmarshal(raw, &mems)
	return mems, nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	return s.deleteOne(ctx, "memory", map[string]any{"id": id})
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	return s.insertOne(ctx, "audit_logs", log)
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	raw, err := s.find(ctx, "audit_logs", map[string]any{"session_id": sessionID}, map[string]any{"created_at": -1}, limit)
	if err != nil {
		return nil, err
	}
	var logs []*storage.AuditLog
	_ = json.Unmarshal(raw, &logs)
	return logs, nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	return s.insertOne(ctx, "traces", t)
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	var t storage.Trace
	if err := s.findOne(ctx, "traces", map[string]any{"id": id}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	raw, err := s.find(ctx, "traces", map[string]any{"session_id": sessionID}, map[string]any{"started_at": 1}, 0)
	if err != nil {
		return nil, err
	}
	var traces []*storage.Trace
	_ = json.Unmarshal(raw, &traces)
	return traces, nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	return s.insertOne(ctx, "events", e)
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	raw, err := s.find(ctx, "events", map[string]any{"session_id": sessionID, "seq_num": map[string]any{"$gt": afterSeq}}, map[string]any{"seq_num": 1}, 0)
	if err != nil {
		return nil, err
	}
	var events []*storage.Event
	_ = json.Unmarshal(raw, &events)
	return events, nil
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	return s.insertOne(ctx, "checkpoints", cp)
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	var cp storage.Checkpoint
	if err := s.findOne(ctx, "checkpoints", map[string]any{"id": id}, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	raw, err := s.find(ctx, "checkpoints", map[string]any{"session_id": sessionID}, map[string]any{"created_at": -1}, 1)
	if err != nil {
		return nil, err
	}
	var cps []*storage.Checkpoint
	_ = json.Unmarshal(raw, &cps)
	if len(cps) == 0 {
		return nil, fmt.Errorf("mongo: no checkpoint found for session %q", sessionID)
	}
	return cps[0], nil
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	raw, err := s.find(ctx, "checkpoints", map[string]any{"session_id": sessionID}, map[string]any{"seq_num": 1}, 0)
	if err != nil {
		return nil, err
	}
	var cps []*storage.Checkpoint
	_ = json.Unmarshal(raw, &cps)
	return cps, nil
}

// --- Lifecycle ---

func (s *Store) Migrate(_ context.Context) error {
	return nil
}

func (s *Store) Close() error {
	return nil
}
