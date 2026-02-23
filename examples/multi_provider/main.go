// Example: multi_provider demonstrates connecting agents to different LLM providers.
//
// This shows how the same agent builder API works with OpenAI, Anthropic, Gemini,
// Mistral, Ollama, Azure OpenAI, and any OpenAI-compatible endpoint.
//
// Set environment variables for the providers you want to test:
//
//	OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, MISTRAL_API_KEY
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/chronos-ai/chronos/engine/graph"
	"github.com/chronos-ai/chronos/engine/model"
	"github.com/chronos-ai/chronos/sdk/agent"
	"github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// Show all available provider constructors.
	providers := buildProviders()
	if len(providers) == 0 {
		fmt.Println("No API keys found. Set at least one of:")
		fmt.Println("  OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, MISTRAL_API_KEY")
		fmt.Println()
		fmt.Println("Or use Ollama (no key needed) with a running local server.")
		return
	}

	for name, provider := range providers {
		fmt.Printf("\n--- %s (model: %s) ---\n", name, provider.Model())

		g := graph.New("chat-graph").
			AddNode("respond", func(_ context.Context, s graph.State) (graph.State, error) {
				msg := fmt.Sprintf("Hello from %s!", name)
				s["response"] = msg
				return s, nil
			}).
			SetEntryPoint("respond").
			SetFinishPoint("respond")

		a, err := agent.New(name+"-agent", name+" Agent").
			Description(fmt.Sprintf("Agent powered by %s", name)).
			WithModel(provider).
			WithStorage(store).
			WithGraph(g).
			Build()
		if err != nil {
			log.Printf("Failed to build %s agent: %v", name, err)
			continue
		}

		result, err := a.Run(ctx, map[string]any{"user": "World"})
		if err != nil {
			log.Printf("%s agent error: %v", name, err)
			continue
		}
		fmt.Printf("Result: %v\n", result.State["response"])
	}
}

func buildProviders() map[string]model.Provider {
	providers := make(map[string]model.Provider)

	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		providers["OpenAI"] = model.NewOpenAI(key)
	}

	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		providers["Anthropic"] = model.NewAnthropic(key)
	}

	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		providers["Gemini"] = model.NewGemini(key)
	}

	if key := os.Getenv("MISTRAL_API_KEY"); key != "" {
		providers["Mistral"] = model.NewMistral(key)
	}

	// Ollama (local, no API key needed — uncomment if Ollama is running)
	// providers["Ollama"] = model.NewOllama("http://localhost:11434", "llama3.2")

	// Azure OpenAI (uncomment and fill in your details)
	// providers["AzureOpenAI"] = model.NewAzureOpenAI(
	// 	"https://your-resource.openai.azure.com",
	// 	os.Getenv("AZURE_OPENAI_API_KEY"),
	// 	"gpt-4o",
	// )

	// Any OpenAI-compatible endpoint
	// providers["Custom"] = model.NewOpenAICompatible(
	// 	"my-server", "http://localhost:8080/v1", "", "my-model",
	// )

	return providers
}
