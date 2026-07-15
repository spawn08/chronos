package auth

import (
	"context"
	"sync"
	"time"
)

// Quota defines rate and budget limits for a subject (an API key or a tenant).
// A zero value in any field means that dimension is unlimited.
type Quota struct {
	// RequestsPerMinute caps requests within a rolling one-minute window.
	RequestsPerMinute int `json:"requests_per_minute,omitempty"`
	// TokensPerDay caps model tokens consumed within a rolling 24h window.
	TokensPerDay int64 `json:"tokens_per_day,omitempty"`
	// MaxCostUSD caps cumulative spend (USD) within a rolling 24h window.
	MaxCostUSD float64 `json:"max_cost_usd,omitempty"`
}

// IsZero reports whether the quota imposes no limits.
func (q Quota) IsZero() bool {
	return q.RequestsPerMinute == 0 && q.TokensPerDay == 0 && q.MaxCostUSD == 0
}

// Usage is a point-in-time snapshot of consumption for a subject.
type Usage struct {
	Requests int     `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

// QuotaStore enforces and records per-subject usage. It is the extension point
// the control plane wires to attach token/cost budgets to requests:
//   - Allow is called at request admission time.
//   - AddUsage is the hook invoked after model calls to record token/cost spend.
type QuotaStore interface {
	// Allow records one request against subject and reports whether it is within
	// the given quota (rate + budget). A subject over any limit returns false.
	Allow(ctx context.Context, subject string, q Quota) (bool, error)
	// AddUsage records token and cost consumption for a subject.
	AddUsage(ctx context.Context, subject string, tokens int64, costUSD float64) error
	// Usage returns the current usage snapshot for a subject.
	Usage(ctx context.Context, subject string) (Usage, error)
}

// MemoryQuotaStore is an in-memory QuotaStore with a rolling one-minute request
// window and a rolling 24h token/cost window. It is safe for concurrent use.
type MemoryQuotaStore struct {
	mu    sync.Mutex
	state map[string]*quotaState
	now   func() time.Time
}

type quotaState struct {
	reqWindowStart time.Time
	reqCount       int

	dayWindowStart time.Time
	tokens         int64
	costUSD        float64
}

// NewMemoryQuotaStore returns an empty in-memory quota store.
func NewMemoryQuotaStore() *MemoryQuotaStore {
	return &MemoryQuotaStore{
		state: make(map[string]*quotaState),
		now:   time.Now,
	}
}

func (s *MemoryQuotaStore) stateFor(subject string, now time.Time) *quotaState {
	st, ok := s.state[subject]
	if !ok {
		st = &quotaState{reqWindowStart: now, dayWindowStart: now}
		s.state[subject] = st
	}
	if now.Sub(st.reqWindowStart) >= time.Minute {
		st.reqWindowStart = now
		st.reqCount = 0
	}
	if now.Sub(st.dayWindowStart) >= 24*time.Hour {
		st.dayWindowStart = now
		st.tokens = 0
		st.costUSD = 0
	}
	return st
}

// Allow admits one request against the subject's rolling windows and reports
// whether all configured limits are satisfied. When a limit is exceeded, the
// request is not counted so a rejected caller does not deepen its own deficit.
func (s *MemoryQuotaStore) Allow(_ context.Context, subject string, q Quota) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	st := s.stateFor(subject, now)

	if q.TokensPerDay > 0 && st.tokens >= q.TokensPerDay {
		return false, nil
	}
	if q.MaxCostUSD > 0 && st.costUSD >= q.MaxCostUSD {
		return false, nil
	}
	if q.RequestsPerMinute > 0 && st.reqCount >= q.RequestsPerMinute {
		return false, nil
	}

	st.reqCount++
	return true, nil
}

// AddUsage records token and cost consumption for the subject.
func (s *MemoryQuotaStore) AddUsage(_ context.Context, subject string, tokens int64, costUSD float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	st := s.stateFor(subject, now)
	st.tokens += tokens
	st.costUSD += costUSD
	return nil
}

// Usage returns the current usage snapshot for the subject.
func (s *MemoryQuotaStore) Usage(_ context.Context, subject string) (Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	st := s.stateFor(subject, now)
	return Usage{Requests: st.reqCount, Tokens: st.tokens, CostUSD: st.costUSD}, nil
}
