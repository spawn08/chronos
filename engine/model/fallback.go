package model

import (
	"context"
	"fmt"
	"strings"
)

// FallbackProvider wraps multiple providers and tries them in order.
// If the primary provider fails, it automatically falls back to the next one.
// This is useful for scenarios like primary-cloud -> cheaper-model or
// cloud-provider -> local-ollama.
type FallbackProvider struct {
	providers []Provider
	// OnFallback is called when a provider fails and the next one is tried.
	// It receives the failed provider index, its name, and the error.
	OnFallback func(index int, name string, err error)
}

// NewFallbackProvider creates a fallback provider from the given providers.
// At least one provider is required.
func NewFallbackProvider(providers ...Provider) (*FallbackProvider, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("fallback provider: at least one provider is required")
	}
	return &FallbackProvider{providers: providers}, nil
}

func (f *FallbackProvider) Name() string {
	names := make([]string, len(f.providers))
	for i, p := range f.providers {
		names[i] = p.Name()
	}
	return "fallback(" + strings.Join(names, ",") + ")"
}

func (f *FallbackProvider) Model() string {
	if len(f.providers) > 0 {
		return f.providers[0].Model()
	}
	return ""
}

// Chat tries each provider in order until one succeeds.
func (f *FallbackProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	var lastErr error
	for i, p := range f.providers {
		resp, err := p.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if f.OnFallback != nil {
			f.OnFallback(i, p.Name(), err)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("fallback provider: context cancelled after %d attempts: %w", i+1, ctx.Err())
		}
	}
	return nil, fmt.Errorf("fallback provider: all %d providers failed, last error: %w", len(f.providers), lastErr)
}

// StreamChat tries each provider in order until one succeeds.
func (f *FallbackProvider) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	var lastErr error
	for i, p := range f.providers {
		ch, err := p.StreamChat(ctx, req)
		if err == nil {
			return ch, nil
		}
		lastErr = err
		if f.OnFallback != nil {
			f.OnFallback(i, p.Name(), err)
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("fallback provider: context cancelled after %d attempts: %w", i+1, ctx.Err())
		}
	}
	return nil, fmt.Errorf("fallback provider: all %d providers failed, last error: %w", len(f.providers), lastErr)
}
