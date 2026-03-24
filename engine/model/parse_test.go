package model

import (
	"testing"
)

func TestParseModelString(t *testing.T) {
	tests := []struct {
		input    string
		provider string
		modelID  string
		wantErr  bool
	}{
		{"openai:gpt-4o", "openai", "gpt-4o", false},
		{"anthropic:claude-3-5-sonnet", "anthropic", "claude-3-5-sonnet", false},
		{"azure:gpt-4o-mini", "azure", "gpt-4o-mini", false},
		{"ollama:llama3", "ollama", "llama3", false},
		{"invalid", "", "", true},
		{":model", "", "", true},
		{"provider:", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		ref, err := ParseModelString(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseModelString(%q) expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseModelString(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if ref.Provider != tt.provider {
			t.Errorf("ParseModelString(%q).Provider = %q, want %q", tt.input, ref.Provider, tt.provider)
		}
		if ref.ModelID != tt.modelID {
			t.Errorf("ParseModelString(%q).ModelID = %q, want %q", tt.input, ref.ModelID, tt.modelID)
		}
	}
}

func TestModelRef_String(t *testing.T) {
	ref := ModelRef{Provider: "openai", ModelID: "gpt-4o"}
	if ref.String() != "openai:gpt-4o" {
		t.Errorf("String() = %q", ref.String())
	}
}

func TestProviderFromString_NoFactory(t *testing.T) {
	_, err := ProviderFromString("unknown:model")
	if err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}
