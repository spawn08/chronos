package skill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFromDir walks root looking for SKILL.md files and returns one *Skill
// per file. Each SKILL.md is expected to begin with a YAML frontmatter block
// delimited by lines containing only `---`; the frontmatter carries the
// metadata fields (Name, Version, Description, Author, Tags, Tools), and the
// remaining Markdown body is captured into Manifest["body"] so callers can
// inject it into an agent's system prompt.
//
// Layout convention: root/<skill-name>/SKILL.md. The skill's Name defaults
// to the containing directory's basename when the frontmatter omits `name`.
// Files without a leading `---` block are skipped so unrelated Markdown in
// the tree does not accidentally become a skill.
//
// A missing root directory is not an error — LoadFromDir returns (nil, nil).
// This lets callers set skills_dir unconditionally without requiring the dir
// to exist for every project.
func LoadFromDir(root string) ([]*Skill, error) {
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skill loader: stat %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skill loader: %q is not a directory", root)
	}

	var paths []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Base(path), "SKILL.md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("skill loader: walk %q: %w", root, err)
	}
	sort.Strings(paths) // deterministic order

	skills := make([]*Skill, 0, len(paths))
	for _, p := range paths {
		s, err := loadSkillFile(p)
		if err != nil {
			return nil, err
		}
		if s == nil {
			continue // no frontmatter → not a skill file
		}
		skills = append(skills, s)
	}
	return skills, nil
}

// LoadFromDirInto walks root and registers every discovered skill in r,
// overwriting any existing entry with the same name (Register semantics).
// It returns the count of skills registered.
func LoadFromDirInto(root string, r *Registry) (int, error) {
	skills, err := LoadFromDir(root)
	if err != nil {
		return 0, err
	}
	for _, s := range skills {
		r.Register(s)
	}
	return len(skills), nil
}

// loadSkillFile parses a single SKILL.md file. Returns (nil, nil) when the
// file has no YAML frontmatter, since that's a legitimate "skip this file"
// signal rather than an error.
func loadSkillFile(path string) (*Skill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("skill loader: read %q: %w", path, err)
	}
	frontmatter, body, ok := splitFrontmatter(raw)
	if !ok {
		return nil, nil
	}

	// Parse the frontmatter as a Skill directly. Unknown keys are ignored by
	// the yaml package's default, which keeps the file forward-compatible.
	var s Skill
	if err := yaml.Unmarshal(frontmatter, &s); err != nil {
		return nil, fmt.Errorf("skill loader: %q frontmatter: %w", path, err)
	}
	if strings.TrimSpace(s.Name) == "" {
		// Default to the parent directory's basename (root/<name>/SKILL.md).
		s.Name = filepath.Base(filepath.Dir(path))
	}
	if strings.TrimSpace(s.Name) == "" {
		return nil, fmt.Errorf("skill loader: %q: skill name is empty and cannot be inferred", path)
	}
	if len(body) > 0 {
		if s.Manifest == nil {
			s.Manifest = map[string]any{}
		}
		s.Manifest["body"] = string(bytes.TrimSpace(body))
	}
	return &s, nil
}

// splitFrontmatter returns (frontmatter, body, ok). ok is false when the file
// does not begin with a `---` fence.
func splitFrontmatter(raw []byte) (frontmatter, body []byte, ok bool) {
	// Tolerate an optional UTF-8 BOM.
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	// A frontmatter block starts with a line whose only content is "---".
	// Split on the newline so we can inspect the first line without allocating
	// a full line iterator.
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if !bytes.HasPrefix(trimmed, []byte("---")) {
		return nil, nil, false
	}
	// Advance past the first line.
	rest := trimmed[3:]
	if len(rest) > 0 && rest[0] != '\n' && rest[0] != '\r' {
		// The `---` had trailing junk on the same line — not a valid fence.
		return nil, nil, false
	}
	// Find the closing fence: a line that is exactly "---" (possibly with
	// trailing whitespace/CR).
	// Normalize line endings to LF for scanning.
	lines := bytes.Split(rest, []byte("\n"))
	fmLines := make([][]byte, 0, len(lines))
	closeIdx := -1
	for i, ln := range lines {
		if i == 0 && len(bytes.TrimSpace(ln)) == 0 {
			// The line immediately after the opening `---` is empty; skip it
			// from the frontmatter but keep scanning.
			continue
		}
		if bytes.Equal(bytes.TrimRight(ln, " \t\r"), []byte("---")) {
			closeIdx = i
			break
		}
		fmLines = append(fmLines, ln)
	}
	if closeIdx == -1 {
		return nil, nil, false
	}
	frontmatter = bytes.Join(fmLines, []byte("\n"))
	body = bytes.Join(lines[closeIdx+1:], []byte("\n"))
	return frontmatter, body, true
}
