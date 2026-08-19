package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadFileSkills verifies skills parse from YAML, env expansion applies
// to description and manifest_path, and duplicate names fail validation.
func TestLoadFileSkills(t *testing.T) {
	t.Setenv("SKILL_TAG", "citations")

	yaml := `
agents:
  - id: agent
    name: Agent
    model: { provider: ollama, model: llama3.3 }
    skills:
      - name: web-search
        version: 1.0.0
        description: "Search and cite (${SKILL_TAG})"
        tools: [search, fetch_url]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	skills := fc.Agents[0].Skills
	if len(skills) != 1 || skills[0].Name != "web-search" {
		t.Fatalf("unexpected skills: %+v", skills)
	}
	if !strings.Contains(skills[0].Description, "citations") {
		t.Errorf("env expansion failed on description: %q", skills[0].Description)
	}
}

// TestLoadFileSkillsDuplicateFails ensures the validator rejects a config
// with two skills sharing a name.
func TestLoadFileSkillsDuplicateFails(t *testing.T) {
	yaml := `
agents:
  - id: agent
    name: Agent
    model: { provider: ollama, model: llama3.3 }
    skills:
      - name: dup
      - name: dup
`
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Fatalf("expected duplicate-name error, got: %v", err)
	}
}

// TestBuildAgentSkillsInjectPrompt confirms that skill descriptions and
// manifest_path bodies land in the built agent's system prompt, and that
// each skill is registered in the agent's skill registry.
func TestBuildAgentSkillsInjectPrompt(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "SKILL.md")
	manifestBody := "When asked for citations, always attach a source URL."
	if err := os.WriteFile(manifestPath, []byte(manifestBody), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cfg := &AgentConfig{
		ID:     "agent",
		Name:   "Agent",
		Model:  ModelConfig{Provider: "ollama", Model: "llama3.3"},
		System: "You are a helpful assistant.",
		Skills: []SkillConfig{
			{
				Name:         "web-search",
				Version:      "1.0.0",
				Description:  "Search the public web.",
				Tools:        []string{"search"},
				ManifestPath: manifestPath,
			},
		},
	}

	a, err := BuildAgent(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}

	got := a.SystemPrompt
	for _, want := range []string{
		"You are a helpful assistant.",
		"## Available skills",
		"### web-search (v1.0.0)",
		"Search the public web.",
		"Tools: search",
		manifestBody,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt missing %q\n---got---\n%s", want, got)
		}
	}

	if s, ok := a.Skills.Get("web-search"); !ok {
		t.Fatal("skill not registered on agent")
	} else if body, _ := s.Manifest["body"].(string); body != manifestBody {
		t.Errorf("manifest body not captured: got %q", body)
	}
}

// TestBuildAgentSkillsManifestMissing surfaces a clear error when the
// manifest_path can't be read.
func TestBuildAgentSkillsManifestMissing(t *testing.T) {
	cfg := &AgentConfig{
		ID:     "agent",
		Name:   "Agent",
		Model:  ModelConfig{Provider: "ollama", Model: "llama3.3"},
		Skills: []SkillConfig{{Name: "s", ManifestPath: "/nonexistent/SKILL.md"}},
	}
	_, err := BuildAgent(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "manifest_path") {
		t.Fatalf("expected manifest_path error, got: %v", err)
	}
}

// TestBuildAll_SkillCatalog_UseSkillsInjection wires the full happy path:
// a project-level SKILL.md in skills_dir is discovered, and an agent that
// lists its name under use_skills gets the skill's body injected into its
// system prompt and the skill registered on its Skills registry.
func TestBuildAll_SkillCatalog_UseSkillsInjection(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	skillDir := filepath.Join(skillsRoot, "ado-wiql")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "Call wit_query_by_wiql with a WHERE clause on [System.TeamProject]."
	sk := "---\n" +
		"name: ado-wiql\n" +
		"version: 1.0.0\n" +
		"description: Run WIQL queries against Azure DevOps.\n" +
		"tools: [wit_query_by_wiql]\n" +
		"---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(sk), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	yamlFile := "agents:\n" +
		"  - id: r\n" +
		"    name: R\n" +
		"    model: { provider: ollama, model: llama3.3 }\n" +
		"    system_prompt: Base prompt.\n" +
		"    use_skills: [ado-wiql]\n" +
		"skills_dir: skills\n"
	yamlPath := filepath.Join(root, "agents.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlFile), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	fc, err := LoadFile(yamlPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !strings.HasSuffix(fc.SkillsDir, filepath.Join(root, "skills")) {
		t.Errorf("skills_dir not resolved against yaml dir: %q", fc.SkillsDir)
	}

	agents, err := BuildAll(context.Background(), fc)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	a := agents["r"]
	if a == nil {
		t.Fatal("agent 'r' not built")
	}
	for _, want := range []string{
		"Base prompt.",
		"### ado-wiql (v1.0.0)",
		"Run WIQL queries against Azure DevOps.",
		"Tools: wit_query_by_wiql",
		body,
	} {
		if !strings.Contains(a.SystemPrompt, want) {
			t.Errorf("system prompt missing %q\n---got---\n%s", want, a.SystemPrompt)
		}
	}
	if _, ok := a.Skills.Get("ado-wiql"); !ok {
		t.Error("skill not registered on agent")
	}
}

// TestBuildAll_UseSkillsUnknownRefFails guarantees typos surface as errors
// instead of silently disappearing.
func TestBuildAll_UseSkillsUnknownRefFails(t *testing.T) {
	root := t.TempDir()
	// Empty skills dir so the catalog is empty.
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlFile := "agents:\n" +
		"  - id: r\n" +
		"    name: R\n" +
		"    model: { provider: ollama, model: llama3.3 }\n" +
		"    use_skills: [does-not-exist]\n" +
		"skills_dir: skills\n"
	yamlPath := filepath.Join(root, "agents.yaml")
	if err := os.WriteFile(yamlPath, []byte(yamlFile), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	fc, err := LoadFile(yamlPath)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	_, err = BuildAll(context.Background(), fc)
	if err == nil || !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("expected unknown-skill error, got: %v", err)
	}
}
