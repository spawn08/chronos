package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AzureOpenAI implements Provider for Azure-hosted OpenAI models.
// Uses the Azure-specific endpoint format: {base}/openai/deployments/{deployment}/chat/completions?api-version={version}
type AzureOpenAI struct {
	config     ProviderConfig
	deployment string
	apiVersion string
	http       *httpClient
}

// AzureConfig extends ProviderConfig with Azure-specific fields.
type AzureConfig struct {
	ProviderConfig
	Deployment string `json:"deployment"`
	APIVersion string `json:"api_version"`
}

// NewAzureOpenAI creates a new Azure OpenAI provider.
// endpoint is the Azure resource endpoint (e.g., "https://myresource.openai.azure.com").
// apiKey is the Azure API key.
// deployment is the model deployment name.
func NewAzureOpenAI(endpoint, apiKey, deployment string) *AzureOpenAI {
	return NewAzureOpenAIWithConfig(AzureConfig{
		ProviderConfig: ProviderConfig{
			APIKey:  apiKey,
			BaseURL: endpoint,
			Model:   deployment,
		},
		Deployment: deployment,
		APIVersion: "2024-10-21",
	})
}

// NewAzureOpenAIWithConfig creates an Azure OpenAI provider with full configuration.
func NewAzureOpenAIWithConfig(cfg AzureConfig) *AzureOpenAI {
	if cfg.APIVersion == "" {
		cfg.APIVersion = "2024-10-21"
	}
	headers := map[string]string{
		"api-key": cfg.APIKey,
	}
	return &AzureOpenAI{
		config:     cfg.ProviderConfig,
		deployment: cfg.Deployment,
		apiVersion: cfg.APIVersion,
		http:       newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (a *AzureOpenAI) Name() string  { return "azure-openai" }
func (a *AzureOpenAI) Model() string { return a.deployment }

func (a *AzureOpenAI) chatPath() string {
	return fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=%s", a.deployment, a.apiVersion)
}

func (a *AzureOpenAI) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := buildOpenAIRequestBody(req, a.deployment, false)
	delete(body, "model") // Azure uses the deployment name in the URL

	resp, err := a.http.post(ctx, a.chatPath(), body)
	if err != nil {
		return nil, fmt.Errorf("azure openai chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure openai chat: %s", readErrorBody(resp))
	}

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("azure openai chat decode: %w", err)
	}
	return convertOpenAIResponse(&oaiResp), nil
}

func (a *AzureOpenAI) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := buildOpenAIRequestBody(req, a.deployment, true)
	delete(body, "model")

	resp, err := a.http.post(ctx, a.chatPath(), body)
	if err != nil {
		return nil, fmt.Errorf("azure openai stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("azure openai stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readOpenAISSEStream(resp, ch)
	}()
	return ch, nil
}
