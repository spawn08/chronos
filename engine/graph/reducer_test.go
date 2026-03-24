package graph

import (
	"reflect"
	"testing"
)

func TestReplaceReducer(t *testing.T) {
	r := ReplaceReducer{}
	result := r.Reduce("old", "new")
	if result != "new" {
		t.Errorf("got %v, want new", result)
	}
}

func TestAppendReducer_SliceToSlice(t *testing.T) {
	r := AppendReducer{}
	result := r.Reduce([]any{"a", "b"}, []any{"c", "d"})
	expected := []any{"a", "b", "c", "d"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestAppendReducer_SingleValue(t *testing.T) {
	r := AppendReducer{}
	result := r.Reduce([]any{"a"}, "b")
	expected := []any{"a", "b"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestAppendReducer_NilExisting(t *testing.T) {
	r := AppendReducer{}
	result := r.Reduce(nil, "first")
	expected := []any{"first"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestAppendReducer_NonSliceExisting(t *testing.T) {
	r := AppendReducer{}
	result := r.Reduce("old", "new")
	expected := []any{"old", "new"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestMergeMapReducer_MergesMaps(t *testing.T) {
	r := MergeMapReducer{}
	existing := map[string]any{"a": 1, "b": 2}
	update := map[string]any{"b": 3, "c": 4}
	result := r.Reduce(existing, update)

	expected := map[string]any{"a": 1, "b": 3, "c": 4}
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}
	if !reflect.DeepEqual(resultMap, expected) {
		t.Errorf("got %v, want %v", resultMap, expected)
	}
}

func TestMergeMapReducer_NonMapExisting(t *testing.T) {
	r := MergeMapReducer{}
	result := r.Reduce("not-a-map", map[string]any{"a": 1})
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("should replace with update map, got %T", result)
	}
	if resultMap["a"] != 1 {
		t.Error("expected update map to be returned")
	}
}

func TestMergeMapReducer_NonMapUpdate(t *testing.T) {
	r := MergeMapReducer{}
	result := r.Reduce(map[string]any{"a": 1}, "not-a-map")
	if result != "not-a-map" {
		t.Error("non-map update should replace")
	}
}

func TestReducerFunc(t *testing.T) {
	fn := ReducerFunc(func(existing, update any) any {
		e, _ := existing.(int)
		u, _ := update.(int)
		return e + u
	})
	result := fn.Reduce(10, 5)
	if result != 15 {
		t.Errorf("got %v, want 15", result)
	}
}

func TestStateSchema_ApplyUpdate(t *testing.T) {
	schema := StateSchema{
		"messages": AppendReducer{},
		"config":   MergeMapReducer{},
	}

	existing := State{
		"messages": []any{"hello"},
		"config":   map[string]any{"theme": "dark"},
		"counter":  1,
	}

	update := State{
		"messages": []any{"world"},
		"config":   map[string]any{"lang": "en"},
		"counter":  2,
	}

	result := schema.ApplyUpdate(existing, update)

	msgs, ok := result["messages"].([]any)
	if !ok {
		t.Fatalf("messages should be []any, got %T", result["messages"])
	}
	if !reflect.DeepEqual(msgs, []any{"hello", "world"}) {
		t.Errorf("messages = %v, want [hello, world]", msgs)
	}

	cfg, ok := result["config"].(map[string]any)
	if !ok {
		t.Fatalf("config should be map, got %T", result["config"])
	}
	if cfg["theme"] != "dark" || cfg["lang"] != "en" {
		t.Errorf("config = %v, want {theme:dark, lang:en}", cfg)
	}

	if result["counter"] != 2 {
		t.Errorf("counter = %v, want 2 (plain replace)", result["counter"])
	}
}

func TestStateSchema_Empty(t *testing.T) {
	schema := StateSchema{}
	existing := State{"a": 1}
	update := State{"a": 2, "b": 3}
	result := schema.ApplyUpdate(existing, update)
	if result["a"] != 2 {
		t.Errorf("a = %v, want 2", result["a"])
	}
	if result["b"] != 3 {
		t.Errorf("b = %v, want 3", result["b"])
	}
}

func TestStateSchema_PreservesUnchangedKeys(t *testing.T) {
	schema := StateSchema{}
	existing := State{"keep": "me", "update": "old"}
	update := State{"update": "new"}
	result := schema.ApplyUpdate(existing, update)
	if result["keep"] != "me" {
		t.Error("should preserve keys not in update")
	}
}
