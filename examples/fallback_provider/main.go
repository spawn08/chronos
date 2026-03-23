// Example: fallback_provider demonstrates automatic failover between model providers.
//
// The FallbackProvider tries each provider in order: if the first fails, it
// transparently falls back to the next. This is useful for production setups
// where you want primary → secondary → local failover.
//
// No API keys needed — uses mock providers that simulate failures.
//
//	go run ./examples/fallback_provider/
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Fallback Provider Example                 ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── 1. Chain with a failing primary ──

	fmt.Println("\n━━━ 1. Primary Fails → Secondary Succeeds ━━━")

	failingPrimary := &mockFailProvider{name: "primary-cloud", model: "gpt-4o"}
	workingSecondary := &mockOKProvider{name: "secondary-cloud", model: "gpt-4o-mini"}
	localFallback := &mockOKProvider{name: "local-ollama", model: "llama3.2"}

	fallback, err := model.NewFallbackProvider(failingPrimary, workingSecondary, localFallback)
	if err != nil {
		log.Fatal(err)
	}
	fallback.OnFallback = func(index int, name string, err error) {
		fmt.Printf("  [FALLBACK] Provider %d (%s) failed: %v → trying next\n", index, name, err)
	}

	a, err := agent.New("resilient-agent", "Resilient Agent").
		WithModel(fallback).
		WithSystemPrompt("You are a helpful assistant.").
		Build()
	if err != nil {
		log.Fatal(err)
	}

	resp, err := a.Chat(ctx, "What is the meaning of life?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Response from: %s\n", resp.Content)
	fmt.Printf("  Fallback provider name: %s\n", fallback.Name())
	fmt.Printf("  Fallback primary model: %s\n", fallback.Model())

	// ── 2. All providers work → uses primary ──

	fmt.Println("\n━━━ 2. All Providers Work → Uses Primary ━━━")

	ok1 := &mockOKProvider{name: "primary", model: "gpt-4o"}
	ok2 := &mockOKProvider{name: "backup", model: "gpt-4o-mini"}

	fb2, err := model.NewFallbackProvider(ok1, ok2)
	if err != nil {
		log.Fatal(err)
	}
	fb2.OnFallback = func(index int, name string, err error) {
		fmt.Printf("  [FALLBACK] Provider %d (%s) failed: %v\n", index, name, err)
	}

	resp, err = fb2.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Hello"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Response: %s (no fallback needed)\n", resp.Content)

	// ── 3. All providers fail → graceful error ──

	fmt.Println("\n━━━ 3. All Providers Fail → Graceful Error ━━━")

	fail1 := &mockFailProvider{name: "cloud-a", model: "model-a"}
	fail2 := &mockFailProvider{name: "cloud-b", model: "model-b"}
	fail3 := &mockFailProvider{name: "cloud-c", model: "model-c"}

	fb3, err := model.NewFallbackProvider(fail1, fail2, fail3)
	if err != nil {
		log.Fatal(err)
	}
	fb3.OnFallback = func(index int, name string, err error) {
		fmt.Printf("  [FALLBACK] Provider %d (%s) failed: %v\n", index, name, err)
	}

	_, err = fb3.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Hello"}},
	})
	fmt.Printf("  Final error: %v\n", err)

	// ── 4. Streaming fallback ──

	fmt.Println("\n━━━ 4. Streaming with Fallback ━━━")

	streamFb, err := model.NewFallbackProvider(failingPrimary, workingSecondary)
	if err != nil {
		log.Fatal(err)
	}
	streamFb.OnFallback = func(index int, name string, err error) {
		fmt.Printf("  [FALLBACK] Stream: Provider %d (%s) failed: %v\n", index, name, err)
	}

	ch, err := streamFb.StreamChat(ctx, &model.ChatRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Stream test"}},
	})
	if err != nil {
		fmt.Printf("  Stream error: %v\n", err)
	} else {
		for chunk := range ch {
			fmt.Printf("  Stream chunk: %s\n", chunk.Content)
		}
	}

	// ── 5. Single provider (degenerate case) ──

	fmt.Println("\n━━━ 5. Single Provider (No Fallback Needed) ━━━")

	single, err := model.NewFallbackProvider(&mockOKProvider{name: "solo", model: "gpt-4o"})
	if err != nil {
		log.Fatal(err)
	}
	resp, err = single.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Just me"}},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Response: %s\n", resp.Content)
	fmt.Printf("  Provider: %s\n", single.Name())

	// ── 6. Zero providers → error ──

	fmt.Println("\n━━━ 6. Zero Providers → Error ━━━")

	_, err = model.NewFallbackProvider()
	fmt.Printf("  Error: %v\n", err)

	fmt.Println("\n✓ Fallback Provider example completed.")
}

type mockFailProvider struct {
	name  string
	model string
}

func (p *mockFailProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return nil, fmt.Errorf("%s: connection timeout", p.name)
}

func (p *mockFailProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, fmt.Errorf("%s: stream connection timeout", p.name)
}

func (p *mockFailProvider) Name() string  { return p.name }
func (p *mockFailProvider) Model() string { return p.model }

type mockOKProvider struct {
	name  string
	model string
}

func (p *mockOKProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1].Content
	return &model.ChatResponse{
		Content:    fmt.Sprintf("[%s/%s] Response to: %s", p.name, p.model, last),
		Role:       "assistant",
		StopReason: model.StopReasonEnd,
	}, nil
}

func (p *mockOKProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := p.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (p *mockOKProvider) Name() string  { return p.name }
func (p *mockOKProvider) Model() string { return p.model }
