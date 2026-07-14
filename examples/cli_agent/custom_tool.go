// Command custom_tool shows how to give a Chronos agent a CUSTOM tool with a
// real Go handler.
//
// Why this exists: custom tools declared in agents.yaml (a name + description
// that is not a built-in) are registered as no-op PLACEHOLDERS — the model can
// see them, but calling one just echoes its arguments (see buildToolFromConfig
// in sdk/agent/config.go). The supported way to attach real behavior is to
// register a tool.Definition programmatically in Go, as shown here.
//
// This program uses a deterministic mock provider, so it runs with NO API keys:
//
//	go run ./examples/cli_agent/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos CLI Agent — Custom Tool Wiring (Go)          ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// The SAME tool name declared as a placeholder in agents.yaml ("word_count")
	// gets a real handler here.
	wordCount := &tool.Definition{
		Name:        "word_count",
		Description: "Count the words in a piece of text",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{"type": "string", "description": "The text to count words in"},
			},
			"required": []any{"text"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			text, _ := args["text"].(string)
			return map[string]any{"words": len(strings.Fields(text))}, nil
		},
	}

	a, err := agent.New("repo-explorer", "Repository Explorer").
		WithModel(&countingMock{}).
		WithSystemPrompt("You help count words in text using the word_count tool.").
		AddTool(wordCount).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// 1. Call the tool directly through the agent's registry (no model involved).
	fmt.Println("\n━━━ Direct tool execution ━━━")
	res, err := a.Tools.Execute(ctx, "word_count", map[string]any{"text": "the quick brown fox"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  word_count(\"the quick brown fox\") = %v\n", res)

	// 2. Let the (mock) model drive a tool call through the full agent loop.
	fmt.Println("\n━━━ Model-driven tool call ━━━")
	resp, err := a.Chat(ctx, "How many words are in: a durable agent framework written in go")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  Assistant: %s\n", resp.Content)

	fmt.Println("\n✓ Custom tool wired in Go and invoked two ways.")
	fmt.Println("  Register this same agent from agents.yaml for CLI use, then attach")
	fmt.Println("  the real handler in Go as shown above.")
}

// countingMock is a deterministic provider: it requests word_count once, then
// reports the result. No network, no API key.
type countingMock struct{}

func (m *countingMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1]
	if last.Role == model.RoleTool && last.Name == "word_count" {
		var out struct {
			Words int `json:"words"`
		}
		_ = json.Unmarshal([]byte(last.Content), &out)
		return &model.ChatResponse{
			Role:       model.RoleAssistant,
			Content:    fmt.Sprintf("That text contains %d words.", out.Words),
			StopReason: model.StopReasonEnd,
		}, nil
	}

	// Extract the text after the colon and ask to count it.
	text := last.Content
	if i := strings.Index(text, ":"); i >= 0 {
		text = strings.TrimSpace(text[i+1:])
	}
	args, _ := json.Marshal(map[string]any{"text": text})
	return &model.ChatResponse{
		Role:       model.RoleAssistant,
		StopReason: model.StopReasonToolCall,
		ToolCalls:  []model.ToolCall{{ID: "call_1", Name: "word_count", Arguments: string(args)}},
	}, nil
}

func (m *countingMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *countingMock) Name() string  { return "counting-mock" }
func (m *countingMock) Model() string { return "mock-v1" }
