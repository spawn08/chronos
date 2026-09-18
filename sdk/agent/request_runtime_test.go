package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
	storagememory "github.com/spawn08/chronos/storage/adapters/memory"
)

type requestRuntimeProvider struct {
	name    string
	started chan string
	release chan struct{}

	mu     sync.Mutex
	models []string
}

func (p *requestRuntimeProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.record(req.Model)
	if p.started != nil {
		p.started <- req.Messages[len(req.Messages)-1].Content
	}
	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &model.ChatResponse{Role: model.RoleAssistant, Content: p.name}, nil
}

func (p *requestRuntimeProvider) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	p.record(req.Model)
	if p.started != nil {
		p.started <- req.Messages[len(req.Messages)-1].Content
	}
	out := make(chan *model.ChatResponse, 1)
	go func() {
		defer close(out)
		if p.release != nil {
			select {
			case <-p.release:
			case <-ctx.Done():
				out <- &model.ChatResponse{Err: ctx.Err()}
				return
			}
		}
		out <- &model.ChatResponse{Role: model.RoleAssistant, Content: p.name}
	}()
	return out, nil
}

func (p *requestRuntimeProvider) Name() string  { return p.name }
func (p *requestRuntimeProvider) Model() string { return p.name + "-model" }

func (p *requestRuntimeProvider) record(modelID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.models = append(p.models, modelID)
}

func TestRequestModelProvidersDoNotMutateSharedAgent(t *testing.T) {
	defaultProvider := &requestRuntimeProvider{name: "default"}
	first := &requestRuntimeProvider{name: "first"}
	second := &requestRuntimeProvider{name: "second"}
	a, err := New("shared", "Shared").WithModel(defaultProvider).Build()
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for _, provider := range []*requestRuntimeProvider{first, second} {
		provider := provider
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, chatErr := a.Chat(WithModelProvider(context.Background(), provider), "request")
			if chatErr != nil || resp.Content != provider.name {
				t.Errorf("Chat() = (%v, %v), want provider %q", resp, chatErr, provider.name)
			}
		}()
	}
	wg.Wait()

	if a.Model != defaultProvider {
		t.Fatal("request override mutated Agent.Model")
	}
	for _, provider := range []*requestRuntimeProvider{first, second} {
		provider.mu.Lock()
		if len(provider.models) != 1 || provider.models[0] != provider.Model() {
			t.Errorf("%s request models = %v", provider.name, provider.models)
		}
		provider.mu.Unlock()
	}
}

func TestSessionCallsSerializeBySessionAndOverlapAcrossSessions(t *testing.T) {
	provider := &requestRuntimeProvider{
		name:    "session",
		started: make(chan string, 4),
		release: make(chan struct{}, 4),
	}
	defaultProvider := &requestRuntimeProvider{name: "default"}
	a, err := New("shared", "Shared").WithModel(defaultProvider).WithStorage(storagememory.New()).Build()
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithModelProvider(context.Background(), provider)

	stream, err := a.ChatStreamWithSession(ctx, "same", "first")
	if err != nil {
		t.Fatal(err)
	}
	waitStarted(t, provider.started, "first")

	secondDone := make(chan error, 1)
	go func() {
		_, chatErr := a.ChatWithSession(ctx, "same", "second")
		secondDone <- chatErr
	}()
	select {
	case got := <-provider.started:
		t.Fatalf("same-session call started before stream completion: %q", got)
	case <-time.After(25 * time.Millisecond):
	}

	otherDone := make(chan error, 1)
	go func() {
		_, chatErr := a.ChatWithSession(ctx, "other", "other")
		otherDone <- chatErr
	}()
	waitStarted(t, provider.started, "other")
	close(provider.release)
	if err := <-otherDone; err != nil {
		t.Fatal(err)
	}

	for range stream {
	}
	waitStarted(t, provider.started, "second")
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	a.sessionLocksMu.Lock()
	defer a.sessionLocksMu.Unlock()
	if len(a.sessionLocks) != 0 {
		t.Fatalf("idle session locks = %d, want 0", len(a.sessionLocks))
	}
	if a.Model != defaultProvider {
		t.Fatal("session override mutated Agent.Model")
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	for i, modelID := range provider.models {
		if modelID != provider.Model() {
			t.Errorf("request %d model = %q, want %q", i, modelID, provider.Model())
		}
	}
}

func waitStarted(t *testing.T, started <-chan string, want string) {
	t.Helper()
	select {
	case got := <-started:
		if got != want {
			t.Fatalf("started request = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}
