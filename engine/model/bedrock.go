package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Bedrock implements Provider for AWS Bedrock's InvokeModel API.
// Supports Claude, Titan, Llama, and other models hosted on Bedrock.
type Bedrock struct {
	config ProviderConfig
	region string
	http   *httpClient
}

// NewBedrock creates a Bedrock provider.
// region is the AWS region (e.g., "us-east-1").
// accessKey and secretKey are AWS credentials.
// modelID is the Bedrock model identifier (e.g., "anthropic.claude-3-sonnet-20240229-v1:0").
func NewBedrock(region, accessKey, secretKey, modelID string) *Bedrock {
	baseURL := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
	return NewBedrockWithConfig(region, ProviderConfig{
		APIKey:  accessKey,
		BaseURL: baseURL,
		Model:   modelID,
	}, secretKey)
}

// NewBedrockWithConfig creates a Bedrock provider with full configuration.
func NewBedrockWithConfig(region string, cfg ProviderConfig, secretKey string) *Bedrock {
	if cfg.BaseURL == "" {
		cfg.BaseURL = fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", region)
	}
	if cfg.Model == "" {
		cfg.Model = "anthropic.claude-3-sonnet-20240229-v1:0"
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	// Note: In production, use AWS SigV4 signing. This simplified version
	// uses bearer token auth for Bedrock endpoints behind API Gateway.
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	return &Bedrock{
		config: cfg,
		region: region,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (b *Bedrock) Name() string  { return "bedrock" }
func (b *Bedrock) Model() string { return b.config.Model }

func (b *Bedrock) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := b.buildRequestBody(req)
	path := fmt.Sprintf("/model/%s/invoke", b.config.Model)

	resp, err := b.http.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("bedrock chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bedrock chat: %s", readErrorBody(resp))
	}

	var raw bedrockResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("bedrock chat decode: %w", err)
	}
	return b.convertResponse(&raw), nil
}

func (b *Bedrock) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := b.buildRequestBody(req)
	path := fmt.Sprintf("/model/%s/invoke-with-response-stream", b.config.Model)

	resp, err := b.http.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("bedrock stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("bedrock stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var event bedrockStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Delta.Text != "" {
				ch <- &ChatResponse{
					Content: event.Delta.Text,
					Role:    RoleAssistant,
					Delta:   true,
				}
			}
		}
	}()

	return ch, nil
}

func (b *Bedrock) buildRequestBody(req *ChatRequest) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))
	var systemPrompt string

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemPrompt = m.Content
			continue
		}
		messages = append(messages, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	body := map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"messages":          messages,
		"max_tokens":        req.MaxTokens,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens <= 0 {
		body["max_tokens"] = 4096
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": t.Function.Parameters,
			}
		}
		body["tools"] = tools
	}

	return body
}

type bedrockResponse struct {
	ID      string `json:"id"`
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input any    `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type bedrockStreamEvent struct {
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

func (b *Bedrock) convertResponse(raw *bedrockResponse) *ChatResponse {
	resp := &ChatResponse{
		ID:   raw.ID,
		Role: RoleAssistant,
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
		},
	}

	for _, c := range raw.Content {
		switch c.Type {
		case "text":
			resp.Content += c.Text
		case "tool_use":
			args, _ := json.Marshal(c.Input)
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: string(args),
			})
		}
	}

	switch raw.StopReason {
	case "end_turn":
		resp.StopReason = StopReasonEnd
	case "max_tokens":
		resp.StopReason = StopReasonMaxTokens
	case "tool_use":
		resp.StopReason = StopReasonToolCall
	}

	return resp
}
