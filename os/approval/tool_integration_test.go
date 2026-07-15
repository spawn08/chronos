package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// TestService_GatesRequireApprovalTool proves the approval Service satisfies
// tool.Approver and, once wired via Registry.SetApprover, gates a tool whose
// Permission is PermRequireApproval: the tool handler runs only after approval.
func TestService_GatesRequireApprovalTool(t *testing.T) {
	// Compile-time proof of the seam.
	var _ tool.Approver = (*Service)(nil)

	tests := []struct {
		name    string
		approve bool
		wantRun bool
		wantErr bool
	}{
		{"approved runs handler", true, true, false},
		{"denied blocks handler", false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService()
			reg := tool.NewRegistry()
			reg.SetApprover(svc)

			ran := make(chan struct{}, 1)
			reg.Register(&tool.Definition{
				Name:       "delete_all",
				Permission: tool.PermRequireApproval,
				Handler: func(_ context.Context, _ map[string]any) (any, error) {
					ran <- struct{}{}
					return "ok", nil
				},
			})

			resCh := make(chan error, 1)
			go func() {
				_, err := reg.Execute(context.Background(), "delete_all", map[string]any{"scope": "prod"})
				resCh <- err
			}()

			id := waitForPending(t, svc)
			body, _ := json.Marshal(map[string]any{"id": id, "approved": tt.approve})
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/respond", bytes.NewBuffer(body))
			svc.HandleRespond(w, r)

			select {
			case err := <-resCh:
				if tt.wantErr && err == nil {
					t.Fatal("expected error when denied")
				}
				if !tt.wantErr && err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Execute did not return")
			}

			select {
			case <-ran:
				if !tt.wantRun {
					t.Fatal("handler ran despite denial")
				}
			default:
				if tt.wantRun {
					t.Fatal("handler did not run despite approval")
				}
			}
		})
	}
}
