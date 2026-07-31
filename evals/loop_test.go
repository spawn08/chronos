package evals

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func TestCaptureFromSession(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if migErr := store.Migrate(ctx); migErr != nil {
		t.Fatal(migErr)
	}

	sid := "s1"
	if createErr := store.CreateSession(ctx, &storage.Session{ID: sid, AgentID: "a1", Status: "active"}); createErr != nil {
		t.Fatal(createErr)
	}
	// Two user→assistant pairs, plus a tool-call-only assistant turn (empty text)
	// that must be skipped.
	msgs := []struct {
		role, content string
	}{
		{"user", "capital of France?"},
		{"assistant", "Paris"},
		{"user", "and Japan?"},
		{"assistant", ""}, // tool-call-only, no golden text
		{"assistant", "Tokyo"},
	}
	for i, m := range msgs {
		if appendErr := store.AppendEvent(ctx, &storage.Event{
			ID:        string(rune('a' + i)),
			SessionID: sid,
			SeqNum:    int64(i + 1),
			Type:      "chat_message",
			Payload:   map[string]any{"role": m.role, "content": m.content},
		}); appendErr != nil {
			t.Fatal(appendErr)
		}
	}

	ds, err := CaptureFromSession(ctx, store, sid, "geo")
	if err != nil {
		t.Fatalf("CaptureFromSession: %v", err)
	}
	if ds.Name != "geo" || len(ds.Cases) != 2 {
		t.Fatalf("got name=%q cases=%d, want geo/2", ds.Name, len(ds.Cases))
	}
	if ds.Cases[0].Input != "capital of France?" || ds.Cases[0].Expected != "Paris" {
		t.Errorf("case0 = %+v", ds.Cases[0])
	}
	if ds.Cases[1].Expected != "Tokyo" {
		t.Errorf("case1 expected = %q, want Tokyo", ds.Cases[1].Expected)
	}

	// Round-trip through JSON.
	data, err := MarshalDataset(ds)
	if err != nil {
		t.Fatal(err)
	}
	back, err := LoadDataset(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Cases) != 2 {
		t.Errorf("round-trip cases = %d, want 2", len(back.Cases))
	}
}

func TestCaptureFromSession_SkipsMalformedPayload(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if migErr := store.Migrate(ctx); migErr != nil {
		t.Fatal(migErr)
	}
	sid := "s-malformed"
	if createErr := store.CreateSession(ctx, &storage.Session{ID: sid, AgentID: "a1", Status: "active"}); createErr != nil {
		t.Fatal(createErr)
	}
	events := []*storage.Event{
		// Non-map payload — must be skipped, not panic.
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: "not-a-map"},
		// Non-chat event type — ignored.
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "model_call", Payload: map[string]any{"role": "user", "content": "x"}},
		// A valid pair.
		{ID: "e3", SessionID: sid, SeqNum: 3, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "q"}},
		{ID: "e4", SessionID: sid, SeqNum: 4, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "a"}},
	}
	for _, e := range events {
		if appendErr := store.AppendEvent(ctx, e); appendErr != nil {
			t.Fatal(appendErr)
		}
	}

	ds, err := CaptureFromSession(ctx, store, sid, "d")
	if err != nil {
		t.Fatalf("CaptureFromSession: %v", err)
	}
	if len(ds.Cases) != 1 || ds.Cases[0].Input != "q" || ds.Cases[0].Expected != "a" {
		t.Fatalf("expected exactly the valid pair, got %+v", ds.Cases)
	}
}

func TestDatasetRunner(t *testing.T) {
	ds := &Dataset{Name: "d", Cases: []DatasetCase{
		{Input: "say paris", Expected: "Paris"},
		{Input: "say tokyo", Expected: "Tokyo"},
	}}

	tests := []struct {
		name       string
		target     Target
		wantAvg    float64
		wantPassed int
	}{
		{
			name: "all correct",
			target: func(_ context.Context, in string) (string, error) {
				return map[string]string{"say paris": "Paris", "say tokyo": "Tokyo"}[in], nil
			},
			wantAvg:    1.0,
			wantPassed: 2,
		},
		{
			name: "one wrong",
			target: func(_ context.Context, in string) (string, error) {
				return map[string]string{"say paris": "Paris", "say tokyo": "Berlin"}[in], nil
			},
			wantAvg:    0.5,
			wantPassed: 1,
		},
		{
			name:       "target error",
			target:     func(_ context.Context, _ string) (string, error) { return "", errors.New("boom") },
			wantAvg:    0.0,
			wantPassed: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DatasetRunner{Target: tt.target, Evaluators: []Eval{&ExactMatchEval{EvalName: "exact"}}}
			report, err := r.Run(context.Background(), ds)
			if err != nil {
				t.Fatal(err)
			}
			if report.AvgScore != tt.wantAvg {
				t.Errorf("AvgScore = %.2f, want %.2f", report.AvgScore, tt.wantAvg)
			}
			if report.Passed != tt.wantPassed {
				t.Errorf("Passed = %d, want %d", report.Passed, tt.wantPassed)
			}
		})
	}
}

