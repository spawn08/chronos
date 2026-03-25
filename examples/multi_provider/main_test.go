package main

import (
	"testing"

	"github.com/spawn08/chronos/examples/internal/exampletest"
)

func TestMainCompletes(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("MISTRAL_API_KEY", "")

	out := exampletest.RunWithStdoutCapture(t, main)
	exampletest.AssertOutputContains(t, out, "No API keys found")
}
