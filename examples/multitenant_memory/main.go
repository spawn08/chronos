// Example: multitenant_memory demonstrates per-user long-term memory isolation —
// two different users served by one logical agent, whose memories never leak
// across tenants.
//
// Chronos long-term memory (memory.Store / memory.Manager) is keyed by the
// Store's agentID. To isolate memory per user you construct a Store whose id is
// namespaced with the user id (agentID + "::" + userID). This example uses that
// pattern to prove that Ada and Bob each see only their own memories.
//
// The deterministic part uses SQLite and needs NO API keys. An optional final
// section runs the LLM-powered memory.Manager against a real provider, and is
// guarded behind OPENAI_API_KEY so `go run` still works without one.
//
//	go run ./examples/multitenant_memory/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// agentID is the single logical agent shared by every tenant.
const agentID = "concierge"

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos Multi-Tenant Memory Isolation Example        ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	dir, err := os.MkdirTemp("", "chronos-mtmem-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := sqlite.New(filepath.Join(dir, "memory.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// ── 1. Seed distinct long-term memories for two tenants ──
	alice := userStore(store, "user_alice")
	bob := userStore(store, "user_bob")

	must(alice.SetLongTerm(ctx, "favorite_language", "Go"))
	must(alice.SetLongTerm(ctx, "timezone", "Europe/London"))
	must(bob.SetLongTerm(ctx, "favorite_language", "Rust"))
	must(bob.SetLongTerm(ctx, "plan", "enterprise"))

	// ── 2. Each tenant sees only its own memories ──
	fmt.Println("\n━━━ Long-term memory per tenant ━━━")
	printMemories(ctx, "alice", alice)
	printMemories(ctx, "bob", bob)

	// ── 3. Prove isolation: Bob's key must not resolve in Alice's store ──
	fmt.Println("\n━━━ Cross-tenant isolation check ━━━")
	if _, err := alice.Get(ctx, "plan"); err != nil {
		fmt.Printf("  alice.Get(\"plan\") -> not found (expected): %v\n", err)
	} else {
		log.Fatal("isolation breach: alice can read bob's \"plan\" memory")
	}
	aliceLang, _ := alice.Get(ctx, "favorite_language")
	bobLang, _ := bob.Get(ctx, "favorite_language")
	fmt.Printf("  same key, different tenants: alice=%v  bob=%v\n", aliceLang, bobLang)
	if aliceLang == bobLang {
		log.Fatal("isolation breach: tenants share the same value for favorite_language")
	}

	// ── 4. memory.Manager.GetUserMemories is likewise scoped per tenant ──
	// GetUserMemories reads long-term memory only; it does not call the model,
	// so the mock provider below is never invoked here.
	fmt.Println("\n━━━ memory.Manager context injection per tenant ━━━")
	aliceMgr := memory.NewManager(agentID, "user_alice", alice, &noopProvider{})
	bobMgr := memory.NewManager(agentID, "user_bob", bob, &noopProvider{})
	printManagerContext(ctx, "alice", aliceMgr)
	printManagerContext(ctx, "bob", bobMgr)

	// ── 5. Optional: autonomous LLM memory extraction (needs a real provider) ──
	runLLMExtraction(ctx, alice)

	fmt.Println("\n✓ Multi-tenant memory isolation verified.")
}

// userStore builds a long-term memory Store scoped to a single tenant by
// namespacing the agent id with the user id.
func userStore(backend storage.Storage, userID string) *memory.Store {
	return memory.NewStore(agentID+"::"+userID, backend)
}

func printMemories(ctx context.Context, label string, s *memory.Store) {
	mems, err := s.ListLongTerm(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  %s (%d memories):\n", label, len(mems))
	for _, m := range mems {
		fmt.Printf("    - %s = %v\n", m.Key, m.Value)
	}
}

func printManagerContext(ctx context.Context, label string, mgr *memory.Manager) {
	ctxStr, err := mgr.GetUserMemories(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  %s context block:\n", label)
	for _, line := range splitLines(ctxStr) {
		fmt.Printf("    | %s\n", line)
	}
}

// runLLMExtraction shows the LLM-powered Manager deciding what to remember from
// a conversation. It is guarded behind OPENAI_API_KEY so the example still runs
// end-to-end without credentials.
func runLLMExtraction(ctx context.Context, alice *memory.Store) {
	fmt.Println("\n━━━ Autonomous LLM extraction (optional) ━━━")
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("  OPENAI_API_KEY not set — skipping live extraction.")
		fmt.Println("  Set it and re-run to see memory.Manager.ExtractMemories store new facts.")
		return
	}

	provider := model.NewOpenAIWithConfig(model.ProviderConfig{APIKey: apiKey, Model: "gpt-4o"})
	mgr := memory.NewManager(agentID, "user_alice", alice, provider)
	convo := []model.Message{
		{Role: model.RoleUser, Content: "By the way, I just moved to Berlin and I prefer dark-mode dashboards."},
	}
	if err := mgr.ExtractMemories(ctx, convo); err != nil {
		fmt.Printf("  extraction failed: %v\n", err)
		return
	}
	fmt.Println("  extraction complete — alice's long-term memory now includes:")
	printMemories(ctx, "alice", alice)
}

// noopProvider is a model.Provider placeholder used where the API requires a
// provider but no model call is actually made (e.g. GetUserMemories).
type noopProvider struct{}

func (p *noopProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return nil, fmt.Errorf("noopProvider: Chat should not be called in this example")
}

func (p *noopProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, fmt.Errorf("noopProvider: StreamChat should not be called in this example")
}

func (p *noopProvider) Name() string  { return "noop" }
func (p *noopProvider) Model() string { return "noop" }

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
