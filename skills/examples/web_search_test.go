package examples

import (
	"testing"
)

func TestWebSearchSkill_Fields(t *testing.T) {
	s := WebSearchSkill
	if s == nil {
		t.Fatal("WebSearchSkill is nil")
	}

	tests := []struct {
		field string
		got   string
		want  string
	}{
		{"Name", s.Name, "web_search"},
		{"Version", s.Version, "1.0.0"},
		{"Author", s.Author, "chronos"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

func TestWebSearchSkill_Tags(t *testing.T) {
	s := WebSearchSkill
	wantTags := map[string]bool{"search": true, "web": true, "rag": true}
	for _, tag := range s.Tags {
		if !wantTags[tag] {
			t.Errorf("unexpected tag %q", tag)
		}
		delete(wantTags, tag)
	}
	for missing := range wantTags {
		t.Errorf("missing expected tag %q", missing)
	}
}

func TestWebSearchSkill_Tools(t *testing.T) {
	s := WebSearchSkill
	if len(s.Tools) == 0 {
		t.Fatal("WebSearchSkill has no tools")
	}
	if s.Tools[0] != "web_search" {
		t.Errorf("Tools[0] = %q, want %q", s.Tools[0], "web_search")
	}
}

func TestWebSearchSkill_Manifest(t *testing.T) {
	s := WebSearchSkill
	if s.Manifest == nil {
		t.Fatal("WebSearchSkill Manifest is nil")
	}
	if s.Manifest["api_key_env"] != "SEARCH_API_KEY" {
		t.Errorf("api_key_env = %v, want SEARCH_API_KEY", s.Manifest["api_key_env"])
	}
	if s.Manifest["max_results"] != 10 {
		t.Errorf("max_results = %v, want 10", s.Manifest["max_results"])
	}
}
