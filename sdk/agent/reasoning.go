package agent

import (
	"fmt"
	"strings"

	"github.com/spawn08/chronos/engine/model"
)

// ReasoningStrategy defines the chain-of-thought approach.
type ReasoningStrategy int

const (
	ReasoningNone       ReasoningStrategy = iota
	ReasoningCoT                          // chain-of-thought: "Let's think step by step"
	ReasoningReflection                   // think, then critique
)

// WithReasoning configures the agent's reasoning strategy.
func (b *Builder) WithReasoning(strategy ReasoningStrategy) *Builder {
	b.agent.Reasoning = strategy
	return b
}

// applyReasoning modifies the messages to include reasoning prompts.
func applyReasoning(strategy ReasoningStrategy, messages []model.Message) []model.Message {
	switch strategy {
	case ReasoningCoT:
		messages = append(messages, model.Message{
			Role:    model.RoleSystem,
			Content: "Before answering, think through the problem step by step. Show your reasoning process, then provide your final answer.",
		})
	case ReasoningReflection:
		messages = append(messages, model.Message{
			Role:    model.RoleSystem,
			Content: "Follow this reasoning protocol:\n1. THINK: Analyze the problem step by step\n2. CRITIQUE: Review your reasoning for errors or gaps\n3. ANSWER: Provide your final, refined answer\n\nFormat your response with <think>, <critique>, and <answer> sections.",
		})
	}
	return messages
}

// ExtractReasoningParts parses a reflection-style response into components.
func ExtractReasoningParts(content string) map[string]string {
	parts := map[string]string{
		"think":    "",
		"critique": "",
		"answer":   "",
	}

	tags := []string{"think", "critique", "answer"}
	for _, tag := range tags {
		start := fmt.Sprintf("<%s>", tag)
		end := fmt.Sprintf("</%s>", tag)
		si := strings.Index(content, start)
		ei := strings.Index(content, end)
		if si >= 0 && ei > si {
			parts[tag] = strings.TrimSpace(content[si+len(start) : ei])
		}
	}

	return parts
}
