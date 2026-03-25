package chronosos

import (
	"context"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage/adapters/memory"
)

func TestStart_ContextCancel_Shutdown_Squeeze(t *testing.T) {
	s := New("127.0.0.1:0", memory.New())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
}
