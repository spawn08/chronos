package sqlite

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// newMigratedStore returns an in-memory store with the schema applied.
func newMigratedStore(t *testing.T) *Store {
	t.Helper()
	st, err := New(":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return st
}

// P0-003: GetLatestCheckpoint must order by seq_num, not wall-clock, so that
// same-tick timestamps still resolve deterministically to the highest seq.
func TestGetLatestCheckpoint_OrdersBySeqNum(t *testing.T) {
	ctx := context.Background()
	sameTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		seqs    []int64 // insertion order
		wantSeq int64
	}{
		{"ascending", []int64{0, 1, 2}, 2},
		{"descending insertion", []int64{5, 4, 3}, 5},
		{"unordered", []int64{2, 0, 3, 1}, 3},
		{"single", []int64{7}, 7},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newMigratedStore(t)
			sess := "sess-" + tc.name
			for _, seq := range tc.seqs {
				cp := &storage.Checkpoint{
					ID:        idFor(sess, seq),
					SessionID: sess,
					RunID:     "run-1",
					NodeID:    nodeFor(seq),
					State:     map[string]any{"seq": seq},
					SeqNum:    seq,
					// All rows share the same timestamp on purpose.
					CreatedAt: sameTime,
				}
				if err := st.SaveCheckpoint(ctx, cp); err != nil {
					t.Fatalf("SaveCheckpoint(seq=%d): %v", seq, err)
				}
			}
			got, err := st.GetLatestCheckpoint(ctx, sess)
			if err != nil {
				t.Fatalf("GetLatestCheckpoint: %v", err)
			}
			if got.SeqNum != tc.wantSeq {
				t.Errorf("latest seq_num = %d, want %d", got.SeqNum, tc.wantSeq)
			}
		})
	}
}

// P0-004: SaveCheckpoint is an idempotent upsert keyed by id — resaving the same
// id must not error on the primary key and must overwrite the row.
func TestSaveCheckpoint_IdempotentUpsert(t *testing.T) {
	ctx := context.Background()
	st := newMigratedStore(t)

	cp := &storage.Checkpoint{
		ID:        "cp-1",
		SessionID: "s1",
		RunID:     "run-1",
		NodeID:    "node-a",
		State:     map[string]any{"v": float64(1)},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	if err := st.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Resave the same id with a mutated state/node (simulating replay/resume).
	cp.NodeID = "node-b"
	cp.State = map[string]any{"v": float64(2)}
	if err := st.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatalf("idempotent resave errored: %v", err)
	}

	got, err := st.GetCheckpoint(ctx, "cp-1")
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if got.NodeID != "node-b" {
		t.Errorf("NodeID = %q, want node-b (upsert should overwrite)", got.NodeID)
	}
	cps, err := st.ListCheckpoints(ctx, "s1")
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Errorf("checkpoint count = %d, want 1 (no duplicate row)", len(cps))
	}
}

