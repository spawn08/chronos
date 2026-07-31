package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
)

// ReportSummary is the compact, trend-friendly record of one eval run kept in
// history. It is what the gate compares across runs.
type ReportSummary struct {
	Dataset  string    `json:"dataset"`
	RanAt    time.Time `json:"ran_at"`
	AvgScore float64   `json:"avg_score"`
	PassRate float64   `json:"pass_rate"`
	Total    int       `json:"total"`
	Passed   int       `json:"passed"`
}

// summarize reduces a full report to its trend record.
func summarize(r *DatasetReport) ReportSummary {
	return ReportSummary{
		Dataset:  r.Dataset,
		RanAt:    r.RanAt,
		AvgScore: r.AvgScore,
		PassRate: r.PassRate,
		Total:    r.Total,
		Passed:   r.Passed,
	}
}

// ReportStore persists eval-run summaries so scores are queryable over time and a
// run can gate against its predecessor. Implementations scope history to the
// context tenant.
type ReportStore interface {
	// SaveReport appends r's summary to the dataset's history.
	SaveReport(ctx context.Context, r *DatasetReport) error
	// History returns the dataset's run summaries in chronological (oldest-first)
	// order; empty when there is no history yet.
	History(ctx context.Context, dataset string) ([]ReportSummary, error)
}

// BaselineFrom returns the most recent summary to gate against, or nil when the
// history is empty (the first run has no baseline).
func BaselineFrom(history []ReportSummary) *DatasetReport {
	if len(history) == 0 {
		return nil
	}
	last := history[len(history)-1]
	return &DatasetReport{
		Dataset:  last.Dataset,
		RanAt:    last.RanAt,
		AvgScore: last.AvgScore,
		PassRate: last.PassRate,
		Total:    last.Total,
		Passed:   last.Passed,
	}
}

// maxHistory bounds how many summaries are retained per dataset so a long-lived
// history record does not grow without bound; the oldest are dropped.
const maxHistory = 200

// evalsAgentID namespaces eval history under a reserved agent id in the memory
// store (the memory record is keyed by agent id + key, and scoped to the tenant).
const evalsAgentID = "__evals__"

func historyKey(dataset string) string { return "history/" + dataset }

// StorageReportStore persists history in the memory table of a storage.Storage as
// a single JSON record per (tenant, dataset), appended on each run. Tenant scoping
// is inherited from the storage adapter (the record carries the context tenant).
type StorageReportStore struct {
	store storage.Storage
}

// NewStorageReportStore builds a report store backed by store.
func NewStorageReportStore(store storage.Storage) *StorageReportStore {
	return &StorageReportStore{store: store}
}

func (s *StorageReportStore) SaveReport(ctx context.Context, r *DatasetReport) error {
	if r == nil {
		return fmt.Errorf("save report: nil report")
	}
	history, err := s.History(ctx, r.Dataset)
	if err != nil {
		return err
	}
	history = append(history, summarize(r))
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}
	data, err := json.Marshal(history)
	if err != nil {
		return fmt.Errorf("save report: marshal history: %w", err)
	}
	rec := &storage.MemoryRecord{
		ID:        evalsAgentID + "/" + historyKey(r.Dataset),
		AgentID:   evalsAgentID,
		Kind:      "eval_history",
		Key:       historyKey(r.Dataset),
		Value:     string(data),
		CreatedAt: time.Now(),
	}
	if err := s.store.PutMemory(ctx, rec); err != nil {
		return fmt.Errorf("save report: put memory: %w", err)
	}
	return nil
}

func (s *StorageReportStore) History(ctx context.Context, dataset string) ([]ReportSummary, error) {
	rec, err := s.store.GetMemory(ctx, evalsAgentID, historyKey(dataset))
	if err != nil {
		// No record yet (or a cross-tenant miss) means no history — a first run.
		return nil, nil
	}
	raw, ok := rec.Value.(string)
	if !ok {
		// Some adapters scan JSON columns into []byte.
		if b, okb := rec.Value.([]byte); okb {
			raw = string(b)
		} else {
			data, _ := json.Marshal(rec.Value)
			raw = string(data)
		}
	}
	if raw == "" {
		return nil, nil
	}
	var history []ReportSummary
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	return history, nil
}

// MemReportStore is an in-memory ReportStore for tests and ephemeral use. It
// partitions history by the context tenant, mirroring StorageReportStore.
type MemReportStore struct {
	mu      sync.Mutex
	history map[string][]ReportSummary // key: tenant + "\x00" + dataset
}

// NewMemReportStore builds an empty in-memory report store.
func NewMemReportStore() *MemReportStore {
	return &MemReportStore{history: make(map[string][]ReportSummary)}
}

func (m *MemReportStore) key(ctx context.Context, dataset string) string {
	return storage.TenantFromContext(ctx) + "\x00" + dataset
}

func (m *MemReportStore) SaveReport(ctx context.Context, r *DatasetReport) error {
	if r == nil {
		return fmt.Errorf("save report: nil report")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(ctx, r.Dataset)
	m.history[k] = append(m.history[k], summarize(r))
	if len(m.history[k]) > maxHistory {
		m.history[k] = m.history[k][len(m.history[k])-maxHistory:]
	}
	return nil
}

func (m *MemReportStore) History(ctx context.Context, dataset string) ([]ReportSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.history[m.key(ctx, dataset)]
	out := make([]ReportSummary, len(h))
	copy(out, h)
	return out, nil
}
