package dynamo

import (
	"context"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/spawn08/chronos/storage"
)

// fakeDynamo is an in-memory implementation of the DynamoDB api used by Store.
// It faithfully reproduces the pieces of the query surface Store relies on:
// GetItem/PutItem/DeleteItem by (pk), and Query over the owner-seq GSI with
// optional seq range and equality FilterExpression.
type fakeDynamo struct {
	items    map[string]map[string]types.AttributeValue // pk -> item
	created  bool
	putErr   error
	queryErr error
	getErr   error
}

func newFake() *fakeDynamo {
	return &fakeDynamo{items: map[string]map[string]types.AttributeValue{}}
}

func s(v types.AttributeValue) string {
	if av, ok := v.(*types.AttributeValueMemberS); ok {
		return av.Value
	}
	return ""
}

func n(v types.AttributeValue) int64 {
	if av, ok := v.(*types.AttributeValueMemberN); ok {
		i, _ := strconv.ParseInt(av.Value, 10, 64)
		return i
	}
	return 0
}

func (f *fakeDynamo) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	if f.putErr != nil {
		return nil, f.putErr
	}
	pk := s(in.Item["pk"])
	f.items[pk] = in.Item
	return &dynamodb.PutItemOutput{}, nil
}

func (f *fakeDynamo) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	pk := s(in.Key["pk"])
	item, ok := f.items[pk]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	return &dynamodb.GetItemOutput{Item: item}, nil
}

func (f *fakeDynamo) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	delete(f.items, s(in.Key["pk"]))
	return &dynamodb.DeleteItemOutput{}, nil
}

func (f *fakeDynamo) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	owner := s(in.ExpressionAttributeValues[":o"])
	var after *int64
	if av, ok := in.ExpressionAttributeValues[":after"]; ok {
		v := n(av)
		after = &v
	}

	// Resolve a single-equality filter (kind or mkey) if present.
	filterAttr, filterVal := "", ""
	if in.FilterExpression != nil {
		for placeholder, name := range in.ExpressionAttributeNames {
			if placeholder == "#o" || placeholder == "#sq" {
				continue
			}
			filterAttr = name
			// matching value placeholder is ":" + name-ish; find the non key/after value
			for vp, av := range in.ExpressionAttributeValues {
				if vp == ":o" || vp == ":after" {
					continue
				}
				filterVal = s(av)
			}
			_ = placeholder
		}
	}

	matched := make([]map[string]types.AttributeValue, 0, len(f.items))
	for _, item := range f.items {
		if s(item["owner"]) != owner {
			continue
		}
		if after != nil && n(item["seq"]) <= *after {
			continue
		}
		if filterAttr != "" && s(item[filterAttr]) != filterVal {
			continue
		}
		matched = append(matched, item)
	}

	forward := in.ScanIndexForward == nil || *in.ScanIndexForward
	sort.Slice(matched, func(i, j int) bool {
		if forward {
			return n(matched[i]["seq"]) < n(matched[j]["seq"])
		}
		return n(matched[i]["seq"]) > n(matched[j]["seq"])
	})
	if in.Limit != nil && int(*in.Limit) < len(matched) {
		matched = matched[:*in.Limit]
	}
	return &dynamodb.QueryOutput{Items: matched}, nil
}

func (f *fakeDynamo) CreateTable(_ context.Context, _ *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	if f.created {
		return nil, &types.ResourceInUseException{}
	}
	f.created = true
	return &dynamodb.CreateTableOutput{}, nil
}

func newTestStore(t *testing.T) (*Store, *fakeDynamo) {
	t.Helper()
	f := newFake()
	return NewWithClient(f, "chronos"), f
}

func TestKeyBuilders(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"sessionPK", sessionPK("s1"), "SESSION#s1"},
		{"memoryPK", memoryPK("m1"), "MEMORY#m1"},
		{"auditPK", auditPK("a1"), "AUDIT#a1"},
		{"tracePK", tracePK("t1"), "TRACE#t1"},
		{"eventPK", eventPK("e1"), "EVENT#e1"},
		{"checkpointPK", checkpointPK("c1"), "CHECKPOINT#c1"},
		{"sessionOwner", sessionOwner("a1"), "AGENT#a1"},
		{"memoryOwner", memoryOwner("a1"), "MEM#a1"},
		{"eventOwner", eventOwner("s1"), "EVENTS#s1"},
		{"checkpointOwner", checkpointOwner("s1"), "CKPT#s1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestApplyOffset(t *testing.T) {
	in := []int{1, 2, 3, 4}
	tests := []struct {
		offset int
		want   []int
	}{
		{0, []int{1, 2, 3, 4}},
		{2, []int{3, 4}},
		{4, []int{}},
		{10, []int{}},
	}
	for _, tt := range tests {
		got := applyOffset(in, tt.offset)
		if len(got) != len(tt.want) {
			t.Errorf("offset %d: len %d, want %d", tt.offset, len(got), len(tt.want))
		}
	}
}

func TestSessionRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	sess := &storage.Session{ID: "s1", AgentID: "a1", Status: "running", Metadata: map[string]any{"k": "v"}, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	got, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != "s1" || got.AgentID != "a1" || got.Status != "running" {
		t.Errorf("got %+v", got)
	}
	if got.Metadata["k"] != "v" {
		t.Errorf("metadata not round-tripped: %+v", got.Metadata)
	}

	sess.Status = "done"
	if err := store.UpdateSession(ctx, sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	got2, _ := store.GetSession(ctx, "s1")
	if got2.Status != "done" {
		t.Errorf("status = %q, want done", got2.Status)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	if _, err := store.GetSession(context.Background(), "missing"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestListSessions_OrderAndPagination(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 3; i++ {
		s := &storage.Session{ID: string(rune('a' + i)), AgentID: "a1", CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := store.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	all, err := store.ListSessions(ctx, "a1", 10, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(all) != 3 || all[0].ID != "c" {
		t.Fatalf("expected newest-first 3 sessions, got %+v", all)
	}
	page, err := store.ListSessions(ctx, "a1", 1, 1)
	if err != nil {
		t.Fatalf("ListSessions page: %v", err)
	}
	if len(page) != 1 || page[0].ID != "b" {
		t.Errorf("page = %+v, want [b]", page)
	}
}

func TestMemory_GetAndList(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	m1 := &storage.MemoryRecord{ID: "m1", AgentID: "a1", Kind: "long_term", Key: "name", Value: "Alice", CreatedAt: now}
	m2 := &storage.MemoryRecord{ID: "m2", AgentID: "a1", Kind: "short_term", Key: "mood", Value: "happy", CreatedAt: now.Add(time.Second)}
	for _, m := range []*storage.MemoryRecord{m1, m2} {
		if err := store.PutMemory(ctx, m); err != nil {
			t.Fatalf("PutMemory: %v", err)
		}
	}

	got, err := store.GetMemory(ctx, "a1", "name")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Value != "Alice" {
		t.Errorf("value = %v, want Alice", got.Value)
	}

	lt, err := store.ListMemory(ctx, "a1", "long_term")
	if err != nil {
		t.Fatalf("ListMemory: %v", err)
	}
	if len(lt) != 1 || lt[0].ID != "m1" {
		t.Errorf("long_term list = %+v", lt)
	}

	if err := store.DeleteMemory(ctx, "m1"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if _, err := store.GetMemory(ctx, "a1", "name"); err == nil {
		t.Error("expected not-found after delete")
	}
}

func TestAuditTraceRoundTrip(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	if err := store.AppendAuditLog(ctx, &storage.AuditLog{ID: "l1", SessionID: "s1", Action: "chat", CreatedAt: now}); err != nil {
		t.Fatalf("AppendAuditLog: %v", err)
	}
	logs, err := store.ListAuditLogs(ctx, "s1", 10, 0)
	if err != nil || len(logs) != 1 {
		t.Fatalf("ListAuditLogs: %v, %+v", err, logs)
	}

	if err = store.InsertTrace(ctx, &storage.Trace{ID: "t1", SessionID: "s1", Name: "n", StartedAt: now}); err != nil {
		t.Fatalf("InsertTrace: %v", err)
	}
	tr, err := store.GetTrace(ctx, "t1")
	if err != nil || tr.ID != "t1" {
		t.Fatalf("GetTrace: %v, %+v", err, tr)
	}
	traces, err := store.ListTraces(ctx, "s1")
	if err != nil || len(traces) != 1 {
		t.Fatalf("ListTraces: %v, %+v", err, traces)
	}
}

func TestEvents_OrderAndFilter(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		e := &storage.Event{ID: string(rune('a' + i)), SessionID: "s1", SeqNum: int64(i), Type: "t"}
		if err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
	all, err := store.ListEvents(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(all) != 3 || all[0].SeqNum != 1 || all[2].SeqNum != 3 {
		t.Fatalf("events not ascending: %+v", all)
	}
	after, _ := store.ListEvents(ctx, "s1", 1)
	if len(after) != 2 {
		t.Errorf("expected 2 events after seq 1, got %d", len(after))
	}
}

func TestCheckpoints(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for i := 1; i <= 2; i++ {
		cp := &storage.Checkpoint{ID: string(rune('a' + i)), SessionID: "s1", SeqNum: int64(i), State: map[string]any{"n": i}}
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint: %v", err)
		}
	}
	got, err := store.GetCheckpoint(ctx, "b")
	if err != nil || got.ID != "b" {
		t.Fatalf("GetCheckpoint: %v, %+v", err, got)
	}
	list, err := store.ListCheckpoints(ctx, "s1")
	if err != nil || len(list) != 2 {
		t.Fatalf("ListCheckpoints: %v, %+v", err, list)
	}
	latest, err := store.GetLatestCheckpoint(ctx, "s1")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if latest.SeqNum != 2 {
		t.Errorf("latest seq = %d, want 2", latest.SeqNum)
	}
}

func TestGetLatestCheckpoint_NotFound(t *testing.T) {
	store, _ := newTestStore(t)
	if _, err := store.GetLatestCheckpoint(context.Background(), "none"); err == nil {
		t.Fatal("expected error for missing checkpoint")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Second call must swallow ResourceInUseException.
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate (idempotent): %v", err)
	}
}

func TestCloseAndConstructor(t *testing.T) {
	store, err := New("http://localhost:8000", "chronos", "us-east-1", "key", "secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if store.tableName != "chronos" {
		t.Errorf("tableName = %q", store.tableName)
	}
	if err := store.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestCompileTimeInterface(t *testing.T) {
	var _ storage.Storage = (*Store)(nil)
}