// P0-004: the UNIQUE(session_id, seq_num) index prevents two distinct rows from
// claiming the same ledger slot.
func TestCheckpoints_UniqueSessionSeq(t *testing.T) {
	ctx := context.Background()
	st := newMigratedStore(t)

	if err := st.SaveCheckpoint(ctx, &storage.Checkpoint{
		ID: "cp-a", SessionID: "s1", RunID: "r", NodeID: "n", SeqNum: 1, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("save cp-a: %v", err)
	}

	// A plain INSERT of a *different* id at the same (session, seq) must violate
	// the unique index.
	_, err := st.db.ExecContext(ctx,
		`INSERT INTO checkpoints (id, session_id, run_id, node_id, state, seq_num, created_at) VALUES (?,?,?,?,?,?,?)`,
		"cp-b", "s1", "r", "n", "{}", 1, time.Now(),
	)
	if err == nil {
		t.Fatal("expected UNIQUE(session_id, seq_num) violation, got nil")
		return
	}
}

// P0-004 regression (CRITICAL-Q01): re-running on an already-used session must
// upsert each (session, seq) slot, not collide with uq_checkpoints_session_seq.
// The runner derives the checkpoint id from (session, seq), so a second run —
// which restarts seq at 0 — reuses the same ids and upserts. The previous
// run-scoped id scheme minted new ids at existing slots, which hard-errored on
// Postgres and silently dropped the prior row on SQLite's INSERT OR REPLACE.
func TestCheckpoints_RerunSameSession_Upserts(t *testing.T) {
	ctx := context.Background()
	st := newMigratedStore(t)

	// id mirrors runner.commit: cp_<session>_<seq>.
	id := func(session string, seq int64) string {
		return "cp_" + session + "_" + strconv.FormatInt(seq, 10)
	}

	save := func(runID, nodeID string) {
		for seq := int64(0); seq < 3; seq++ {
			if err := st.SaveCheckpoint(ctx, &storage.Checkpoint{
				ID: id("s1", seq), SessionID: "s1", RunID: runID, NodeID: nodeID,
				State: map[string]any{"run": runID}, SeqNum: seq, CreatedAt: time.Now(),
			}); err != nil {
				t.Fatalf("%s seq %d: %v", runID, seq, err)
			}
		}
	}

	save("run1", "a")
	// Second run on the SAME session: same ids, must upsert (not collide).
	save("run2", "b")

	cps, err := st.ListCheckpoints(ctx, "s1")
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 3 {
		t.Fatalf("checkpoint count = %d, want 3 (one row per seq, upserted)", len(cps))
	}
	for _, cp := range cps {
		if cp.RunID != "run2" {
			t.Errorf("seq %d run_id = %q, want run2 (latest run overwrites in place)", cp.SeqNum, cp.RunID)
		}
	}
}

// P0-004: AppendEvent is idempotent — re-appending the same event id is a no-op
// rather than a primary-key error, so replay does not gap or duplicate.
func TestAppendEvent_Idempotent(t *testing.T) {
	ctx := context.Background()
	st := newMigratedStore(t)

	evt := &storage.Event{
		ID:        "evt-1",
		SessionID: "s1",
		SeqNum:    1,
		Type:      "node_executed",
		Payload:   map[string]any{"node": "a"},
		CreatedAt: time.Now(),
	}
	if err := st.AppendEvent(ctx, evt); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := st.AppendEvent(ctx, evt); err != nil {
		t.Fatalf("idempotent re-append errored: %v", err)
	}

	events, err := st.ListEvents(ctx, "s1", -1)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("event count = %d, want 1", len(events))
	}
}

// P0-004: SaveCheckpointAndEvent commits both writes atomically and is idempotent
// on replay.
func TestSaveCheckpointAndEvent_Atomic(t *testing.T) {
	ctx := context.Background()
	st := newMigratedStore(t)

	cp := &storage.Checkpoint{
		ID: "cp-1", SessionID: "s1", RunID: "r", NodeID: "n", State: map[string]any{}, SeqNum: 1, CreatedAt: time.Now(),
	}
	evt := &storage.Event{
		ID: "evt-1", SessionID: "s1", SeqNum: 1, Type: "node_executed", Payload: map[string]any{}, CreatedAt: time.Now(),
	}

	if err := st.SaveCheckpointAndEvent(ctx, cp, evt); err != nil {
		t.Fatalf("SaveCheckpointAndEvent: %v", err)
	}
	// Replay the same commit — must remain a no-op / upsert, never a PK error.
	if err := st.SaveCheckpointAndEvent(ctx, cp, evt); err != nil {
		t.Fatalf("idempotent replay errored: %v", err)
	}

	cps, err := st.ListCheckpoints(ctx, "s1")
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 1 {
		t.Errorf("checkpoint count = %d, want 1", len(cps))
	}
	events, err := st.ListEvents(ctx, "s1", -1)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("event count = %d, want 1", len(events))
	}
}

// TestSaveCheckpointAndEvent_NilEvent verifies the checkpoint is written even
// when no event accompanies it (the interrupt-pause path).
func TestSaveCheckpointAndEvent_NilEvent(t *testing.T) {
	ctx := context.Background()
	st := newMigratedStore(t)

	cp := &storage.Checkpoint{
		ID: "cp-1", SessionID: "s1", RunID: "r", NodeID: "pause", State: map[string]any{}, SeqNum: 3, CreatedAt: time.Now(),
	}
	if err := st.SaveCheckpointAndEvent(ctx, cp, nil); err != nil {
		t.Fatalf("SaveCheckpointAndEvent(nil evt): %v", err)
	}
	got, err := st.GetLatestCheckpoint(ctx, "s1")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if got.NodeID != "pause" || got.SeqNum != 3 {
		t.Errorf("got %q seq=%d, want pause seq=3", got.NodeID, got.SeqNum)
	}
}

func idFor(sess string, seq int64) string { return sess + "-cp-" + strconv.FormatInt(seq, 10) }
func nodeFor(seq int64) string            { return "node-" + strconv.FormatInt(seq, 10) }
