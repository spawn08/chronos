package builtins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spawn08/chronos/engine/tool"
)

// NewFileReadTool creates a tool that reads file contents.
func NewFileReadTool(basePath string) *tool.Definition {
	return &tool.Definition{
		Name:        "file_read",
		Description: "Read the contents of a file at the given path.",
		Permission:  tool.PermAllow,
		Effects:     []tool.Effect{tool.EffectRead},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file to read",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			if p == "" {
				return nil, fmt.Errorf("file_read: 'path' argument is required")
			}
			resolved, err := resolveWorkspacePath(ctx, basePath, p)
			if err != nil {
				return nil, fmt.Errorf("file_read: %w", err)
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return nil, fmt.Errorf("file_read: %w", err)
			}
			return map[string]any{"content": string(data), "path": resolved}, nil
		},
	}
}

// NewFileWriteTool creates a tool that writes content to a file.
func NewFileWriteTool(basePath string) *tool.Definition {
	return &tool.Definition{
		Name: "file_write",
		Description: "Create or modify a file. To edit an existing file, pass old_content (exact text currently in the file, " +
			"including whitespace, unique unless replace_all is true) and new_content; prefer this for changes so each call stays small. " +
			"To create a file or replace it entirely, pass content instead. Directories are created as needed.",
		Permission: tool.PermRequireApproval,
		Effects:    []tool.Effect{tool.EffectScratchWrite, tool.EffectDeliveryWrite},
		ResolveEffects: func(ctx context.Context, _ map[string]any) ([]tool.Effect, error) {
			if tool.IsScratchWorkspace(ctx) {
				return []tool.Effect{tool.EffectScratchWrite}, nil
			}
			return []tool.Effect{tool.EffectDeliveryWrite}, nil
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to write the file",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Full file content; creates or overwrites the file. Omit when using old_content/new_content.",
				},
				"old_content": map[string]any{
					"type":        "string",
					"description": "Exact existing text to replace in the file.",
				},
				"new_content": map[string]any{
					"type":        "string",
					"description": "Replacement text for old_content (may be empty to delete it).",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "Replace every occurrence of old_content instead of requiring exactly one.",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			if p == "" {
				return nil, fmt.Errorf("file_write: 'path' argument is required")
			}
			resolved, err := resolveWorkspacePath(ctx, basePath, p)
			if err != nil {
				return nil, fmt.Errorf("file_write: %w", err)
			}
			if _, editing := args["old_content"]; editing {
				return editFile(resolved, args)
			}
			content, ok := args["content"].(string)
			if !ok {
				return nil, fmt.Errorf("file_write: pass 'content' to write the whole file, or 'old_content' and 'new_content' to edit it")
			}
			if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
				return nil, fmt.Errorf("file_write: creating dirs: %w", err)
			}
			if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
				return nil, fmt.Errorf("file_write: %w", err)
			}
			return map[string]any{"path": resolved, "bytes_written": len(content)}, nil
		},
	}
}

// editFile replaces old_content with new_content in an existing file. The
// match must be unique unless replace_all is set, so an ambiguous edit fails
// instead of changing the wrong occurrence.
func editFile(resolved string, args map[string]any) (any, error) {
	oldContent, _ := args["old_content"].(string)
	newContent, hasNew := args["new_content"].(string)
	replaceAll, _ := args["replace_all"].(bool)
	if oldContent == "" {
		return nil, fmt.Errorf("file_write: 'old_content' must be non-empty; pass 'content' to create or overwrite a file")
	}
	if !hasNew {
		return nil, fmt.Errorf("file_write: 'new_content' is required with 'old_content'")
	}
	if oldContent == newContent {
		return nil, fmt.Errorf("file_write: 'old_content' and 'new_content' are identical; nothing to change")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file_write: %s does not exist; pass 'content' to create it", resolved)
		}
		return nil, fmt.Errorf("file_write: %w", err)
	}
	current := string(data)
	count := strings.Count(current, oldContent)
	switch {
	case count == 0:
		return nil, fmt.Errorf("file_write: old_content not found in %s; read the file again and copy the exact current text, including whitespace", resolved)
	case count > 1 && !replaceAll:
		return nil, fmt.Errorf("file_write: old_content matches %d places in %s; include more surrounding lines to make it unique, or set replace_all", count, resolved)
	}
	updated := strings.Replace(current, oldContent, newContent, 1)
	if replaceAll {
		updated = strings.ReplaceAll(current, oldContent, newContent)
	}
	if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
		return nil, fmt.Errorf("file_write: %w", err)
	}
	replaced := 1
	if replaceAll {
		replaced = count
	}
	return map[string]any{"path": resolved, "bytes_written": len(updated), "replacements": replaced}, nil
}

