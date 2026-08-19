package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromDir_ParsesFrontmatterAndBody(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "web-search")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\n" +
		"name: web-search\n" +
		"version: 1.0.0\n" +
		"description: Search the public web.\n" +
		"tags: [research, io]\n" +
		"tools: [search, fetch_url]\n" +
		"---\n" +
		"# When to use\n" +
		"Prefer this when the user asks about current events.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	skills, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Name != "web-search" || s.Version != "1.0.0" {
		t.Errorf("metadata: %+v", s)
	}
	if got := s.Manifest["body"].(string); !strings.Contains(got, "current events") {
		t.Errorf("body missing content: %q", got)
	}
}

func TestLoadFromDir_MissingDirIsNotError(t *testing.T) {
	skills, err := LoadFromDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil, got %v", skills)
	}
}

func TestLoadFromDir_InfersNameFromDirWhenMissing(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "cited-answers")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\ndescription: Attach sources.\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	skills, err := LoadFromDir(dir)
	if err != nil || len(skills) != 1 || skills[0].Name != "cited-answers" {
		t.Fatalf("expected inferred name 'cited-answers', got %+v (err=%v)", skills, err)
	}
}

func TestLoadFromDir_SkipsFilesWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("plain markdown, no fence\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	skills, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}
