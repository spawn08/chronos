// Package mongo provides a MongoDB-backed Storage adapter for Chronos.
//
// It is built on the official MongoDB Go driver (go.mongodb.org/mongo-driver).
// The previous implementation talked to the MongoDB Atlas Data API over
// net/http — a REST service that MongoDB deprecated and shut down on
// 2024-09-30 — and mislabelled it as "the MongoDB wire protocol". That approach
// is dead; this rewrite speaks the real wire protocol through the maintained
// driver, which handles connection pooling, retries, auth and BSON.
//
// # Document naming
//
// Chronos entities carry json tags (id, agent_id, session_id, ...). To keep
// field names identical across every storage backend, documents are stored as
// the JSON projection of each entity (entity -> JSON -> map -> BSON) and decoded
// back the same way. Filters therefore use json-tag names (e.g. "agent_id").
package mongo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/spawn08/chronos/storage"
)

// Collection names.
const (
	collSessions    = "sessions"
	collMemory      = "memory"
	collAuditLogs   = "audit_logs"
	collTraces      = "traces"
	collEvents      = "events"
	collCheckpoints = "checkpoints"
)

// Store implements storage.Storage using MongoDB.
type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

// New connects to MongoDB at uri (e.g. "mongodb://localhost:27017") and selects
// the given database. Connectivity is verified with a ping.
func New(uri, database string) (*Store, error) {
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	return &Store{client: client, db: client.Database(database)}, nil
}

// NewWithClient wraps an existing mongo.Client (useful for tests / custom opts).
func NewWithClient(client *mongo.Client, database string) *Store {
	return &Store{client: client, db: client.Database(database)}
}

// toDoc converts an entity to a BSON document using its json field names.
func toDoc(v any) (bson.M, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("mongo encode: %w", err)
	}
	var m bson.M
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("mongo to-doc: %w", err)
	}
	return m, nil
}

// fromDoc decodes a BSON document into an entity via its json field names.
func fromDoc(m bson.M, out any) error {
	delete(m, "_id") // strip Mongo's synthetic id; entities carry their own.
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("mongo from-doc encode: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("mongo decode: %w", err)
	}
	return nil
}

func (s *Store) insert(ctx context.Context, coll string, v any) error {
	doc, err := toDoc(v)
	if err != nil {
		return err
	}
	if _, err := s.db.Collection(coll).InsertOne(ctx, doc); err != nil {
		return fmt.Errorf("mongo insert %s: %w", coll, err)
	}
	return nil
}

// upsert replaces the document matched by filter, inserting if absent.
func (s *Store) upsert(ctx context.Context, coll string, filter bson.M, v any) error {
	doc, err := toDoc(v)
	if err != nil {
		return err
	}
	opts := options.Replace().SetUpsert(true)
	if _, err := s.db.Collection(coll).ReplaceOne(ctx, filter, doc, opts); err != nil {
		return fmt.Errorf("mongo upsert %s: %w", coll, err)
	}
	return nil
}

func (s *Store) findOne(ctx context.Context, coll string, filter bson.M, out any) error {
	var m bson.M
	err := s.db.Collection(coll).FindOne(ctx, filter).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("mongo: document not found in %s", coll)
	}
	if err != nil {
		return fmt.Errorf("mongo find one %s: %w", coll, err)
	}
	return fromDoc(m, out)
}

func (s *Store) findAll(ctx context.Context, coll string, filter bson.M, sort bson.D, limit int) ([]bson.M, error) {
	opts := options.Find()
	if sort != nil {
		opts.SetSort(sort)
	}
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cur, err := s.db.Collection(coll).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("mongo find %s: %w", coll, err)
	}
	defer cur.Close(ctx)
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("mongo cursor %s: %w", coll, err)
	}
	return docs, nil
}

