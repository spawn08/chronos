package chronosos

import (
	"testing"

	"github.com/spawn08/chronos/storage/adapters/memory"
)

func TestNew_PopulatesSubsystems_Boost(t *testing.T) {
	st := memory.New()
	s := New(":0", st)
	if s == nil {
		t.Fatal("nil server")
	}
	if s.Broker == nil || s.Auth == nil || s.Trace == nil || s.Approval == nil || s.Metrics == nil || s.Scheduler == nil || s.mux == nil {
		t.Fatal("expected all subsystems non-nil")
	}
	if s.ShutdownTimeout == 0 {
		t.Error("ShutdownTimeout should be positive")
	}
}
