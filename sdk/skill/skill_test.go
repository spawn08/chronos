package skill

import (
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.skills == nil {
		t.Fatal("skills map is nil")
	}
}

func TestRegisterAndGet(t *testing.T) {
	tests := []struct {
		name  string
		skill *Skill
	}{
		{
			name: "basic skill",
			skill: &Skill{
				Name:        "search",
				Version:     "1.0.0",
				Description: "Web search capability",
			},
		},
		{
			name: "skill with tags and tools",
			skill: &Skill{
				Name:        "code_exec",
				Version:     "2.1.0",
				Description: "Code execution",
				Author:      "chronos",
				Tags:        []string{"python", "sandbox"},
				Tools:       []string{"run_python", "run_bash"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			r.Register(tc.skill)
			got, ok := r.Get(tc.skill.Name)
			if !ok {
				t.Fatalf("Get(%q) returned false", tc.skill.Name)
			}
			if got.Name != tc.skill.Name {
				t.Errorf("expected name %q, got %q", tc.skill.Name, got.Name)
			}
			if got.Version != tc.skill.Version {
				t.Errorf("expected version %q, got %q", tc.skill.Version, got.Version)
			}
		})
	}
}

func TestGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected false for missing skill")
	}
}

func TestUninstall(t *testing.T) {
	r := NewRegistry()
	s := &Skill{Name: "my_skill", Version: "1.0"}
	r.Register(s)

	err := r.Uninstall("my_skill")
	if err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	_, ok := r.Get("my_skill")
	if ok {
		t.Fatal("skill still present after uninstall")
	}
}

func TestUninstallNotFound(t *testing.T) {
	r := NewRegistry()
	err := r.Uninstall("ghost")
	if err == nil {
		t.Fatal("expected error uninstalling nonexistent skill")
	}
}

func TestList(t *testing.T) {
	r := NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}

	skills := []*Skill{
		{Name: "a", Version: "1.0"},
		{Name: "b", Version: "1.0"},
		{Name: "c", Version: "1.0"},
	}
	for _, s := range skills {
		r.Register(s)
	}

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(list))
	}
}

func TestRegisterOverwrite(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{Name: "x", Version: "1.0"})
	r.Register(&Skill{Name: "x", Version: "2.0"})

	got, ok := r.Get("x")
	if !ok {
		t.Fatal("skill not found after re-register")
	}
	if got.Version != "2.0" {
		t.Errorf("expected version 2.0, got %s", got.Version)
	}
}

func TestSkillFields(t *testing.T) {
	s := &Skill{
		Name:        "full_skill",
		Version:     "3.0.0",
		Description: "A skill with all fields",
		Author:      "test",
		Tags:        []string{"tag1", "tag2"},
		Manifest:    map[string]any{"key": "value"},
		Tools:       []string{"tool1"},
	}
	r := NewRegistry()
	r.Register(s)
	got, _ := r.Get("full_skill")
	if got.Author != "test" {
		t.Errorf("Author mismatch")
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags mismatch")
	}
	if len(got.Tools) != 1 {
		t.Errorf("Tools mismatch")
	}
}

func TestConcurrentRegister(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			name := "skill_" + string(rune('a'+i))
			r.Register(&Skill{Name: name, Version: "1.0"})
			r.Get(name)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if len(r.List()) != 10 {
		t.Fatalf("expected 10 skills after concurrent register, got %d", len(r.List()))
	}
}
