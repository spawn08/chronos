// Example: multi_provider demonstrates connecting agents to different LLM providers.
//
// This shows how the same agent builder API works with OpenAI, Anthropic, Gemini,
// Mistral, Ollama, Azure OpenAI, Google Cloud Vertex AI, AWS Bedrock, and any
// OpenAI-compatible endpoint (Together, Groq, DeepSeek, OpenRouter, Fireworks,
// Perplexity, Anyscale, vLLM, LiteLLM).
//
// Set environment variables for the providers you want to test:
//
//	OPENAI_API_KEY, ANTHROPIC_API_KEY, GEMINI_API_KEY, MISTRAL_API_KEY
//	AZURE_OPENAI_API_KEY / AZURE_OPENAI_ENDPOINT / AZURE_OPENAI_DEPLOYMENT
//	GOOGLE_CLOUD_PROJECT / GOOGLE_ACCESS_TOKEN (see examples/vertex)
//	AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_REGION (Bedrock)
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
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

	// Azure OpenAI — key + endpoint + deployment resource name.
	if key, endpoint, deployment := os.Getenv("AZURE_OPENAI_API_KEY"),
		os.Getenv("AZURE_OPENAI_ENDPOINT"),
		os.Getenv("AZURE_OPENAI_DEPLOYMENT"); key != "" && endpoint != "" && deployment != "" {
		providers["AzureOpenAI"] = model.NewAzureOpenAIWithConfig(model.AzureConfig{
			ProviderConfig: model.ProviderConfig{APIKey: key, BaseURL: endpoint},
			Deployment:     deployment,
			APIVersion:     envOr("AZURE_OPENAI_API_VERSION", "2024-12-01-preview"),
		})
	}

	// Google Cloud Vertex AI via its OpenAI-compatible endpoint. Auth uses a
	// short-lived access token from `gcloud auth print-access-token`.
	if project, token := os.Getenv("GOOGLE_CLOUD_PROJECT"), os.Getenv("GOOGLE_ACCESS_TOKEN"); project != "" && token != "" {
		location := envOr("GOOGLE_CLOUD_LOCATION", "us-central1")
		base := fmt.Sprintf(
			"https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/endpoints/openapi",
			location, project, location,
		)
		providers["Vertex"] = model.NewOpenAICompatibleWithConfig("vertex", model.ProviderConfig{
			APIKey:  token,
			BaseURL: base,
			Model:   envOr("VERTEX_MODEL", "google/gemini-2.5-pro"),
		})
	}

	// AWS Bedrock (Claude, Llama, Titan, Cohere, Mistral on Bedrock).
	if region, ak, sk := os.Getenv("AWS_REGION"),
		os.Getenv("AWS_ACCESS_KEY_ID"),
		os.Getenv("AWS_SECRET_ACCESS_KEY"); region != "" && ak != "" && sk != "" {
		providers["Bedrock"] = model.NewBedrock(region, ak, sk,
			envOr("BEDROCK_MODEL_ID", "anthropic.claude-3-5-sonnet-20241022-v2:0"))
	}

	// Any OpenAI-compatible endpoint — Together, Groq, DeepSeek, vLLM, LiteLLM…
	// providers["Groq"] = model.NewGroq(os.Getenv("GROQ_API_KEY"), "llama-3.3-70b-versatile")
	// providers["Custom"] = model.NewOpenAICompatible(
	// 	"my-server", "http://localhost:8080/v1", "", "my-model",
	// )

	return providers
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
