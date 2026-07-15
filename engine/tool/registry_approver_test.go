package tool

import (
	"context"
	"errors"
	"testing"
)

// fakeApprover is a test Approver that returns a fixed decision and records the
// tool name it was asked about.
type fakeApprover struct {
	approve bool
	err     error
	gotTool string
}

func (f *fakeApprover) RequestApproval(_ context.Context, toolName string, _ map[string]any) (bool, error) {
	f.gotTool = toolName
	return f.approve, f.err
}

func TestSetApprover_GatesRequireApproval(t *testing.T) {
	tests := []struct {
		name     string
		approver *fakeApprover
		wantRun  bool
		wantErr  bool
	}{
		{"approved runs", &fakeApprover{approve: true}, true, false},
		{"denied blocks", &fakeApprover{approve: false}, false, true},
		{"approver error blocks", &fakeApprover{err: errors.New("down")}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			r.SetApprover(tt.approver)

			ran := false
			r.Register(&Definition{
				Name:       "danger",
				Permission: PermRequireApproval,
				Handler: func(_ context.Context, _ map[string]any) (any, error) {
					ran = true
					return "ok", nil
				},
			})

			_, err := r.Execute(context.Background(), "danger", nil)
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if ran != tt.wantRun {
				t.Fatalf("handler ran = %v, want %v", ran, tt.wantRun)
			}
			if tt.approver.gotTool != "danger" {
				t.Fatalf("approver saw tool %q, want danger", tt.approver.gotTool)
			}
		})
	}
}

func TestSetApprover_NilClearsHandler(t *testing.T) {
	r := NewRegistry()
	r.SetApprover(&fakeApprover{approve: true})
	r.SetApprover(nil)

	r.Register(&Definition{
		Name:       "danger",
		Permission: PermRequireApproval,
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})

	if _, err := r.Execute(context.Background(), "danger", nil); err == nil {
		t.Fatal("expected error when approver cleared (no handler set)")
	}
}
