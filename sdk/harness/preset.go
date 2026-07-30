package harness

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage"
)

// DefaultDeepAgentSystemPrompt is the opinionated system prompt used when
// DeepAgentConfig.SystemPrompt is empty. It teaches the model to use the harness
// primitives the preset wires in: the plan, the virtual filesystem, and subagent
// delegation — and that older turns are compacted, so durable state belongs in the
// plan and files, not the conversation.
const DefaultDeepAgentSystemPrompt = `You are a capable agent that works on long, multi-step tasks.

You have a built-in harness — use it:
- PLAN: call update_plan to lay out the steps before you start, and keep it current — mark a step in_progress when you begin it and completed when it is done. Your current plan is always shown to you.
- OFFLOAD: write large intermediate artifacts (research notes, drafts, tool output) to the virtual filesystem with fs_write, and read them back by path with fs_read. Keep bulky content in files, not in your messages.
- DELEGATE: hand a self-contained sub-task to a subagent with spawn_subagent. It works in its own fresh context and returns only its result, keeping your context small.

Older conversation turns are summarized automatically as the context fills, so anything that must survive belongs in the plan or a file. Work methodically and finish the whole task.`

// defaultDeepAgentContext is the compaction policy applied when the caller does
// not supply one: summarize at 80% of the window and keep the six most recent
// turns verbatim.
func defaultDeepAgentContext() agent.ContextConfig {
	return agent.ContextConfig{SummarizeThreshold: 0.8, PreserveRecentTurns: 6}
}

// DeepAgentConfig configures NewDeepAgent. Only Model is required; every other
// field has a sensible default, and each capability can be overridden or turned
// off, so the preset is batteries-included but not a black box.
type DeepAgentConfig struct {
	// ID and Name identify the agent; they default to "deep-agent"/"Deep Agent".
	ID   string
	Name string

	// Model is the LLM provider driving the loop. Required.
	Model model.Provider

	// Storage, when set, makes the plan and virtual filesystem durable and
	// enables session compaction via Agent.ChatWithSession. It must implement
	// storage.SessionFileStore (sqlite and postgres do) to back the VFS. When nil,
	// the plan and VFS are in-memory (ephemeral) and only Agent.Chat is available.
	Storage storage.Storage

	// MemoryManager, when set, is attached so cross-session semantic recall
	// (WC-D-001) is available; recall is on by default when the manager has a
	// vector index.
	MemoryManager *memory.Manager

	// Broker, when set, receives plan-update stream events.
	Broker *stream.Broker

	// SystemPrompt overrides DefaultDeepAgentSystemPrompt.
	SystemPrompt string

	// Instructions are appended as additional system-level guidance.
	Instructions []string

	// SubAgents pre-registers named specialist templates the agent can select by
	// name via spawn_subagent (it can also define subagents dynamically).
	SubAgents []SubAgentSpec

	// MaxSubAgentDepth bounds subagent nesting; <= 0 uses DefaultMaxSubAgentDepth.
	MaxSubAgentDepth int

	// DisableSubAgents omits the spawn_subagent tool entirely.
	DisableSubAgents bool

	// SubAgentRunner overrides the execution strategy for subagents; nil uses an
	// in-process runner. Pass a QueuedRunner for durable, relocatable subagents.
	SubAgentRunner Runner

	// ExtraTools and ExtraToolkits add domain tools (e.g. web search, SQL) on top
	// of the harness primitives. Subagents may be granted these by name.
	ExtraTools    []*tool.Definition
	ExtraToolkits []*tool.Toolkit

	// Context overrides the compaction policy; nil uses defaultDeepAgentContext.
	// The active plan is pinned regardless, so it is never summarized away.
	Context *agent.ContextConfig
}

