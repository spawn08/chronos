package storage

import "testing"

func TestApplySearchOptions(t *testing.T) {
	if got := ApplySearchOptions(); got.Filter != nil {
		t.Errorf("no options should yield nil Filter, got %v", got.Filter)
	}
	f := map[string]any{"scope": "abc"}
	got := ApplySearchOptions(WithFilter(f))
	if got.Filter["scope"] != "abc" {
		t.Errorf("WithFilter not applied: %v", got.Filter)
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name   string
		md     map[string]any
		filter map[string]any
		want   bool
	}{
		{name: "nil filter matches", md: map[string]any{"scope": "a"}, filter: nil, want: true},
		{name: "empty filter matches", md: map[string]any{"scope": "a"}, filter: map[string]any{}, want: true},
		{name: "single key match", md: map[string]any{"scope": "a"}, filter: map[string]any{"scope": "a"}, want: true},
		{name: "single key mismatch", md: map[string]any{"scope": "a"}, filter: map[string]any{"scope": "b"}, want: false},
		{name: "missing key", md: map[string]any{"other": "a"}, filter: map[string]any{"scope": "a"}, want: false},
		{name: "nil metadata with filter", md: nil, filter: map[string]any{"scope": "a"}, want: false},
		{name: "multi key all match (AND)", md: map[string]any{"scope": "a", "k": "1"}, filter: map[string]any{"scope": "a", "k": "1"}, want: true},
		{name: "multi key one mismatch", md: map[string]any{"scope": "a", "k": "1"}, filter: map[string]any{"scope": "a", "k": "2"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MatchesFilter(tc.md, tc.filter); got != tc.want {
				t.Errorf("MatchesFilter(%v, %v) = %v, want %v", tc.md, tc.filter, got, tc.want)
			}
		})
	}
}

func TestFilterSearchResults(t *testing.T) {
	results := []SearchResult{
		{Embedding: Embedding{ID: "1", Metadata: map[string]any{"scope": "a"}}},
		{Embedding: Embedding{ID: "2", Metadata: map[string]any{"scope": "b"}}},
		{Embedding: Embedding{ID: "3", Metadata: map[string]any{"scope": "a"}}},
	}

	// Nil filter returns all, order preserved.
	if got := FilterSearchResults(results, nil); len(got) != 3 {
		t.Fatalf("nil filter returned %d, want 3", len(got))
	}

	got := FilterSearchResults(results, map[string]any{"scope": "a"})
	if len(got) != 2 {
		t.Fatalf("scope=a returned %d, want 2", len(got))
	}
	if got[0].ID != "1" || got[1].ID != "3" {
		t.Errorf("filter did not preserve order: %v", []string{got[0].ID, got[1].ID})
	}
}
