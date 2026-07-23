package repl

import (
	"context"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
)

func newRosterAgent(t *testing.T, id, name string) *agent.Agent {
	t.Helper()
	a, err := agent.New(id, name).WithModel(&mockProvider{resp: &model.ChatResponse{Content: "ok"}}).Build()
	if err != nil {
		t.Fatalf("build agent %q: %v", id, err)
	}
	return a
}

func TestSetAgents_ListsRosterAndMarksActive(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{
		newRosterAgent(t, "researcher", "Researcher"),
		newRosterAgent(t, "writer", "Writer"),
		newRosterAgent(t, "editor", "Editor"),
	})

	// First agent is active by default.
	if r.agent == nil || r.agent.ID != "researcher" {
		t.Fatalf("active agent = %v, want researcher", r.agent)
	}

	out := captureStdout(t, func() { _ = r.commands["/agent"].Handler("") })
	for _, want := range []string{"Agents (3):", "researcher", "writer", "editor", "* researcher"} {
		if !strings.Contains(out, want) {
			t.Errorf("/agent listing missing %q:\n%s", want, out)
		}
	}
}

func TestAgentCommand_SwitchByID(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{
		newRosterAgent(t, "researcher", "Researcher"),
		newRosterAgent(t, "writer", "Writer"),
	})

	out := captureStdout(t, func() { _ = r.commands["/agent"].Handler("writer") })
	if !strings.Contains(out, "Switched to") || !strings.Contains(out, "writer") {
		t.Errorf("switch output = %q", out)
	}
	if r.agent.ID != "writer" {
		t.Errorf("active agent = %q, want writer", r.agent.ID)
	}
}

func TestAgentCommand_SwitchByName(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{
		newRosterAgent(t, "a1", "Researcher"),
		newRosterAgent(t, "a2", "Writer"),
	})

	_ = captureStdout(t, func() { _ = r.commands["/agent"].Handler("Writer") })
	if r.agent.ID != "a2" {
		t.Errorf("active agent = %q, want a2 (matched by name)", r.agent.ID)
	}
}

func TestAgentCommand_SwitchUnknownErrors(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{newRosterAgent(t, "a1", "One")})

	if err := r.commands["/agent"].Handler("nope"); err == nil {
		t.Error("expected error switching to unknown agent")
	}
}

func TestTeamsCommands_RegisteredAndListed(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{newRosterAgent(t, "a1", "One")})
	r.SetTeams([]string{"pipeline", "review"}, func(_ context.Context, _, _ string) (<-chan team.TeamStreamEvent, error) {
		ch := make(chan team.TeamStreamEvent)
		close(ch)
		return ch, nil
	})

	if _, ok := r.commands["/teams"]; !ok {
		t.Fatal("/teams not registered")
	}
	if _, ok := r.commands["/team"]; !ok {
		t.Fatal("/team not registered")
	}

	out := captureStdout(t, func() { _ = r.commands["/teams"].Handler("") })
	for _, want := range []string{"Teams (2):", "pipeline", "review"} {
		if !strings.Contains(out, want) {
			t.Errorf("/teams listing missing %q:\n%s", want, out)
		}
	}
}

func TestTeamCommand_UsageError(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{newRosterAgent(t, "a1", "One")})
	r.SetTeams([]string{"pipeline"}, func(_ context.Context, _, _ string) (<-chan team.TeamStreamEvent, error) {
		return nil, nil
	})

	if err := r.commands["/team"].Handler("pipeline"); err == nil {
		t.Error("expected usage error when message is missing")
	}
}

func TestTeamCommand_StreamsEvents(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgents([]*agent.Agent{newRosterAgent(t, "a1", "One")})
	r.SetTeams([]string{"pipeline"}, func(_ context.Context, teamID, message string) (<-chan team.TeamStreamEvent, error) {
		ch := make(chan team.TeamStreamEvent, 4)
		ch <- team.TeamStreamEvent{Type: team.TeamEventAgentStart, AgentID: "researcher"}
		ch <- team.TeamStreamEvent{Type: team.TeamEventToken, AgentID: "researcher", Content: "hello"}
		ch <- team.TeamStreamEvent{Type: team.TeamEventAgentEnd, AgentID: "researcher"}
		ch <- team.TeamStreamEvent{Type: team.TeamEventComplete}
		close(ch)
		return ch, nil
	})

	out := captureStdout(t, func() { _ = r.commands["/team"].Handler("pipeline what can you do?") })
	for _, want := range []string{"researcher", "hello"} {
		if !strings.Contains(out, want) {
			t.Errorf("/team output missing %q:\n%s", want, out)
		}
	}
}
