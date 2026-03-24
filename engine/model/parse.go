package model

import (
	"fmt"
	"strings"
)

// ModelRef represents a parsed "provider:model_id" string.
type ModelRef struct {
	Provider string
	ModelID  string
}

// ParseModelString parses "provider:model_id" into its components.
// Examples: "openai:gpt-4o", "anthropic:claude-3-5-sonnet", "azure:gpt-4o-mini"
func ParseModelString(s string) (ModelRef, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ModelRef{}, fmt.Errorf("invalid model string %q: expected format 'provider:model_id'", s)
	}
	return ModelRef{
		Provider: strings.TrimSpace(parts[0]),
		ModelID:  strings.TrimSpace(parts[1]),
	}, nil
}

// String returns the "provider:model_id" format.
func (r ModelRef) String() string {
	return r.Provider + ":" + r.ModelID
}

// ProviderFactory creates a Provider from a model reference.
// Register provider constructors here for model-as-string support.
type ProviderFactory func(ref ModelRef) (Provider, error)

var providerFactories = make(map[string]ProviderFactory)

// RegisterProviderFactory registers a factory for a provider name.
func RegisterProviderFactory(name string, factory ProviderFactory) {
	providerFactories[strings.ToLower(name)] = factory
}

// ProviderFromString creates a Provider from a "provider:model_id" string.
func ProviderFromString(s string) (Provider, error) {
	ref, err := ParseModelString(s)
	if err != nil {
		return nil, err
	}
	factory, ok := providerFactories[strings.ToLower(ref.Provider)]
	if !ok {
		return nil, fmt.Errorf("no provider factory registered for %q", ref.Provider)
	}
	return factory(ref)
}
