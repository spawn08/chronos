package tool

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// TestExecutePanicRecovery verifies that a tool whose handler panics results in
// a returned error rather than crashing the process.
func TestExecutePanicRecovery(t *testing.T) {
	tests := []struct {
		name       string
		handler    Handler
		wantSubstr string
	}{
		{
			name: "string panic",
			handler: func(_ context.Context, _ map[string]any) (any, error) {
				panic("boom")
			},
			wantSubstr: "boom",
		},
		{
			name: "error panic",
			handler: func(_ context.Context, _ map[string]any) (any, error) {
				panic(errors.New("kaboom"))
			},
			wantSubstr: "kaboom",
		},
		{
			name: "nil deref panic",
			handler: func(_ context.Context, _ map[string]any) (any, error) {
				var m map[string]any
				m["x"] = 1 //nolint:govet,staticcheck // deliberate nil-map write to exercise panic recovery
				return nil, nil
			},
			wantSubstr: "panicked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			r.Register(&Definition{
				Name:       "panicky",
				Permission: PermAllow,
				Handler:    tt.handler,
			})

			result, err := r.Execute(context.Background(), "panicky", nil)
			if err == nil {
				t.Fatal("expected error from panicking tool, got nil")
				return
			}
			if result != nil {
				t.Fatalf("expected nil result, got %v", result)
			}
			if !strings.Contains(err.Error(), "panicky") {
				t.Errorf("error %q should name the tool", err.Error())
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantSubstr)
			}
			// Process survives: a subsequent normal call must succeed.
			r.Register(&Definition{
				Name:       "ok",
				Permission: PermAllow,
				Handler: func(_ context.Context, _ map[string]any) (any, error) {
					return "fine", nil
				},
			})
			got, err := r.Execute(context.Background(), "ok", nil)
			if err != nil {
				t.Fatalf("normal call after panic failed: %v", err)
			}
			if got != "fine" {
				t.Fatalf("got %v, want fine", got)
			}
		})
	}
}

// TestSetApprovalHandlerRace exercises concurrent SetApprovalHandler and Execute
// to prove the mutex guards the approval/userInput fields. Run with -race.
func TestSetApprovalHandlerRace(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "guarded",
		Permission: PermRequireApproval,
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "done", nil
		},
	})

	approve := func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return true, nil
	}
	userInput := func(_ context.Context, _ string, _ string) (string, error) {
		return "in", nil
	}

	const workers = 8
	const iters = 100
	var wg sync.WaitGroup
	wg.Add(workers * 3)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				r.SetApprovalHandler(approve)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				r.SetUserInputHandler(userInput)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_, _ = r.Execute(context.Background(), "guarded", nil)
			}
		}()
	}
	wg.Wait()
}
