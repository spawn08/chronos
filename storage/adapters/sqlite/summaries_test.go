package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/spawn08/chronos/storage"
)

func appendSummaryEvent(t *testing.T, store *Store, ctx context.Context, session string, seq int64, kind string, payload any) {
	t.Helper()
	err := store.AppendEvent(ctx, &storage.Event{
		ID:        fmt.Sprintf("%s-%s-%d", storage.TenantFromContext(ctx), session, seq),
		SessionID: session, SeqNum: seq, Type: kind, Payload: payload, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadSessionSummaryIsolationAndLatestValid(t *testing.T) {
	store := newTestStore(t)
	ctx := storage.WithTenant(context.Background(), "a")
	other := storage.WithTenant(context.Background(), "b")
	appendSummaryEvent(t, store, ctx, "shared", 1, "chat_summary", map[string]any{"summary": "old"})
	appendSummaryEvent(t, store, ctx, "shared", 4, "chat_summary", map[string]any{"summary": " \t\u2003latest\u00a0\n ", "version": 1, "covered_seq": 2})
	appendSummaryEvent(t, store, ctx, "shared", 3, "chat_summary", map[string]any{"summary": "inserted later, lower sequence"})
	for i, payload := range []any{map[string]any{"summary": " \n\t\u2003\u00a0"}, map[string]any{"summary": 42}, []string{"invalid"}, "invalid", nil} {
		appendSummaryEvent(t, store, ctx, "shared", int64(5+i), "chat_summary", payload)
	}
	// An invalid raw JSON payload must also be ignored without decoding errors.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO events (id, tenant_id, session_id, seq_num, type, payload, created_at) VALUES ('invalid', 'a', 'shared', 99, 'chat_summary', '{', ?)`, time.Now()); err != nil {
		t.Fatal(err)
	}
	appendSummaryEvent(t, store, other, "shared", 100, "chat_summary", map[string]any{"summary": "other tenant"})
	appendSummaryEvent(t, store, ctx, "different", 101, "chat_summary", map[string]any{"summary": "other session"})
	for _, tt := range []struct {
		ctx      context.Context
		id, want string
		seq      int64
	}{
		{ctx, "shared", "latest", 4},
		{other, "shared", "other tenant", 100},
		{ctx, "different", "other session", 101},
		{context.Background(), "shared", "", 0},
		{ctx, "missing", "", 0},
	} {
		text, source, seq, clipped, err := store.ReadSessionSummary(tt.ctx, tt.id, 2000)
		if err != nil || text != tt.want || seq != tt.seq || clipped || text != "" && source != "chat_summary" {
			t.Fatalf("tenant=%s session=%s: %q %q %d %t %v", storage.TenantFromContext(tt.ctx), tt.id, text, source, seq, clipped, err)
		}
	}
}

func TestReadSessionSummaryRecentFallback(t *testing.T) {
	store := newTestStore(t)
	ctx := storage.WithTenant(context.Background(), "a")
	for i := 1; i <= 40; i++ {
		appendSummaryEvent(t, store, ctx, "s", int64(i), "chat_message", map[string]any{"role": "user", "content": fmt.Sprintf("message %02d", i)})
	}
	appendSummaryEvent(t, store, ctx, "s", 41, "chat_message", map[string]any{"role": "tool", "content": "excluded"})
	appendSummaryEvent(t, store, ctx, "s", 42, "chat_message", map[string]any{"role": "assistant", "content": "ééé"})
	appendSummaryEvent(t, store, ctx, "s", 43, "chat_message", map[string]any{"role": "user", "content": []string{"invalid"}})
	appendSummaryEvent(t, store, storage.WithTenant(ctx, "b"), "s", 44, "chat_message", map[string]any{"role": "assistant", "content": "other tenant"})
	appendSummaryEvent(t, store, ctx, "other", 45, "chat_message", map[string]any{"role": "assistant", "content": "other session"})
	text, source, seq, clipped, err := store.ReadSessionSummary(ctx, "s", 2000)
	if err != nil || source != "chat_message" || seq != 42 || !clipped || len(strings.Split(text, "\n")) != 32 || !strings.HasPrefix(text, "user: message 10\n") || !strings.HasSuffix(text, "assistant: ééé") {
		t.Fatalf("recent fallback: %q %q %d %t %v", text, source, seq, clipped, err)
	}
	for _, budget := range []int{1, 11, 12, 14, 17, 32} {
		text, source, seq, clipped, err := store.ReadSessionSummary(ctx, "s", budget)
		if err != nil || len(text) > budget || !utf8.ValidString(text) || source != "chat_message" || seq != 42 || !clipped || strings.Contains(text, "other") {
			t.Fatalf("budget=%d: %q %q %d %t %v", budget, text, source, seq, clipped, err)
		}
	}
}

func TestReadSessionSummaryHugeHistoryProjection(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	// Thousands of large non-chat payloads must never be projected or decoded.
	_, err := store.db.ExecContext(ctx, `WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<2000)
		INSERT INTO events (id, session_id, seq_num, type, payload, created_at)
		SELECT 'history-' || x, 's', x, 'tool_result', json_object('output', ?), ? FROM n`, strings.Repeat("x", 16*1024), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("é", 1024*1024)
	for _, kind := range []string{"chat_summary", "chat_message"} {
		id := kind
		appendSummaryEvent(t, store, ctx, id, 2001, kind, map[string]any{"summary": large, "role": "assistant", "content": large, "unused": large})
		text, source, seq, clipped, err := store.ReadSessionSummary(ctx, id, 31)
		if err != nil || len(text) > 31 || !utf8.ValidString(text) || source != kind || seq != 2001 || !clipped {
			t.Fatalf("large %s: bytes=%d source=%s seq=%d clipped=%t err=%v", kind, len(text), source, seq, clipped, err)
		}
		// Inspect the actual production SQL projection before Go truncation: only
		// bounded text/scalars may cross the database boundary.
		query := readSummaryQuery
		args := []any{31, 31, storage.DefaultTenant, id}
		if kind == "chat_message" {
			query = readSummaryMessagesQuery
			args = append(args, summaryRecentMessages+1)
		}
		rows, err := store.db.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		if !rows.Next() {
			t.Fatal("missing projection")
		}
		var projected []byte
		if kind == "chat_summary" {
			err = rows.Scan(&seq, &projected, &clipped)
		} else {
			var role string
			err = rows.Scan(&seq, &role, &projected, &clipped)
		}
		if err != nil || len(projected) != 31 || !clipped {
			t.Fatalf("unbounded SQL projection: bytes=%d clipped=%t err=%v", len(projected), clipped, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		planRows, err := store.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		indexed := false
		for planRows.Next() {
			var id, parent, unused int
			var detail string
			if err := planRows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			indexed = indexed || strings.Contains(detail, "idx_events_tenant_session_seq (tenant_id=? AND session_id=?)")
		}
		if err := planRows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := planRows.Close(); err != nil {
			t.Fatal(err)
		}
		if !indexed {
			t.Fatalf("%s query did not use tenant/session/sequence index", kind)
		}
	}
	appendSummaryEvent(t, store, ctx, "s", 2001, "chat_message", map[string]any{"role": "user", "content": "recent question"})
	text, _, seq, clipped, err := store.ReadSessionSummary(ctx, "s", 2000)
	if err != nil || text != "user: recent question" || seq != 2001 || clipped {
		t.Fatalf("huge history fallback: %q, %d, %t, %v", text, seq, clipped, err)
	}
	appendSummaryEvent(t, store, ctx, "s", 2002, "chat_summary", map[string]any{"summary": "small useful summary"})
	text, _, _, _, err = store.ReadSessionSummary(ctx, "s", 2000)
	if err != nil || text != "small useful summary" {
		t.Fatalf("huge history: %q, %v", text, err)
	}
}

func TestReadSessionSummaryDisabledAndCanceled(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, budget := range []int{0, -1} {
		text, source, seq, clipped, err := store.ReadSessionSummary(ctx, "s", budget)
		if err != nil || text != "" || source != "" || seq != 0 || clipped {
			t.Fatalf("disabled read = %q %q %d %t %v", text, source, seq, clipped, err)
		}
	}
	if _, _, _, _, err := store.ReadSessionSummary(ctx, "s", 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
}
