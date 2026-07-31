package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
)

// maxHistory bounds how many summaries MemReportStore retains per dataset so an
// ephemeral history does not grow without bound; the oldest are dropped.
const maxHistory = 200

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

// evalSessionPrefix namespaces eval-history checkpoints away from real run
// sessions.
const evalSessionPrefix = "__evals__"

// evalSession folds the context tenant into the checkpoint session id. The
// checkpoint uniqueness index is (session_id, seq_num) and is NOT itself
// tenant-scoped, so the tenant must be part of the session id to keep one
// tenant's history from colliding with another's on the same dataset name. Reads
// additionally filter by the tenant_id column, so this is belt-and-suspenders.
func evalSession(ctx context.Context, dataset string) string {
	return evalSessionPrefix + "/" + storage.TenantFromContext(ctx) + "/" + dataset
}

// StorageReportStore persists run history as append-only checkpoints (one per
// run) in a storage.Storage, keyed by a tenant-scoped per-dataset session id.
// Using checkpoints (rather than a single rewritten record) makes each run an
// independent row, so there is no read-modify-write over a growing blob.
//
// Concurrency: two runs of the same (tenant, dataset) that race compute the same
// next seq_num; the (session_id, seq_num) upsert means one overwrites the other's
// summary at that slot — a single lost run, never corruption of the whole
// history. Serialize concurrent runs of one dataset if that matters.
//
// It targets the tenant-scoped backends (sqlite, postgres). The experimental
// adapters that do not yet enforce tenant scoping (PLAN.md P2-002 follow-up)
// inherit that limitation here too.
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
	sess := evalSession(ctx, r.Dataset)
	var seq int64 = 1
	if latest, err := s.store.GetLatestCheckpoint(ctx, sess); err == nil && latest != nil {
		seq = latest.SeqNum + 1
	}
	state, err := summaryToState(summarize(r))
	if err != nil {
		return err
	}
	cp := &storage.Checkpoint{
		ID:        sess + "/" + strconv.FormatInt(seq, 10),
		SessionID: sess,
		NodeID:    "eval",
		State:     state,
		SeqNum:    seq,
		CreatedAt: time.Now(),
	}
	if err := s.store.SaveCheckpoint(ctx, cp); err != nil {
		return fmt.Errorf("save report: %w", err)
	}
	return nil
}

func (s *StorageReportStore) History(ctx context.Context, dataset string) ([]ReportSummary, error) {
	cps, err := s.store.ListCheckpoints(ctx, evalSession(ctx, dataset))
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	out := make([]ReportSummary, 0, len(cps))
	for _, cp := range cps {
		sum, convErr := stateToSummary(cp.State)
		if convErr != nil {
			return nil, convErr
		}
		out = append(out, sum)
	}
	return out, nil
}

// summaryToState renders a summary as a checkpoint state map via a JSON round-trip.
func summaryToState(sum ReportSummary) (map[string]any, error) {
	data, err := json.Marshal(sum)
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("encode summary state: %w", err)
	}
	return m, nil
}

// stateToSummary reconstructs a summary from a checkpoint state map.
func stateToSummary(state map[string]any) (ReportSummary, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return ReportSummary{}, fmt.Errorf("encode history state: %w", err)
	}
	var sum ReportSummary
	if err := json.Unmarshal(data, &sum); err != nil {
		return ReportSummary{}, fmt.Errorf("decode history summary: %w", err)
	}
	return sum, nil
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
