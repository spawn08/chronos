package mongo

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/spawn08/chronos/storage"
)

func TestToDoc_UsesJSONFieldNames(t *testing.T) {
	sess := &storage.Session{
		ID:        "s1",
		AgentID:   "a1",
		Status:    "running",
		Metadata:  map[string]any{"k": "v"},
		CreatedAt: time.Unix(1000, 0).UTC(),
	}
	doc, err := toDoc(sess)
	if err != nil {
		t.Fatalf("toDoc: %v", err)
	}
	// json tags must survive into the document (not Go field names).
	if _, ok := doc["agent_id"]; !ok {
		t.Errorf("expected key agent_id, got keys %v", keys(doc))
	}
	if doc["id"] != "s1" {
		t.Errorf("id = %v, want s1", doc["id"])
	}
	if _, ok := doc["AgentID"]; ok {
		t.Error("unexpected Go field name AgentID in document")
	}
}

func TestToDoc_Error(t *testing.T) {
	if _, err := toDoc(map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("expected marshal error for channel value")
	}
}

func TestFromDoc_RoundTrip(t *testing.T) {
	orig := &storage.Event{
		ID:        "e1",
		SessionID: "sess-1",
		SeqNum:    7,
		Type:      "node_enter",
		Payload:   map[string]any{"node": "start"},
		CreatedAt: time.Unix(2000, 0).UTC(),
	}
	doc, err := toDoc(orig)
	if err != nil {
		t.Fatalf("toDoc: %v", err)
	}
	// Simulate Mongo adding an _id which must be stripped on decode.
	doc["_id"] = "synthetic-object-id"

	var got storage.Event
	if err := fromDoc(doc, &got); err != nil {
		t.Fatalf("fromDoc: %v", err)
	}
	if got.ID != orig.ID || got.SeqNum != orig.SeqNum || got.Type != orig.Type {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("session_id not decoded: %q", got.SessionID)
	}
}

func TestDecodeAll(t *testing.T) {
	docs := []bson.M{
		{"id": "a", "session_id": "s", "seq_num": 1},
		{"id": "b", "session_id": "s", "seq_num": 2},
	}
	out := decodeAll[storage.Event](docs)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	if out[0].ID != "a" || out[1].SeqNum != 2 {
		t.Errorf("decoded wrong: %+v %+v", out[0], out[1])
	}
}

func keys(m bson.M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestCompileTimeInterface(t *testing.T) {
	var _ storage.Storage = (*Store)(nil)
}

// TestIntegration exercises the adapter against a live MongoDB. It is skipped
// unless MONGO_URI is set (e.g. mongodb://localhost:27017), since the official
// driver has no in-memory server.
func TestIntegration(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		t.Skip("set MONGO_URI to run mongo integration test")
	}
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	dbName := "chronos_test"
	store := NewWithClient(client, dbName)
	t.Cleanup(func() {
		_ = client.Database(dbName).Drop(ctx)
		_ = store.Close()
	})

	if err = store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Now()
	sess := &storage.Session{ID: "s1", AgentID: "a1", Status: "running", CreatedAt: now, UpdatedAt: now}
	if err = store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.AgentID != "a1" {
		t.Errorf("agent_id = %q", got.AgentID)
	}

	for i := 1; i <= 3; i++ {
		if err = store.AppendEvent(ctx, &storage.Event{ID: string(rune('a' + i)), SessionID: "s1", SeqNum: int64(i), Type: "t"}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	evs, err := store.ListEvents(ctx, "s1", 1)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 2 {
		t.Errorf("expected 2 events after seq 1, got %d", len(evs))
	}
}
