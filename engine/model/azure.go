package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// AzureOpenAI implements Provider for Azure-hosted OpenAI models. Standard
// requests use the deployment-scoped Chat Completions endpoint; native
// reasoning requests use /openai/v1/responses so reasoning and tools can be
// combined without the Chat Completions API restriction.
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
		http:       newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers, withMaxRetries(cfg.MaxRetries)),
	}
}

func (a *AzureOpenAI) Name() string  { return "azure" }
func (a *AzureOpenAI) Model() string { return a.deployment }

// usesV1API reports whether this resource's configured api-version selects
// Azure's newer unified "v1" surface (docs call the version string
// "preview") rather than the classic dated api-versions. The v1 surface
// serves every deployment from one path with the deployment name in the
// request body's "model" field; the classic surface scopes the deployment
// into the URL path itself and 404s if a "preview"-style api-version is
// sent to it, so the two must not be mixed.
func (a *AzureOpenAI) usesV1API() bool {
	return a.apiVersion == "preview"
}

func (a *AzureOpenAI) chatPath() string {
	if a.usesV1API() {
		return fmt.Sprintf("/openai/v1/chat/completions?api-version=%s", a.apiVersion)
	}
	return fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=%s", a.deployment, a.apiVersion)
}

func (a *AzureOpenAI) responsesPath() string { return "/openai/v1/responses" }

func (a *AzureOpenAI) usesResponsesAPI(req *ChatRequest) bool {
	return req != nil && nativeReasoningEnabled(req.Reasoning)
}

func azureReasoningToolConflict(status int, body string, req *ChatRequest) bool {
	return status == http.StatusBadRequest && req != nil && len(req.Tools) > 0 &&
		strings.Contains(strings.ToLower(body), "function tools with reasoning_effort are not supported")
}

func (a *AzureOpenAI) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if a.usesResponsesAPI(req) {
		return a.responsesChat(ctx, req)
	}

	body := buildOpenAIRequestBody(req, a.deployment, false)
	if !a.usesV1API() {
		delete(body, "model") // classic surface uses the deployment name in the URL
	}

	resp, err := a.http.post(ctx, a.chatPath(), body)
	if err != nil {
		return nil, fmt.Errorf("azure openai chat: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		drainAndClose(resp.Body)
		if azureReasoningToolConflict(resp.StatusCode, errMsg, req) {
			return a.responsesChat(ctx, req)
		}
		return nil, fmt.Errorf("azure openai chat: %s", errMsg)
	}
	defer drainAndClose(resp.Body)

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("azure openai chat decode: %w", err)
	}
	return convertOpenAIResponse(&oaiResp), nil
}

func (a *AzureOpenAI) responsesChat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	resp, err := a.http.post(ctx, a.responsesPath(), buildResponsesRequestBody(req, a.deployment, false))
	if err != nil {
		return nil, fmt.Errorf("azure openai responses: %w", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure openai responses: %s", readErrorBody(resp))
	}

	var raw responsesAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("azure openai responses decode: %w", err)
	}
	if err := responsesError(&raw); err != nil {
		return nil, fmt.Errorf("azure openai responses: %w", err)
	}
	return convertResponsesResponse(&raw), nil
}

func (a *AzureOpenAI) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	responsesMode := a.usesResponsesAPI(req)
	path := a.chatPath()
	body := buildOpenAIRequestBody(req, a.deployment, true)
	// Azure Chat Completions follows the OpenAI streaming contract: usage is
	// delivered in a final, choices-free SSE chunk only when requested. Keep
	// the request aligned with OpenAI so clients can report token usage for
	// streamed Azure turns.
	body["stream_options"] = map[string]any{"include_usage": true}
	if !a.usesV1API() {
		delete(body, "model") // classic surface uses the deployment name in the URL
	}
	if responsesMode {
		path = a.responsesPath()
		body = buildResponsesRequestBody(req, a.deployment, true)
	}

	var resp *http.Response
	for {
		var err error
		resp, err = a.http.postStream(ctx, path, body)
		if err != nil {
			return nil, fmt.Errorf("azure openai stream: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			break
		}
		errMsg := readErrorBody(resp)
		drainAndClose(resp.Body)
		if !responsesMode && azureReasoningToolConflict(resp.StatusCode, errMsg, req) {
			responsesMode = true
			path = a.responsesPath()
			body = buildResponsesRequestBody(req, a.deployment, true)
			continue
		}
		return nil, fmt.Errorf("azure openai stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		if responsesMode {
			readResponsesSSEStream(ctx, resp, ch)
			return
		}
		readOpenAISSEStream(ctx, resp, ch)
	}()
	return ch, nil
}
