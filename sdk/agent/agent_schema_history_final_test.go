package agent

import (
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

func TestFormatHistoryMessages_OtherRole(t *testing.T) {
	out := formatHistoryMessages([]model.Message{
		{Role: "tool", Content: "tr"},
	})
	if !strings.Contains(out, "tool:") {
		t.Fatalf("got %q", out)
	}
}

func TestValidateAgainstSchema_RequiredAndTypes(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"answer": map[string]any{"type": "string"},
			"flag":   map[string]any{"type": "boolean"},
			"items":  map[string]any{"type": "array"},
			"meta":   map[string]any{"type": "object"},
			"n":      map[string]any{"type": "integer"},
		},
		"required": []any{"answer"},
	}
	if err := validateAgainstSchema(`{"answer":1}`, schema); err == nil {
		t.Fatal("expected type error for answer")
	}
	if err := validateAgainstSchema(`{"answer":"ok","flag":"no"}`, schema); err == nil {
		t.Fatal("expected boolean type error")
	}
	if err := validateAgainstSchema(`{"answer":"ok","flag":true,"items":{}}`, schema); err == nil {
		t.Fatal("expected array type error")
	}
	if err := validateAgainstSchema(`{"answer":"ok","flag":true,"items":[],"meta":[]}`, schema); err == nil {
		t.Fatal("expected object type error")
	}
	if err := validateAgainstSchema(`{"answer":"ok","flag":true,"items":[],"meta":{},"n":"1"}`, schema); err == nil {
		t.Fatal("expected number type error")
	}
	if err := validateAgainstSchema(`not json`, schema); err == nil {
		t.Fatal("expected json error")
	}
	if err := validateAgainstSchema(`{"answer":"ok"}`, map[string]any{"properties": nil}); err != nil {
		t.Fatal(err)
	}
}
