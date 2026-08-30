// Example: structured_output — asking an LLM for strict JSON and decoding it.
//
// What you'll learn:
//   - How to request JSON output with model.ChatRequest.ResponseFormat
//   - The difference between "json_object" (valid JSON) and "json_schema"
//     (JSON conforming to a schema in Metadata["json_schema"])
//   - How to decode the model's reply into a typed Go struct
//
// This example needs a real LLM provider (it makes one network call). Configure
// one via the environment (OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY,
// …); see examples/internal/providers for the full list. With no provider set
// it prints the required env vars and exits cleanly.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./examples/structured_output/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/examples/internal/providers"
)

// Recipe is the typed shape we want the model to return.
type Recipe struct {
	Name        string   `json:"name"`
	Servings    int      `json:"servings"`
	Ingredients []string `json:"ingredients"`
	Steps       []string `json:"steps"`
}

// jsonSchema mirrors Recipe. OpenAI, Azure OpenAI, OpenAI-compatible
// providers (Ollama, Mistral, ...), Gemini, and the Responses API send this
// to the model as a native structured-output constraint. Anthropic has no
// equivalent API parameter, so it falls back to best-effort JSON driven only
// by the system prompt below — which is why that prompt is explicit about
// returning ONLY JSON. Every provider's reply is still validated against
// this schema afterward (see sdk/agent's OutputSchema when going through
// Agent.Chat instead of calling a Provider directly, as this example does).
var jsonSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":        map[string]any{"type": "string"},
		"servings":    map[string]any{"type": "integer"},
		"ingredients": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"steps":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"name", "servings", "ingredients", "steps"},
}

func main() {
	ctx := context.Background()

	fmt.Println("━━━ Chronos structured (JSON) output example ━━━")

	provider, name := providers.Pick()
	if provider == nil {
		fmt.Println("No LLM provider configured.")
		fmt.Println(providers.EnvHint())
		return
	}
	fmt.Printf("Provider: %s (%s)\n", name, provider.Model())

	// Request strict JSON. ResponseFormat "json_schema" carries the schema in
	// Metadata["json_schema"] (see the jsonSchema comment above for which
	// providers actually enforce it) — the explicit system prompt below is
	// what carries the load on a provider (Anthropic) that doesn't.
	resp, err := provider.Chat(ctx, &model.ChatRequest{
		ResponseFormat: "json_schema",
		Metadata:       map[string]any{"json_schema": jsonSchema},
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You return ONLY valid JSON matching the requested schema. No prose, no code fences."},
			{Role: model.RoleUser, Content: "Give me a simple recipe for pancakes."},
		},
	})
	if err != nil {
		log.Fatalf("chat: %v", err)
	}

	recipe, err := parseRecipe(resp.Content)
	if err != nil {
		log.Fatalf("decode JSON: %v\nraw response: %s", err, resp.Content)
	}

	fmt.Printf("\nParsed struct:\n")
	fmt.Printf("  Name:     %s\n", recipe.Name)
	fmt.Printf("  Servings: %d\n", recipe.Servings)
	fmt.Printf("  Ingredients (%d):\n", len(recipe.Ingredients))
	for _, ing := range recipe.Ingredients {
		fmt.Printf("    - %s\n", ing)
	}
	fmt.Printf("  Steps (%d):\n", len(recipe.Steps))
	for i, step := range recipe.Steps {
		fmt.Printf("    %d. %s\n", i+1, step)
	}
}

// parseRecipe decodes a model reply into a Recipe. It tolerates the common case
// where a model wraps JSON in ```json code fences despite instructions.
func parseRecipe(content string) (Recipe, error) {
	var r Recipe
	if err := json.Unmarshal([]byte(stripCodeFence(content)), &r); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// stripCodeFence removes a leading/trailing Markdown code fence if present.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
