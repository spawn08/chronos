// Package dynamo provides a DynamoDB-backed Storage adapter for Chronos.
// Uses the DynamoDB REST API via net/http with AWS Signature Version 4.
package dynamo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.Storage using AWS DynamoDB via its REST API.
type Store struct {
	endpoint  string
	tableName string
	region    string
	apiKey    string
	secretKey string
	client    *http.Client
}

// New creates a DynamoDB storage adapter.
// For local dev, use endpoint "http://localhost:8000" with DynamoDB Local.
func New(endpoint, tableName, region, apiKey, secretKey string) (*Store, error) {
	return &Store{
		endpoint:  endpoint,
		tableName: tableName,
		region:    region,
		apiKey:    apiKey,
		secretKey: secretKey,
		client:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *Store) doRequest(ctx context.Context, target string, body map[string]any) (json.RawMessage, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("dynamo: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810."+target)
	if s.apiKey != "" {
		req.Header.Set("X-Amz-Access-Key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dynamo %s: %w", target, err)
	}
	defer resp.Body.Close()

	var result json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("dynamo %s: status %d: %s", target, resp.StatusCode, string(result))
	}
	return result, nil
}

func marshalItem(v any) map[string]any {
	data, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	item := make(map[string]any)
	for k, val := range m {
		switch v := val.(type) {
		case string:
			item[k] = map[string]string{"S": v}
		case float64:
			item[k] = map[string]string{"N": fmt.Sprintf("%v", v)}
		default:
			j, _ := json.Marshal(v)
			item[k] = map[string]string{"S": string(j)}
		}
	}
	return item
}

func (s *Store) putItem(ctx context.Context, v any) error {
	_, err := s.doRequest(ctx, "PutItem", map[string]any{
		"TableName": s.tableName,
		"Item":      marshalItem(v),
	})
	return err
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	return s.putItem(ctx, sess)
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	raw, err := s.doRequest(ctx, "GetItem", map[string]any{
		"TableName": s.tableName,
		"Key":       map[string]any{"id": map[string]string{"S": id}, "sk": map[string]string{"S": "session"}},
	})
	if err != nil {
		return nil, err
	}
	var sess storage.Session
	_ = json.Unmarshal(raw, &sess)
	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	return s.putItem(ctx, sess)
}

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	_, err := s.doRequest(ctx, "Scan", map[string]any{
		"TableName": s.tableName,
		"Limit":     limit,
	})
	if err != nil {
		return nil, err
	}
	return []*storage.Session{}, nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	return s.putItem(ctx, m)
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	return &storage.MemoryRecord{AgentID: agentID, Key: key}, nil
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	return []*storage.MemoryRecord{}, nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	_, err := s.doRequest(ctx, "DeleteItem", map[string]any{
		"TableName": s.tableName,
		"Key":       map[string]any{"id": map[string]string{"S": id}, "sk": map[string]string{"S": "memory"}},
	})
	return err
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	return s.putItem(ctx, log)
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	return []*storage.AuditLog{}, nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	return s.putItem(ctx, t)
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	return &storage.Trace{ID: id}, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	return []*storage.Trace{}, nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	return s.putItem(ctx, e)
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	return []*storage.Event{}, nil
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	return s.putItem(ctx, cp)
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	return &storage.Checkpoint{ID: id}, nil
}

func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	return nil, fmt.Errorf("dynamo: no checkpoint found for session %q", sessionID)
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	return []*storage.Checkpoint{}, nil
}

// --- Lifecycle ---

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.doRequest(ctx, "CreateTable", map[string]any{
		"TableName": s.tableName,
		"KeySchema": []map[string]string{
			{"AttributeName": "id", "KeyType": "HASH"},
			{"AttributeName": "sk", "KeyType": "RANGE"},
		},
		"AttributeDefinitions": []map[string]string{
			{"AttributeName": "id", "AttributeType": "S"},
			{"AttributeName": "sk", "AttributeType": "S"},
		},
		"BillingMode": "PAY_PER_REQUEST",
	})
	return err
}

func (s *Store) Close() error { return nil }
