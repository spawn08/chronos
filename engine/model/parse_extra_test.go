package model

import (
	"strings"
	"testing"
)

func TestProviderFromString_ParseError(t *testing.T) {
	_, err := ProviderFromString("not-a-model-ref")
	if err == nil {
		t.Fatal("expected error from invalid model string")
	}
	if !strings.Contains(err.Error(), "invalid model string") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestProviderFromString_UsesLowercaseProviderKey(t *testing.T) {
	RegisterProviderFactory("ParseExtraCase", func(ref ModelRef) (Provider, error) {
		return NewOpenAI("x"), nil
	})
	p, err := ProviderFromString("parseextracase:gpt-4o")
	if err != nil {
		t.Fatalf("ProviderFromString: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider")
	}
}
