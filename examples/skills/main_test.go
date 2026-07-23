package main

import (
	"testing"

	"github.com/spawn08/chronos/sdk/skill"
)

func TestRegistryRegisterAndResolve(t *testing.T) {
	r := skill.NewRegistry()

	r.Register(&skill.Skill{Name: "web-search", Version: "1.0.0", Tools: []string{"search"}})
	r.Register(&skill.Skill{Name: "code-review", Version: "0.3.0"})

	if got := len(r.List()); got != 2 {
		t.Fatalf("List() = %d skills, want 2", got)
	}

	s, ok := r.Get("web-search")
	if !ok {
		t.Fatal("expected to resolve web-search")
	}
	if s.Version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", s.Version)
	}
}

func TestRegistryVersionUpgrade(t *testing.T) {
	r := skill.NewRegistry()

	r.Register(&skill.Skill{Name: "web-search", Version: "1.0.0"})
	r.Register(&skill.Skill{Name: "web-search", Version: "1.1.0"})

	if got := len(r.List()); got != 1 {
		t.Fatalf("List() = %d skills, want 1 after in-place upgrade", got)
	}
	s, _ := r.Get("web-search")
	if s.Version != "1.1.0" {
		t.Errorf("version = %q, want 1.1.0 after upgrade", s.Version)
	}
}

func TestRegistryUninstall(t *testing.T) {
	r := skill.NewRegistry()
	r.Register(&skill.Skill{Name: "code-review", Version: "0.3.0"})

	if err := r.Uninstall("code-review"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, ok := r.Get("code-review"); ok {
		t.Error("skill still resolvable after uninstall")
	}
	if err := r.Uninstall("code-review"); err == nil {
		t.Error("expected error uninstalling a missing skill")
	}
}
