// Google Cloud Vertex AI example for Chronos.
//
// Vertex AI exposes an OpenAI-compatible endpoint (public preview) that
// Chronos can drive through NewOpenAICompatibleWithConfig. Auth uses a
// short-lived GCP access token (Bearer) rather than a static API key.
//
// Prerequisites:
//   - A GCP project with Vertex AI API enabled
//   - `gcloud auth application-default login` (or a workload-identity token)
//   - Go 1.24+
//
// Set the following environment variables before running:
//
//	export GOOGLE_CLOUD_PROJECT=<your-gcp-project-id>
//	export GOOGLE_CLOUD_LOCATION=us-central1
//	export VERTEX_MODEL=google/gemini-2.5-pro
//	export GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)
//
// Run:
//
//	go run ./examples/vertex/main.go
//	go run ./examples/vertex/main.go -stream
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	stream := flag.Bool("stream", false, "enable streaming output")
	flag.Parse()

	ctx := context.Background()

	project := requireEnv("GOOGLE_CLOUD_PROJECT")
	location := envOr("GOOGLE_CLOUD_LOCATION", "us-central1")
	modelID := envOr("VERTEX_MODEL", "google/gemini-2.5-pro")
	token := requireEnv("GOOGLE_ACCESS_TOKEN")

	// Vertex AI's OpenAI-compatible endpoint. The trailing /openapi is what
	// makes /chat/completions resolvable underneath.
	baseURL := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/endpoints/openapi",
		location, project, location,
	)

	provider := model.NewOpenAICompatibleWithConfig("vertex", model.ProviderConfig{
		APIKey:  token, // becomes "Authorization: Bearer <token>"
		BaseURL: baseURL,
		Model:   modelID,
	})

	prompt := "Explain Kubernetes controllers in three sentences."
	systemPrompt := "You are a concise senior platform engineer."

	if *stream {
		ch, err := provider.StreamChat(ctx, &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: systemPrompt},
				{Role: model.RoleUser, Content: prompt},
			},
		})
		if err != nil {
			log.Fatal(err)
		}
		for chunk := range ch {
			fmt.Print(chunk.Content)
		}
		fmt.Println()
		return
	}

	a, err := agent.New("vertex-agent", "Vertex Gemini Agent").
		WithModel(provider).
		WithSystemPrompt(systemPrompt).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	resp, err := a.Chat(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(resp.Content)
}

func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("environment variable %s is required", k)
	}
	return v
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