// NewDeepAgent assembles the full agent harness — planning (WC-A-001), a virtual
// filesystem (WC-A-002), context-isolated subagents (WC-A-003), automatic
// compaction with the plan pinned (WC-A-004), and semantic memory recall
// (WC-D-001) — into a single ready-to-run agent with a sensible default prompt
// and tool set. Everything is override-able through DeepAgentConfig.
//
// Use it with a Storage backend and Agent.ChatWithSession for the full durable,
// self-compacting experience:
//
//	a, _ := harness.NewDeepAgent(harness.DeepAgentConfig{Model: p, Storage: store})
//	resp, _ := a.ChatWithSession(ctx, "task-1", "Research X and write a report.")
func NewDeepAgent(cfg DeepAgentConfig) (*agent.Agent, error) {
	if cfg.Model == nil {
		return nil, fmt.Errorf("harness: NewDeepAgent requires a model")
	}
	id, name := cfg.ID, cfg.Name
	if id == "" {
		id = "deep-agent"
	}
	if name == "" {
		name = "Deep Agent"
	}

	// Plan store: durable when a storage backend is provided (StoragePlanStore
	// persists in Session.Metadata and works on any storage.Storage), else
	// in-memory.
	var planStore builtins.PlanStore
	var vfs builtins.VFS
	if cfg.Storage != nil {
		planStore = builtins.NewStoragePlanStore(cfg.Storage)
		sv, err := builtins.NewStorageVFS(cfg.Storage)
		if err != nil {
			return nil, fmt.Errorf("harness: deep agent virtual filesystem: %w", err)
		}
		vfs = sv
	} else {
		planStore = builtins.NewInMemoryPlanStore()
		vfs = builtins.NewInMemoryVFS()
	}

	ctxCfg := defaultDeepAgentContext()
	if cfg.Context != nil {
		ctxCfg = *cfg.Context
	}

	prompt := cfg.SystemPrompt
	if prompt == "" {
		prompt = DefaultDeepAgentSystemPrompt
	}

	b := agent.New(id, name).
		WithModel(cfg.Model).
		WithSystemPrompt(prompt).
		AddToolkit(builtins.NewPlanToolkit(planStore, cfg.Broker)).
		AddToolkit(builtins.NewVFSToolkit(vfs)).
		WithContextConfig(ctxCfg).
		// Pin the live plan so compaction never summarizes it away (WC-A-004 seam).
		WithContextPins(planPin(planStore))

	if cfg.Storage != nil {
		b = b.WithStorage(cfg.Storage)
	}
	if cfg.MemoryManager != nil {
		b = b.WithMemoryManager(cfg.MemoryManager)
	}
	if cfg.Broker != nil {
		b = b.WithBroker(cfg.Broker)
	}
	for _, inst := range cfg.Instructions {
		b = b.AddInstruction(inst)
	}
	for _, tk := range cfg.ExtraToolkits {
		b = b.AddToolkit(tk)
	}
	for _, td := range cfg.ExtraTools {
		b = b.AddTool(td)
	}

	a, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("harness: build deep agent: %w", err)
	}

	// Subagents are derived from the built parent (they inherit its model and draw
	// tools from its registry) and the spawn tool is attached afterward — matching
	// the documented Attach flow and keeping sdk/agent free of a harness import.
	if !cfg.DisableSubAgents {
		svc, svcErr := NewSubAgentService(a, WithMaxDepth(cfg.MaxSubAgentDepth))
		if svcErr != nil {
			return nil, fmt.Errorf("harness: deep agent subagents: %w", svcErr)
		}
		for _, spec := range cfg.SubAgents {
			if regErr := svc.Register(spec); regErr != nil {
				return nil, fmt.Errorf("harness: register subagent %q: %w", spec.Name, regErr)
			}
		}
		runner := cfg.SubAgentRunner
		if runner == nil {
			runner = NewInProcessRunner(svc)
		}
		Attach(svc, runner)
	}

	return a, nil
}

// planPin returns a dynamic context pin that renders the current plan as a system
// message every turn. Pinned content is never summarized by compaction, so the
// plan always stays in view. It degrades to no pin when there is no active session
// (plain Chat) or no plan yet, so it is safe on every path.
func planPin(store builtins.PlanStore) func(ctx context.Context) []model.Message {
	return func(ctx context.Context) []model.Message {
		plan, err := store.Load(ctx)
		if err != nil || plan == nil || len(plan.Tasks) == 0 {
			return nil
		}
		return []model.Message{{
			Role:    model.RoleSystem,
			Content: "Current plan (keep it updated with update_plan):\n" + plan.Summary(),
		}}
	}
}
