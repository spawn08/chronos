// Azure OpenAI example for Chronos.
//
// This example demonstrates how to use Chronos with Azure OpenAI, in both
// standard (full response) and streaming (token-by-token) modes.
//
// Prerequisites:
//   - An Azure OpenAI resource with a deployed model
//   - Go 1.24+
//
// Set the following environment variables before running:
//
//	export AZURE_OPENAI_API_KEY=<your-azure-api-key>
//	export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com
//	export AZURE_OPENAI_DEPLOYMENT=<your-deployment-name>
//	export AZURE_OPENAI_API_VERSION=2024-12-01-preview
//
// Run (standard mode — waits for full response, then prints):
//
//	go run ./examples/azure/main.go
//
// Run (streaming mode — prints tokens as they arrive):
//
//	go run ./examples/azure/main.go -stream
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/chronos-ai/chronos/engine/model"
	"github.com/chronos-ai/chronos/sdk/agent"
)

func main() {
	stream := flag.Bool("stream", false, "enable streaming output")
	flag.Parse()

	ctx := context.Background()

	provider := model.NewAzureOpenAIWithConfig(model.AzureConfig{
		ProviderConfig: model.ProviderConfig{
			APIKey:  os.Getenv("AZURE_OPENAI_API_KEY"),
			BaseURL: os.Getenv("AZURE_OPENAI_ENDPOINT"),
		},
		Deployment: os.Getenv("AZURE_OPENAI_DEPLOYMENT"),
		APIVersion: os.Getenv("AZURE_OPENAI_API_VERSION"),
	})

	prompt := "Write a Go program to sort an array using the quicksort algorithm."
	systemPrompt := "You are a helpful assistant that specializes in Go for writing efficient algorithms and data structures."

	if *stream {
		runStreaming(ctx, provider, systemPrompt, prompt)
	} else {
		runChat(ctx, provider, systemPrompt, prompt)
	}
}

func runChat(ctx context.Context, provider model.Provider, systemPrompt, prompt string) {
	a, err := agent.New("azure-agent", "Azure GPT-4o Mini").
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

func runStreaming(ctx context.Context, provider model.Provider, systemPrompt, prompt string) {
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
}
