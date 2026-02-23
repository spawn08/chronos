// Package skill provides the skill/plugin system for Chronos agents.
package skill

import (
	"fmt"
	"sync"
)

// Skill represents an installable capability with metadata.
type Skill struct {
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Description string         `json:"description"`
	Author      string         `json:"author,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Manifest    map[string]any `json:"manifest,omitempty"` // from SKILL.md or JSON manifest
	Tools       []string       `json:"tools,omitempty"`    // tool names this skill provides
}

// Registry manages installed skills.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

func (r *Registry) Register(s *Skill) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills[s.Name] = s
}

func (r *Registry) Uninstall(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.skills[name]; !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	delete(r.skills, name)
	return nil
}

func (r *Registry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

func (r *Registry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	return out
}
