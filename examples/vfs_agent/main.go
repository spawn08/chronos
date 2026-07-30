// Example: vfs_agent demonstrates the virtual filesystem for context offloading
// (WC-A-002). The agent produces a large intermediate artifact, saves it to
// per-session scratch storage with fs_write (paying only a tiny receipt in
// context instead of the whole artifact), keeps working with a small context,
// then pages part of it back in with fs_read to produce the final answer.
//
// A deterministic mock model.Provider drives the loop, so the example runs with
// NO API keys and NO network access:
//
//	turn 0: generate a big report and fs_write it            (context stays tiny)
//	turn 1: fs_read the saved report back                    (page it in on demand)
//	turn 2: emit the final answer using the retrieved content
//
//	go run ./examples/vfs_agent/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const (
	sessionID    = "vfs-session-1"
	artifactPath = "research/report.md"
)

// bigArtifact is the large intermediate result the agent offloads (~55 KB).
var bigArtifact = strings.Repeat(
	"Go's history: conceived at Google in 2007 by Griesemer, Pike, and Thompson; open-sourced in 2009. ",
	560,
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos Virtual Filesystem (Context Offload) Example ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}
	vfs, err := builtins.NewStorageVFS(store)
	if err != nil {
		log.Fatal(err)
	}

	a, err := agent.New("research-agent", "Research Assistant").
		WithModel(&vfsMock{}).
		WithStorage(store).
		WithSystemPrompt("You research topics. Offload large notes to scratch storage; keep your context small.").
		AddToolkit(builtins.NewVFSToolkit(vfs)).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	task := "Research the history of Go and summarize it in one sentence."
	fmt.Printf("\nUser: %s\n", task)
	fmt.Printf("(the intermediate report is %d bytes; watch that it never enters the chat context)\n\n", len(bigArtifact))

	resp, err := a.ChatWithSession(ctx, sessionID, task)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nAssistant: %s\n", resp.Content)
	fmt.Println("\n✓ The 55 KB report lived in the VFS, not the prompt — only a path + a short summary did.")
}

// vfsMock is a deterministic provider that offloads a big artifact, reads it
// back, then answers — choosing its next step from the most recent message.
type vfsMock struct{}

func (m *vfsMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1]

	if last.Role == model.RoleTool {
		switch last.Name {
		case builtins.FSWriteToolName:
			// The write returned only a tiny receipt; now read the report back.
			fmt.Printf("  [context] after fs_write, the tool receipt is %d bytes (not %d)\n", len(last.Content), len(bigArtifact))
			return toolCall("c2", builtins.FSReadToolName, map[string]any{"path": artifactPath}), nil
		case builtins.FSReadToolName:
			// We have the content again; produce the final one-sentence summary.
			var out struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal([]byte(last.Content), &out)
			sentence := firstSentence(out.Content)
			return &model.ChatResponse{Role: model.RoleAssistant, Content: sentence, StopReason: model.StopReasonEnd}, nil
		}
	}

	// Turn 0: generate the big report and offload it.
	return toolCall("c1", builtins.FSWriteToolName, map[string]any{
		"path":    artifactPath,
		"content": bigArtifact,
	}), nil
}

func (m *vfsMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *vfsMock) Name() string  { return "vfs-mock" }
func (m *vfsMock) Model() string { return "mock-v1" }

func firstSentence(s string) string {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return strings.TrimSpace(s[:i+1])
	}
	return strings.TrimSpace(s)
}

func toolCall(id, name string, args map[string]any) *model.ChatResponse {
	raw, _ := json.Marshal(args)
	return &model.ChatResponse{
		Role:       model.RoleAssistant,
		StopReason: model.StopReasonToolCall,
		ToolCalls:  []model.ToolCall{{ID: id, Name: name, Arguments: string(raw)}},
	}
}
