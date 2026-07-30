package a2a

import (
	"context"
	"fmt"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// defaultRemotePoll is how often the remote-agent tool polls task status.
const defaultRemotePoll = 500 * time.Millisecond

// RemoteToolOption configures a remote-agent tool.
type RemoteToolOption func(*remoteToolConfig)

type remoteToolConfig struct {
	poll       time.Duration
	permission tool.Permission
}

// WithPollInterval sets how often the tool polls the remote task for completion.
func WithPollInterval(d time.Duration) RemoteToolOption {
	return func(c *remoteToolConfig) {
		if d > 0 {
			c.poll = d
		}
	}
}

// WithPermission overrides the tool's permission (default PermAllow). Use
// PermRequireApproval to gate outbound delegation to an external agent behind
// human approval.
func WithPermission(p tool.Permission) RemoteToolOption {
	return func(c *remoteToolConfig) { c.permission = p }
}

// awaitRemote waits for a remote task to reach a terminal state, preferring the
// server's streamed updates and falling back to polling when streaming is
// unavailable (e.g. an older peer without the stream endpoint).
func awaitRemote(ctx context.Context, client *Client, taskID string, poll time.Duration) (*Task, error) {
	// Cancel the stream on any return so the StreamTask goroutine cannot block on
	// an unread send (e.g. a peer that keeps sending past the terminal snapshot).
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	tasks, errs := client.StreamTask(streamCtx, taskID)
	var last *Task
	for t := range tasks {
		snap := t
		last = &snap
		if isTerminal(snap.Status) {
			return last, nil
		}
	}
	// Stream ended without a terminal snapshot: surface a stream error as a poll
	// fallback, or poll to confirm the final state.
	if err := <-errs; err != nil {
		return client.WaitForCompletion(ctx, taskID, poll)
	}
	if last != nil && isTerminal(last.Status) {
		return last, nil
	}
	return client.WaitForCompletion(ctx, taskID, poll)
}

// NewRemoteAgentTool adapts a remote A2A agent into a Chronos tool so an agent
// can delegate a sub-task to it — the same way it would spawn a local subagent
// (composes with the harness spawn/subagent model). The tool submits the task,
// waits for the remote agent to finish, and returns only its final output, so
// the remote agent's intermediate work never enters the caller's context.
//
// name/description are what the calling model sees; client points at the remote
// A2A endpoint.
func NewRemoteAgentTool(name, description string, client *Client, opts ...RemoteToolOption) *tool.Definition {
	cfg := remoteToolConfig{poll: defaultRemotePoll, permission: tool.PermAllow}
	for _, opt := range opts {
		opt(&cfg)
	}

	return &tool.Definition{
		Name:        name,
		Description: description,
		Permission:  cfg.permission,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "The task to delegate to the remote agent.",
				},
			},
			"required": []any{"task"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			input, _ := args["task"].(string)
			if input == "" {
				return nil, fmt.Errorf("%s: 'task' must be a non-empty string", name)
			}

			created, err := client.CreateTask(ctx, input, nil)
			if err != nil {
				return nil, fmt.Errorf("%s: create remote task: %w", name, err)
			}
			done, err := awaitRemote(ctx, client, created.ID, cfg.poll)
			if err != nil {
				return nil, fmt.Errorf("%s: await remote task: %w", name, err)
			}
			switch done.Status {
			case TaskStatusCompleted:
				return map[string]any{"agent": name, "result": done.Output}, nil
			case TaskStatusFailed:
				return nil, fmt.Errorf("%s: remote task failed: %s", name, done.Error)
			default: // canceled
				return nil, fmt.Errorf("%s: remote task ended %s", name, done.Status)
			}
		},
	}
}