// NewFileListTool creates a tool that lists files in a directory.
func NewFileListTool(basePath string) *tool.Definition {
	return &tool.Definition{
		Name:        "file_list",
		Description: "List files and directories at the given path.",
		Permission:  tool.PermAllow,
		Effects:     []tool.Effect{tool.EffectRead},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory path to list",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			if p == "" {
				p = "."
			}
			resolved, err := resolveWorkspacePath(ctx, basePath, p)
			if err != nil {
				return nil, fmt.Errorf("file_list: %w", err)
			}
			entries, err := os.ReadDir(resolved)
			if err != nil {
				return nil, fmt.Errorf("file_list: %w", err)
			}
			items := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				info, _ := e.Info()
				item := map[string]any{
					"name":   e.Name(),
					"is_dir": e.IsDir(),
				}
				if info != nil {
					item["size"] = info.Size()
				}
				items = append(items, item)
			}
			return map[string]any{"path": resolved, "entries": items}, nil
		},
	}
}

// NewFileGlobTool creates a tool that matches files using glob patterns.
func NewFileGlobTool(basePath string) *tool.Definition {
	return &tool.Definition{
		Name:        "file_glob",
		Description: "Find files matching a glob pattern.",
		Permission:  tool.PermAllow,
		Effects:     []tool.Effect{tool.EffectRead},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern to match (e.g. '*.go', 'src/**/*.ts')",
				},
			},
			"required": []string{"pattern"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return nil, fmt.Errorf("file_glob: 'pattern' argument is required")
			}
			resolved, err := resolveWorkspacePath(ctx, basePath, pattern)
			if err != nil {
				return nil, fmt.Errorf("file_glob: %w", err)
			}
			matches, err := filepath.Glob(resolved)
			if err != nil {
				return nil, fmt.Errorf("file_glob: %w", err)
			}
			return map[string]any{"pattern": pattern, "matches": matches}, nil
		},
	}
}

// NewFileGrepTool creates a tool that searches file contents for a pattern.
func NewFileGrepTool(basePath string) *tool.Definition {
	return &tool.Definition{
		Name:        "file_grep",
		Description: "Search for a text pattern in a file, returning matching lines.",
		Permission:  tool.PermAllow,
		Effects:     []tool.Effect{tool.EffectRead},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path to search in",
				},
				"pattern": map[string]any{
					"type":        "string",
					"description": "Text pattern to search for",
				},
			},
			"required": []string{"path", "pattern"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			pattern, _ := args["pattern"].(string)
			if p == "" || pattern == "" {
				return nil, fmt.Errorf("file_grep: 'path' and 'pattern' arguments are required")
			}
			resolved, err := resolveWorkspacePath(ctx, basePath, p)
			if err != nil {
				return nil, fmt.Errorf("file_grep: %w", err)
			}
			data, err := os.ReadFile(resolved)
			if err != nil {
				return nil, fmt.Errorf("file_grep: %w", err)
			}
			lines := strings.Split(string(data), "\n")
			var matches []map[string]any
			for i, line := range lines {
				if strings.Contains(line, pattern) {
					matches = append(matches, map[string]any{
						"line_number": i + 1,
						"content":     line,
					})
				}
			}
			return map[string]any{"path": resolved, "pattern": pattern, "matches": matches}, nil
		},
	}
}

// NewFileToolkit creates a toolkit with all file tools.
func NewFileToolkit(basePath string) *tool.Toolkit {
	tk := tool.NewToolkit("file_tools", "File system operations: read, write, list, glob, grep")
	tk.Add(NewFileReadTool(basePath))
	tk.Add(NewFileWriteTool(basePath))
	tk.Add(NewFileListTool(basePath))
	tk.Add(NewFileGlobTool(basePath))
	tk.Add(NewFileGrepTool(basePath))
	return tk
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if base != "" {
		return filepath.Join(base, path)
	}
	return path
}
