package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Gemini implements Provider for Google's Gemini API.
type Gemini struct {
	config ProviderConfig
	http   *httpClient
}

// NewGemini creates a new Gemini provider with the given API key.
func NewGemini(apiKey string) *Gemini {
	return NewGeminiWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		Model:   "gemini-2.0-flash",
	})
}

// NewGeminiWithConfig creates a Gemini provider with full configuration.
func NewGeminiWithConfig(cfg ProviderConfig) *Gemini {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-2.0-flash"
	}
	return &Gemini{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, nil),
	}
}

func (g *Gemini) Name() string  { return "gemini" }
func (g *Gemini) Model() string { return g.config.Model }

func (g *Gemini) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	modelID := req.Model
	if modelID == "" {
		modelID = g.config.Model
	}

	body := g.buildRequestBody(req)
	path := fmt.Sprintf("/models/%s:generateContent?key=%s", modelID, g.config.APIKey)

	resp, err := g.http.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("gemini chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini chat: %s", readErrorBody(resp))
	}

	var raw geminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("gemini chat decode: %w", err)
	}
	return g.convertResponse(&raw), nil
}

func (g *Gemini) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	modelID := req.Model
	if modelID == "" {
		modelID = g.config.Model
	}

	body := g.buildRequestBody(req)
	path := fmt.Sprintf("/models/%s:streamGenerateContent?key=%s&alt=sse", modelID, g.config.APIKey)

	resp, err := g.http.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("gemini stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		g.readSSEStream(resp, ch)
	}()
	return ch, nil
}

func (g *Gemini) buildRequestBody(req *ChatRequest) map[string]any {
	var systemInstruction map[string]any
	contents := make([]map[string]any, 0, len(req.Messages))

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			systemInstruction = map[string]any{
				"parts": []map[string]string{{"text": m.Content}},
			}
			continue
		}

		role := m.Role
		if role == RoleAssistant {
			role = "model"
		}

		if m.Role == RoleTool {
			contents = append(contents, map[string]any{
				"role": "function",
				"parts": []map[string]any{{
					"functionResponse": map[string]any{
						"name":     m.Name,
						"response": map[string]string{"result": m.Content},
					},
				}},
			})
			continue
		}

		parts := []map[string]any{{"text": m.Content}}
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				var args any
				_ = json.Unmarshal([]byte(tc.Arguments), &args)
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Name,
						"args": args,
					},
				})
			}
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": parts,
		})
	}

	body := map[string]any{
		"contents": contents,
	}
	if systemInstruction != nil {
		body["systemInstruction"] = systemInstruction
	}

	genConfig := map[string]any{}
	if req.MaxTokens > 0 {
		genConfig["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		genConfig["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		genConfig["topP"] = req.TopP
	}
	if len(req.Stop) > 0 {
		genConfig["stopSequences"] = req.Stop
	}
	if req.ResponseFormat == "json_object" {
		genConfig["responseMimeType"] = "application/json"
	}
	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	if len(req.Tools) > 0 {
		funcDecls := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			funcDecls[i] = map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			}
		}
		body["tools"] = []map[string]any{{
			"functionDeclarations": funcDecls,
		}}
	}

	return body
}

func (g *Gemini) convertResponse(raw *geminiResponse) *ChatResponse {
	cr := &ChatResponse{Role: RoleAssistant}

	if raw.UsageMetadata != nil {
		cr.Usage = Usage{
			PromptTokens:     raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
		}
	}

	if len(raw.Candidates) == 0 {
		return cr
	}

	candidate := raw.Candidates[0]
	var textParts []string
	for _, part := range candidate.Content.Parts {
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			cr.ToolCalls = append(cr.ToolCalls, ToolCall{
				ID:        fmt.Sprintf("call_%s", part.FunctionCall.Name),
				Name:      part.FunctionCall.Name,
				Arguments: string(argsJSON),
			})
		}
	}
	cr.Content = strings.Join(textParts, "")

	switch candidate.FinishReason {
	case "MAX_TOKENS":
		cr.StopReason = StopReasonMaxTokens
	case "SAFETY":
		cr.StopReason = StopReasonFilter
	default:
		if len(cr.ToolCalls) > 0 {
			cr.StopReason = StopReasonToolCall
		} else {
			cr.StopReason = StopReasonEnd
		}
	}
	return cr
}

func (g *Gemini) readSSEStream(resp *http.Response, ch chan<- *ChatResponse) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var raw geminiResponse
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}
		cr := g.convertResponse(&raw)
		cr.Delta = true
		ch <- cr
	}
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text         string `json:"text,omitempty"`
				FunctionCall *struct {
					Name string `json:"name"`
					Args any    `json:"args"`
				} `json:"functionCall,omitempty"`
			} `json:"parts"`
			Role string `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}
