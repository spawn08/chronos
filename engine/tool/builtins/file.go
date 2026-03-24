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
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			if p == "" {
				return nil, fmt.Errorf("file_read: 'path' argument is required")
			}
			resolved := resolvePath(basePath, p)
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
		Name:        "file_write",
		Description: "Write content to a file at the given path, creating directories as needed.",
		Permission:  tool.PermRequireApproval,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to write the file",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			content, _ := args["content"].(string)
			if p == "" {
				return nil, fmt.Errorf("file_write: 'path' argument is required")
			}
			resolved := resolvePath(basePath, p)
			if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
				return nil, fmt.Errorf("file_write: creating dirs: %w", err)
			}
			if err := os.WriteFile(resolved, []byte(content), 0644); err != nil {
				return nil, fmt.Errorf("file_write: %w", err)
			}
			return map[string]any{"path": resolved, "bytes_written": len(content)}, nil
		},
	}
}

// NewFileListTool creates a tool that lists files in a directory.
func NewFileListTool(basePath string) *tool.Definition {
	return &tool.Definition{
		Name:        "file_list",
		Description: "List files and directories at the given path.",
		Permission:  tool.PermAllow,
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
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			if p == "" {
				p = "."
			}
			resolved := resolvePath(basePath, p)
			entries, err := os.ReadDir(resolved)
			if err != nil {
				return nil, fmt.Errorf("file_list: %w", err)
			}
			items := make([]map[string]any, 0, len(entries))
			for _, e := range entries {
				info, _ := e.Info()
				item := map[string]any{
					"name":  e.Name(),
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
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			pattern, _ := args["pattern"].(string)
			if pattern == "" {
				return nil, fmt.Errorf("file_glob: 'pattern' argument is required")
			}
			resolved := resolvePath(basePath, pattern)
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
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			p, _ := args["path"].(string)
			pattern, _ := args["pattern"].(string)
			if p == "" || pattern == "" {
				return nil, fmt.Errorf("file_grep: 'path' and 'pattern' arguments are required")
			}
			resolved := resolvePath(basePath, p)
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
