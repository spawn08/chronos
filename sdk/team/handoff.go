package team

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

// HandoffConfig configures a handoff tool for a specific target agent.
type HandoffConfig struct {
	TargetAgent  *agent.Agent
	Description  string
	Instructions string
}

// NewHandoffTool creates a tool that hands off conversation to another agent.
// The calling agent can invoke this tool to delegate work to the target agent
// with specific instructions.
func NewHandoffTool(cfg HandoffConfig) *tool.Definition {
	name := fmt.Sprintf("transfer_to_%s", cfg.TargetAgent.ID)
	desc := cfg.Description
	if desc == "" {
		desc = fmt.Sprintf("Transfer the conversation to %s (%s)", cfg.TargetAgent.Name, cfg.TargetAgent.ID)
	}

	return &tool.Definition{
		Name:        name,
		Description: desc,
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "Context and instructions to pass to the target agent",
				},
			},
			"required": []string{"message"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			message, _ := args["message"].(string)
			if message == "" {
				message = "Please continue the conversation."
			}

			prompt := message
			if cfg.Instructions != "" {
				prompt = cfg.Instructions + "\n\nUser request: " + message
			}

			resp, err := cfg.TargetAgent.Chat(ctx, prompt)
			if err != nil {
				return nil, fmt.Errorf("handoff to %s: %w", cfg.TargetAgent.ID, err)
			}

			return map[string]any{
				"agent_id":   cfg.TargetAgent.ID,
				"agent_name": cfg.TargetAgent.Name,
				"response":   resp.Content,
			}, nil
		},
	}
}

// CreateHandoffTools generates transfer tools for a set of agents.
// Each agent gets a tool to transfer to every other agent.
func CreateHandoffTools(agents []*agent.Agent) map[string][]*tool.Definition {
	tools := make(map[string][]*tool.Definition)
	for _, a := range agents {
		for _, target := range agents {
			if a.ID == target.ID {
				continue
			}
			t := NewHandoffTool(HandoffConfig{
				TargetAgent: target,
			})
			tools[a.ID] = append(tools[a.ID], t)
		}
	}
	return tools
}

// HandoffResult extracts the handoff result from a tool call response.
func HandoffResult(result any) (agentID, response string, err error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", "", fmt.Errorf("marshal handoff result: %w", err)
	}
	var r struct {
		AgentID  string `json:"agent_id"`
		Response string `json:"response"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", "", fmt.Errorf("unmarshal handoff result: %w", err)
	}
	return r.AgentID, r.Response, nil
}
