package builtins

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/tool"
)

// VFS tool names.
const (
	FSWriteToolName  = "fs_write"
	FSReadToolName   = "fs_read"
	FSListToolName   = "fs_ls"
	FSDeleteToolName = "fs_delete"
)

// MaxArtifactBytes caps the size of a single artifact written through fs_write.
// The VFS exists to offload large artifacts, but an unbounded write would let a
// misbehaving agent bloat the backing table (and later load the whole blob back
// into memory), so writes above this generous text-artifact limit are rejected.
const MaxArtifactBytes = 5 << 20 // 5 MiB

// NewFSWriteTool returns a tool that saves an artifact to the virtual filesystem
// instead of the context window. It returns only the path and byte count — never
// the content — so writing a large artifact costs almost nothing in tokens.
func NewFSWriteTool(vfs VFS) *tool.Definition {
	return &tool.Definition{
		Name: FSWriteToolName,
		Description: "Save a text artifact (notes, drafts, long tool output) to scratch storage under a path, " +
			"instead of keeping it in the conversation. Returns only the path and size — read it back later with fs_read. " +
			"Use this to keep your context small on long tasks.",
		Permission: tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "Where to store the artifact, e.g. 'research/notes.md'."},
				"content": map[string]any{"type": "string", "description": "The artifact text to store."},
			},
			"required": []any{"path", "content"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			path, ok := args["path"].(string)
			if !ok {
				return nil, fmt.Errorf("%s: 'path' must be a string", FSWriteToolName)
			}
			content, ok := args["content"].(string)
			if !ok {
				return nil, fmt.Errorf("%s: 'content' must be a string", FSWriteToolName)
			}
			if len(content) > MaxArtifactBytes {
				return nil, fmt.Errorf("%s: artifact is %d bytes, exceeds the %d-byte limit", FSWriteToolName, len(content), MaxArtifactBytes)
			}
			if err := vfs.Write(ctx, path, []byte(content)); err != nil {
				return nil, fmt.Errorf("%s: %w", FSWriteToolName, err)
			}
			return map[string]any{"path": path, "bytes_written": len(content)}, nil
		},
	}
}

// NewFSReadTool returns a tool that pages an artifact back into context by path.
func NewFSReadTool(vfs VFS) *tool.Definition {
	return &tool.Definition{
		Name:        FSReadToolName,
		Description: "Read back a text artifact previously saved with fs_write, by its path.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path of the artifact to read."},
			},
			"required": []any{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			path, _ := args["path"].(string)
			content, err := vfs.Read(ctx, path)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", FSReadToolName, err)
			}
			return map[string]any{"path": path, "content": string(content)}, nil
		},
	}
}

// NewFSListTool returns a tool that lists stored artifacts (metadata only, no
// content) so the model can discover what it has offloaded.
func NewFSListTool(vfs VFS) *tool.Definition {
	return &tool.Definition{
		Name:        FSListToolName,
		Description: "List saved artifacts (path and size only) under an optional path prefix.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prefix": map[string]any{"type": "string", "description": "Optional path prefix to filter by; omit to list all."},
			},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			prefix, _ := args["prefix"].(string)
			files, err := vfs.List(ctx, prefix)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", FSListToolName, err)
			}
			entries := make([]map[string]any, 0, len(files))
			for _, f := range files {
				entries = append(entries, map[string]any{"path": f.Path, "size": f.Size})
			}
			return map[string]any{"files": entries}, nil
		},
	}
}

// NewFSDeleteTool returns a tool that deletes a stored artifact.
func NewFSDeleteTool(vfs VFS) *tool.Definition {
	return &tool.Definition{
		Name:        FSDeleteToolName,
		Description: "Delete a saved artifact by path.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path of the artifact to delete."},
			},
			"required": []any{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			path, _ := args["path"].(string)
			if err := vfs.Delete(ctx, path); err != nil {
				return nil, fmt.Errorf("%s: %w", FSDeleteToolName, err)
			}
			return map[string]any{"path": path, "deleted": true}, nil
		},
	}
}

// NewVFSToolkit bundles the virtual-filesystem tools (fs_write, fs_read, fs_ls,
// fs_delete) so an agent gains context-offloading scratch space with
// agent.New(...).AddToolkit(builtins.NewVFSToolkit(vfs)).
func NewVFSToolkit(vfs VFS) *tool.Toolkit {
	tk := tool.NewToolkit("virtual_fs", "Session-scoped scratch filesystem for offloading large artifacts out of context.")
	tk.Add(NewFSWriteTool(vfs))
	tk.Add(NewFSReadTool(vfs))
	tk.Add(NewFSListTool(vfs))
	tk.Add(NewFSDeleteTool(vfs))
	return tk
}
