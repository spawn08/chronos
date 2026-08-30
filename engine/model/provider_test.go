package model

import "testing"

func TestJSONSchemaFromMetadata(t *testing.T) {
	schema := map[string]any{"type": "object"}

	tests := []struct {
		name    string
		req     *ChatRequest
		wantOK  bool
		wantSet bool
	}{
		{
			name:    "json_schema with a schema present",
			req:     &ChatRequest{ResponseFormat: "json_schema", Metadata: map[string]any{"json_schema": schema}},
			wantOK:  true,
			wantSet: true,
		},
		{
			name:   "json_schema with no schema in metadata",
			req:    &ChatRequest{ResponseFormat: "json_schema"},
			wantOK: false,
		},
		{
			name:   "json_schema with wrong-typed metadata value",
			req:    &ChatRequest{ResponseFormat: "json_schema", Metadata: map[string]any{"json_schema": "not a map"}},
			wantOK: false,
		},
		{
			name:   "json_object mode never returns a schema, even if one is set",
			req:    &ChatRequest{ResponseFormat: "json_object", Metadata: map[string]any{"json_schema": schema}},
			wantOK: false,
		},
		{
			name:   "no response format set",
			req:    &ChatRequest{},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := jsonSchemaFromMetadata(tt.req)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantSet && got["type"] != "object" {
				t.Errorf("schema = %#v, want the original schema", got)
			}
		})
	}
}