// decodeAll converts a slice of BSON documents into a typed slice.
func decodeAll[T any](docs []bson.M) []*T {
	out := make([]*T, 0, len(docs))
	for _, d := range docs {
		var v T
		if fromDoc(d, &v) == nil {
			out = append(out, &v)
		}
	}
	return out
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	return s.insert(ctx, collSessions, sess)
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	var sess storage.Session
	if err := s.findOne(ctx, collSessions, bson.M{"id": id}, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	return s.upsert(ctx, collSessions, bson.M{"id": sess.ID}, sess)
}

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	filter := bson.M{}
	if agentID != "" {
		filter["agent_id"] = agentID
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	if offset > 0 {
		opts.SetSkip(int64(offset))
	}
	cur, err := s.db.Collection(collSessions).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("mongo list sessions: %w", err)
	}
	defer cur.Close(ctx)
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("mongo list sessions cursor: %w", err)
	}
	return decodeAll[storage.Session](docs), nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	return s.upsert(ctx, collMemory, bson.M{"id": m.ID}, m)
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	var m storage.MemoryRecord
	if err := s.findOne(ctx, collMemory, bson.M{"agent_id": agentID, "key": key}, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	docs, err := s.findAll(ctx, collMemory, bson.M{"agent_id": agentID, "kind": kind}, nil, 0)
	if err != nil {
		return nil, err
	}
	return decodeAll[storage.MemoryRecord](docs), nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	if _, err := s.db.Collection(collMemory).DeleteOne(ctx, bson.M{"id": id}); err != nil {
		return fmt.Errorf("mongo delete memory: %w", err)
	}
	return nil
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	return s.insert(ctx, collAuditLogs, log)
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	if offset > 0 {
		opts.SetSkip(int64(offset))
	}
	cur, err := s.db.Collection(collAuditLogs).Find(ctx, bson.M{"session_id": sessionID}, opts)
	if err != nil {
		return nil, fmt.Errorf("mongo list audit logs: %w", err)
	}
	defer cur.Close(ctx)
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("mongo list audit logs cursor: %w", err)
	}
	return decodeAll[storage.AuditLog](docs), nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	return s.insert(ctx, collTraces, t)
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	var t storage.Trace
	if err := s.findOne(ctx, collTraces, bson.M{"id": id}, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	docs, err := s.findAll(ctx, collTraces, bson.M{"session_id": sessionID}, bson.D{{Key: "started_at", Value: 1}}, 0)
	if err != nil {
		return nil, err
	}
	return decodeAll[storage.Trace](docs), nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	return s.insert(ctx, collEvents, e)
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	filter := bson.M{"session_id": sessionID, "seq_num": bson.M{"$gt": afterSeq}}
	docs, err := s.findAll(ctx, collEvents, filter, bson.D{{Key: "seq_num", Value: 1}}, 0)
	if err != nil {
		return nil, err
	}
	return decodeAll[storage.Event](docs), nil
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	return s.insert(ctx, collCheckpoints, cp)
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	var cp storage.Checkpoint
	if err := s.findOne(ctx, collCheckpoints, bson.M{"id": id}, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	docs, err := s.findAll(ctx, collCheckpoints, bson.M{"session_id": sessionID}, bson.D{{Key: "seq_num", Value: -1}}, 1)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("mongo: no checkpoint found for session %q", sessionID)
	}
	cps := decodeAll[storage.Checkpoint](docs)
	if len(cps) == 0 {
		return nil, fmt.Errorf("mongo: failed to decode checkpoint for session %q", sessionID)
	}
	return cps[0], nil
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	docs, err := s.findAll(ctx, collCheckpoints, bson.M{"session_id": sessionID}, bson.D{{Key: "seq_num", Value: 1}}, 0)
	if err != nil {
		return nil, err
	}
	return decodeAll[storage.Checkpoint](docs), nil
}

// --- Lifecycle ---

// Migrate ensures indexes used by list/get queries exist. Collections are
// created lazily by MongoDB on first write.
func (s *Store) Migrate(ctx context.Context) error {
	indexes := map[string][]mongo.IndexModel{
		collSessions:    {{Keys: bson.D{{Key: "id", Value: 1}}}, {Keys: bson.D{{Key: "agent_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		collMemory:      {{Keys: bson.D{{Key: "id", Value: 1}}}, {Keys: bson.D{{Key: "agent_id", Value: 1}, {Key: "kind", Value: 1}}}},
		collAuditLogs:   {{Keys: bson.D{{Key: "session_id", Value: 1}, {Key: "created_at", Value: -1}}}},
		collTraces:      {{Keys: bson.D{{Key: "id", Value: 1}}}, {Keys: bson.D{{Key: "session_id", Value: 1}}}},
		collEvents:      {{Keys: bson.D{{Key: "session_id", Value: 1}, {Key: "seq_num", Value: 1}}}},
		collCheckpoints: {{Keys: bson.D{{Key: "id", Value: 1}}}, {Keys: bson.D{{Key: "session_id", Value: 1}, {Key: "seq_num", Value: -1}}}},
	}
	for coll, models := range indexes {
		if _, err := s.db.Collection(coll).Indexes().CreateMany(ctx, models); err != nil {
			return fmt.Errorf("mongo create indexes on %s: %w", coll, err)
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s.client != nil {
		if err := s.client.Disconnect(context.Background()); err != nil {
			return fmt.Errorf("mongo close: %w", err)
		}
	}
	return nil
}

// Ensure Store implements storage.Storage at compile time.
var _ storage.Storage = (*Store)(nil)
