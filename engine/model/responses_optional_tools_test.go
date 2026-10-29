package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildResponsesRequestBodyPreservesOptionalToolParameters(t *testing.T) {
	tests := []struct {
		name   string
		stream bool
	}{
		{name: "stream=false", stream: false},
		{name: "stream=true", stream: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":       map[string]any{"type": "string"},
					"start_line": map[string]any{"type": "integer"},
					"end_line":   map[string]any{"type": "integer"},
					"offset":     map[string]any{"type": "integer"},
					"limit":      map[string]any{"type": "integer"},
				},
				"required": []string{"path"},
			}
			before, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal original schema: %v", err)
			}
			req := &ChatRequest{
				Messages: []Message{{Role: RoleUser, Content: "Read a file"}},
				Tools: []ToolDefinition{{
					Type: "function",
					Function: FunctionDef{
						Name:        "file_read",
						Description: "Read a file with optional ranges",
						Parameters:  schema,
					},
				}},
			}

			body := buildResponsesRequestBody(req, "deployment", tt.stream)
			after, err := json.Marshal(schema)
			if err != nil {
				t.Fatalf("marshal schema after building request: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("original schema mutated: before = %s, after = %s", before, after)
			}

			tools, ok := body["tools"].([]map[string]any)
			if !ok || len(tools) != 1 {
				t.Fatalf("tools = %#v, want one function tool", body["tools"])
			}
			tool := tools[0]
			if tool["type"] != "function" || tool["name"] != "file_read" {
				t.Errorf("tool = %#v, want file_read function", tool)
			}
			if strict, ok := tool["strict"].(bool); !ok || strict {
				t.Errorf("tool.strict = %#v, want explicit boolean false", tool["strict"])
			}
			parameters, ok := tool["parameters"].(map[string]any)
			if !ok {
				t.Fatalf("tool.parameters = %#v, want map[string]any", tool["parameters"])
			}
			if !reflect.DeepEqual(parameters, schema) {
				t.Errorf("tool.parameters = %#v, want original schema %#v", parameters, schema)
			}
			if !reflect.DeepEqual(parameters["required"], []string{"path"}) {
				t.Errorf("tool.parameters.required = %#v, want [path] so ranges remain optional", parameters["required"])
			}
		})
	}
}
