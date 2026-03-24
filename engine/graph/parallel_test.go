package graph

import (
	"context"
	"fmt"
	"testing"
)

func TestFanOut_AllBranchesExecute(t *testing.T) {
	branchA := func(_ context.Context, s State) (State, error) {
		s["a"] = "done"
		return s, nil
	}
	branchB := func(_ context.Context, s State) (State, error) {
		s["b"] = "done"
		return s, nil
	}

	fn := FanOut([]NodeFunc{branchA, branchB}, MergeAll)
	result, err := fn(context.Background(), State{"initial": true})
	if err != nil {
		t.Fatal(err)
	}
	if result["a"] != "done" {
		t.Errorf("branch a not executed, got %v", result["a"])
	}
	if result["b"] != "done" {
		t.Errorf("branch b not executed, got %v", result["b"])
	}
	if result["initial"] != true {
		t.Error("original state lost")
	}
}

func TestFanOut_BranchesGetCopiedState(t *testing.T) {
	branchA := func(_ context.Context, s State) (State, error) {
		s["shared"] = "from_a"
		return s, nil
	}
	branchB := func(_ context.Context, s State) (State, error) {
		if _, ok := s["shared"]; ok {
			t.Error("branch B saw branch A's mutation — state was not copied")
		}
		s["shared"] = "from_b"
		return s, nil
	}

	fn := FanOut([]NodeFunc{branchA, branchB}, MergeAll)
	_, err := fn(context.Background(), State{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFanOut_BranchError(t *testing.T) {
	branchOK := func(_ context.Context, s State) (State, error) {
		return s, nil
	}
	branchFail := func(_ context.Context, _ State) (State, error) {
		return nil, fmt.Errorf("boom")
	}

	fn := FanOut([]NodeFunc{branchOK, branchFail}, MergeAll)
	_, err := fn(context.Background(), State{})
	if err == nil {
		t.Fatal("expected error from failing branch")
	}
}

func TestFanOut_EmptyBranches(t *testing.T) {
	fn := FanOut([]NodeFunc{}, MergeAll)
	result, err := fn(context.Background(), State{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	if result["x"] != 1 {
		t.Error("empty fan-out should preserve state")
	}
}

func TestMergeAll_LaterBranchWins(t *testing.T) {
	original := State{"base": true}
	results := []State{
		{"key": "first", "base": true},
		{"key": "second", "base": true},
	}
	merged, err := MergeAll(original, results)
	if err != nil {
		t.Fatal(err)
	}
	if merged["key"] != "second" {
		t.Errorf("got key=%v, want second (last writer wins)", merged["key"])
	}
}
