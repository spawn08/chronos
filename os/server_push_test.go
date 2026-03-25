package chronosos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage/adapters/memory"
)

type errMigrateMemStore struct {
	*memory.Store
}

func (e *errMigrateMemStore) Migrate(ctx context.Context) error {
	return errors.New("migrate failed (push test)")
}

func TestHandleReadiness_MigrateError_Push(t *testing.T) {
	base := memory.New()
	s := New(":0", &errMigrateMemStore{Store: base})
	s.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when Migrate fails, got %d body=%q", w.Code, w.Body.String())
	}
}

func TestStart_InvalidListenAddr_ReturnsError_Push(t *testing.T) {
	s := New(":0", memory.New())
	s.Addr = "localhost:999999999"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.Start(ctx)
	if err == nil {
		t.Fatal("expected Start to return ListenAndServe error for invalid address")
	}
}

func TestNew_SetsSchedulerAndTrace_Push(t *testing.T) {
	s := New("127.0.0.1:0", memory.New())
	if s.Scheduler == nil || s.Trace == nil || s.Metrics == nil {
		t.Fatalf("expected Scheduler, Trace, Metrics to be initialized: %+v", s)
	}
}