func TestDatasetRunner_Misconfigured(t *testing.T) {
	noop := func(context.Context, string) (string, error) { return "", nil }
	if _, err := (&DatasetRunner{}).Run(context.Background(), &Dataset{}); err == nil {
		t.Error("expected error with nil target")
	}
	if _, err := (&DatasetRunner{Target: noop}).Run(context.Background(), &Dataset{}); err == nil {
		t.Error("expected error with no evaluators")
	}
	if _, err := (&DatasetRunner{Target: noop, Evaluators: []Eval{&ExactMatchEval{}}}).Run(context.Background(), nil); err == nil {
		t.Error("expected error with nil dataset")
	}
}

func TestGate(t *testing.T) {
	base := &DatasetReport{AvgScore: 0.9, PassRate: 0.9}
	tests := []struct {
		name     string
		report   *DatasetReport
		baseline *DatasetReport
		cfg      GateConfig
		want     bool
	}{
		{"passes min score", &DatasetReport{AvgScore: 0.95, PassRate: 1}, nil, GateConfig{MinAvgScore: 0.8}, true},
		{"fails min score", &DatasetReport{AvgScore: 0.6, PassRate: 1}, nil, GateConfig{MinAvgScore: 0.8}, false},
		{"fails min pass rate", &DatasetReport{AvgScore: 0.9, PassRate: 0.5}, nil, GateConfig{MinPassRate: 0.8}, false},
		{"passes no regression", &DatasetReport{AvgScore: 0.88}, base, GateConfig{MaxRegression: 0.05}, true},
		{"fails regression", &DatasetReport{AvgScore: 0.7}, base, GateConfig{MaxRegression: 0.05}, false},
		{"regression ignored without baseline", &DatasetReport{AvgScore: 0.1}, nil, GateConfig{MaxRegression: 0.05}, true},
		{"empty config always passes", &DatasetReport{AvgScore: 0.0}, nil, GateConfig{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Gate(tt.report, tt.baseline, tt.cfg)
			if got.Passed != tt.want {
				t.Errorf("Gate passed = %v, want %v (%s)", got.Passed, tt.want, got.String())
			}
		})
	}
}

func TestReportStore_HistoryAndBaseline(t *testing.T) {
	stores := map[string]ReportStore{"mem": NewMemReportStore()}
	// Also exercise the storage-backed store.
	ctx := context.Background()
	st, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	stores["storage"] = NewStorageReportStore(st)

	for name, rs := range stores {
		t.Run(name, func(t *testing.T) {
			if h, err := rs.History(ctx, "d"); err != nil || len(h) != 0 {
				t.Fatalf("empty history: got %v err %v", h, err)
			}
			if BaselineFrom(nil) != nil {
				t.Error("BaselineFrom(nil) should be nil")
			}

			_ = rs.SaveReport(ctx, &DatasetReport{Dataset: "d", AvgScore: 0.8, PassRate: 0.8, Total: 5, Passed: 4})
			_ = rs.SaveReport(ctx, &DatasetReport{Dataset: "d", AvgScore: 0.9, PassRate: 1, Total: 5, Passed: 5})

			h, err := rs.History(ctx, "d")
			if err != nil {
				t.Fatal(err)
			}
			if len(h) != 2 {
				t.Fatalf("history len = %d, want 2", len(h))
			}
			base := BaselineFrom(h)
			if base == nil || base.AvgScore != 0.9 {
				t.Errorf("baseline = %+v, want most recent (0.9)", base)
			}
		})
	}
}

func TestReportStore_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if migErr := st.Migrate(ctx); migErr != nil {
		t.Fatal(migErr)
	}

	// Both implementations of the interface must isolate history by tenant.
	stores := map[string]ReportStore{
		"mem":     NewMemReportStore(),
		"storage": NewStorageReportStore(st),
	}
	for name, rs := range stores {
		t.Run(name, func(t *testing.T) {
			ctxA := storage.WithTenant(context.Background(), "tenant-a")
			ctxB := storage.WithTenant(context.Background(), "tenant-b")

			// Both tenants use the SAME dataset name — the collision case.
			if err := rs.SaveReport(ctxA, &DatasetReport{Dataset: "shared", AvgScore: 0.11}); err != nil {
				t.Fatal(err)
			}
			if err := rs.SaveReport(ctxB, &DatasetReport{Dataset: "shared", AvgScore: 0.99}); err != nil {
				t.Fatal(err)
			}

			hA, err := rs.History(ctxA, "shared")
			if err != nil {
				t.Fatal(err)
			}
			if len(hA) != 1 || hA[0].AvgScore != 0.11 {
				t.Fatalf("tenant-a history = %+v, want exactly its own [0.11]", hA)
			}
			hB, err := rs.History(ctxB, "shared")
			if err != nil {
				t.Fatal(err)
			}
			if len(hB) != 1 || hB[0].AvgScore != 0.99 {
				t.Fatalf("tenant-b history = %+v, want exactly its own [0.99]", hB)
			}
		})
	}
}
