package model

import "testing"

func TestNewGeminiWithConfig_DefaultBaseURLAndModel_Squeeze(t *testing.T) {
	t.Parallel()
	g := NewGeminiWithConfig(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: "",
		Model:   "",
	})
	if g == nil {
		t.Fatal("nil provider")
	}
	if g.config.BaseURL != "https://generativelanguage.googleapis.com/v1beta" {
		t.Errorf("BaseURL=%q", g.config.BaseURL)
	}
	if g.config.Model != "gemini-2.0-flash" {
		t.Errorf("Model=%q", g.config.Model)
	}
	if g.Model() != "gemini-2.0-flash" {
		t.Errorf("Model()=%q", g.Model())
	}
}

func TestNewGeminiWithConfig_PreservesExplicit_Squeeze(t *testing.T) {
	t.Parallel()
	g := NewGeminiWithConfig(ProviderConfig{
		APIKey:  "k",
		BaseURL: "https://custom.example/v1",
		Model:   "gemini-pro",
	})
	if g.config.BaseURL != "https://custom.example/v1" || g.config.Model != "gemini-pro" {
		t.Fatalf("config=%+v", g.config)
	}
}
