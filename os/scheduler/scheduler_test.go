package scheduler

import (
	"context"
	"testing"
	"time"
)

func TestValidateCron_Valid(t *testing.T) {
	tests := []string{
		"* * * * *",
		"0 * * * *",
		"*/5 * * * *",
		"0 9 * * 1-5",
		"30 8 1 * *",
		"0 0 1,15 * *",
	}
	for _, expr := range tests {
		if err := validateCron(expr); err != nil {
			t.Errorf("validateCron(%q) = %v, want nil", expr, err)
		}
	}
}

func TestValidateCron_Invalid(t *testing.T) {
	tests := []string{
		"",
		"* * *",
		"60 * * * *",
		"* 25 * * *",
		"* * * 13 *",
		"* * * * 8",
	}
	for _, expr := range tests {
		if err := validateCron(expr); err == nil {
			t.Errorf("validateCron(%q) = nil, want error", expr)
		}
	}
}

func TestNextCronTime(t *testing.T) {
	now := time.Date(2026, 3, 24, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		expr string
		want time.Time
	}{
		{"* * * * *", time.Date(2026, 3, 24, 10, 31, 0, 0, time.UTC)},
		{"0 11 * * *", time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC)},
		{"0 0 25 * *", time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		got := nextCronTime(tt.expr, now)
		if !got.Equal(tt.want) {
			t.Errorf("nextCronTime(%q, %v) = %v, want %v", tt.expr, now, got, tt.want)
		}
	}
}

func TestScheduler_AddAndList(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})

	sched, err := s.Add("agent-1", "*/5 * * * *", "hello", true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sched.AgentID != "agent-1" {
		t.Errorf("agent_id = %q", sched.AgentID)
	}
	if !sched.Enabled {
		t.Error("should be enabled")
	}

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
}

func TestScheduler_Remove(t *testing.T) {
	s := New(nil)
	sched, _ := s.Add("a", "* * * * *", "", true)

	if err := s.Remove(sched.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("should be empty after remove")
	}
}

func TestScheduler_RemoveNotFound(t *testing.T) {
	s := New(nil)
	if err := s.Remove("nonexistent"); err == nil {
		t.Error("expected error for nonexistent schedule")
	}
}

func TestScheduler_Get(t *testing.T) {
	s := New(nil)
	sched, _ := s.Add("a", "* * * * *", "", true)

	got, err := s.Get(sched.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sched.ID {
		t.Errorf("got ID = %q", got.ID)
	}

	_, err = s.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent")
	}
}

func TestScheduler_InvalidCron(t *testing.T) {
	s := New(nil)
	_, err := s.Add("a", "bad cron", "", true)
	if err == nil {
		t.Error("expected error for invalid cron")
	}
}

func TestScheduler_History(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})
	sched, _ := s.Add("a", "* * * * *", "test", true)

	// Simulate execution
	s.executeSched(context.Background(), sched)

	history := s.History(sched.ID)
	if len(history) != 1 {
		t.Fatalf("history len = %d, want 1", len(history))
	}
	if history[0].Status != "success" {
		t.Errorf("status = %q, want success", history[0].Status)
	}
	if sched.RunCount != 1 {
		t.Errorf("run count = %d, want 1", sched.RunCount)
	}
}

func TestParseCronField_Step(t *testing.T) {
	cf, err := parseCronField("*/15", 0, 59)
	if err != nil {
		t.Fatal(err)
	}
	if !cf.values[0] || !cf.values[15] || !cf.values[30] || !cf.values[45] {
		t.Error("expected 0,15,30,45")
	}
}

func TestParseCronField_Range(t *testing.T) {
	cf, err := parseCronField("1-5", 0, 6)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if !cf.values[i] {
			t.Errorf("missing %d", i)
		}
	}
}
