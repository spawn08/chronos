package scheduler

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduler_ExecuteSched_ErrorRunFunc(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return errors.New("run failed")
	})
	sched, _ := s.Add("a", "* * * * *", "input", true)

	s.executeSched(context.Background(), sched)

	history := s.History(sched.ID)
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
	if history[0].Status != "error" {
		t.Errorf("status=%q, want error", history[0].Status)
	}
	if history[0].Error == "" {
		t.Error("expected error message in record")
	}
}

func TestScheduler_ExecuteSched_ReuseSession(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})
	sched, _ := s.Add("a", "* * * * *", "input", false) // newSession=false

	// Run twice
	s.executeSched(context.Background(), sched)
	firstSessionID := sched.SessionID

	s.executeSched(context.Background(), sched)
	secondSessionID := sched.SessionID

	// Should reuse the same session ID
	if firstSessionID != secondSessionID {
		t.Errorf("session IDs should match for reuse: %q vs %q", firstSessionID, secondSessionID)
	}
}

func TestScheduler_ExecuteSched_NewSession(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})
	sched, _ := s.Add("a", "* * * * *", "input", true) // newSession=true

	s.executeSched(context.Background(), sched)
	first := sched.SessionID

	s.executeSched(context.Background(), sched)
	second := sched.SessionID

	// With newSession=true, session ID is generated per run but sched.SessionID stays empty
	_ = first
	_ = second
	// The session ID in the record may differ but sched.SessionID should not be updated
	if sched.SessionID != "" {
		t.Errorf("session ID should stay empty for new-session runs, got %q", sched.SessionID)
	}
}

func TestScheduler_ExecuteSched_RunCountIncrement(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})
	sched, _ := s.Add("a", "* * * * *", "test", true)

	for i := 0; i < 3; i++ {
		s.executeSched(context.Background(), sched)
	}

	if sched.RunCount != 3 {
		t.Errorf("RunCount=%d, want 3", sched.RunCount)
	}
}

func TestScheduler_ExecuteSched_NextRunUpdated(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})
	sched, _ := s.Add("a", "* * * * *", "test", true)

	// Force NextRunAt to 2 minutes in the past so next calculation is clearly in the future
	s.mu.Lock()
	sched.NextRunAt = sched.NextRunAt.Add(-2 * time.Minute)
	s.mu.Unlock()
	before := sched.NextRunAt

	s.executeSched(context.Background(), sched)
	after := sched.NextRunAt

	// NextRunAt should have been updated to a future time from now
	if !after.After(before) {
		t.Errorf("NextRunAt should advance after execution: before=%v, after=%v", before, after)
	}
}

func TestScheduler_CheckAndRun_FiresDueSchedules(t *testing.T) {
	var callCount int64
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		atomic.AddInt64(&callCount, 1)
		return nil
	})

	sched, _ := s.Add("a", "* * * * *", "test", true)
	// Force the schedule to be due
	s.mu.Lock()
	sched.NextRunAt = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()

	s.checkAndRun(context.Background(), time.Now())

	if atomic.LoadInt64(&callCount) != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestScheduler_CheckAndRun_SkipsDisabled(t *testing.T) {
	var callCount int64
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		atomic.AddInt64(&callCount, 1)
		return nil
	})

	sched, _ := s.Add("a", "* * * * *", "test", true)
	s.mu.Lock()
	sched.NextRunAt = time.Now().Add(-1 * time.Second)
	sched.Enabled = false
	s.mu.Unlock()

	s.checkAndRun(context.Background(), time.Now())

	if atomic.LoadInt64(&callCount) != 0 {
		t.Errorf("disabled schedule should not run, got %d calls", callCount)
	}
}

func TestScheduler_CheckAndRun_SkipsFuture(t *testing.T) {
	var callCount int64
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		atomic.AddInt64(&callCount, 1)
		return nil
	})

	sched, _ := s.Add("a", "* * * * *", "test", true)
	s.mu.Lock()
	sched.NextRunAt = time.Now().Add(10 * time.Minute) // far in future
	s.mu.Unlock()

	s.checkAndRun(context.Background(), time.Now())

	if atomic.LoadInt64(&callCount) != 0 {
		t.Errorf("future schedule should not run, got %d calls", callCount)
	}
}

func TestScheduler_Stop_Idempotent(t *testing.T) {
	s := New(nil)
	// Should not panic when calling Stop twice
	s.Stop()
	s.Stop()
}

func TestScheduler_Start_StopsOnContextCancel(t *testing.T) {
	s := New(nil).WithTickInterval(10 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Start(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Error("Start() should have returned after context cancel")
	}
}

func TestScheduler_Start_StopsOnStop(t *testing.T) {
	s := New(nil).WithTickInterval(10 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		s.Start(context.Background())
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	s.Stop()

	select {
	case <-done:
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Error("Start() should have returned after Stop()")
	}
}

func TestNextCronTime_DoW_Monday(t *testing.T) {
	// Find next Monday at 9am from a Wednesday
	now := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC) // Wednesday
	got := nextCronTime("0 9 * * 1", now)                  // Monday 9am

	if got.Weekday() != time.Monday {
		t.Errorf("expected Monday, got %v", got.Weekday())
	}
	if got.Hour() != 9 || got.Minute() != 0 {
		t.Errorf("expected 09:00, got %v", got.Format("15:04"))
	}
}

func TestNextCronTime_InvalidExpr(t *testing.T) {
	got := nextCronTime("invalid", time.Now())
	if !got.IsZero() {
		t.Errorf("expected zero time for invalid expr, got %v", got)
	}
}

func TestParseCronField_Comma(t *testing.T) {
	cf, err := parseCronField("1,15,30", 0, 59)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []int{1, 15, 30} {
		if !cf.values[v] {
			t.Errorf("expected value %d to be set", v)
		}
	}
}

func TestParseCronField_InvalidStep(t *testing.T) {
	_, err := parseCronField("*/0", 0, 59) // step of 0 is invalid
	if err == nil {
		t.Error("expected error for step=0")
	}
}

func TestParseCronField_InvalidRange(t *testing.T) {
	_, err := parseCronField("10-5", 0, 59) // backwards range
	if err == nil {
		t.Error("expected error for backwards range")
	}
}

func TestParseCronField_OutOfBounds(t *testing.T) {
	_, err := parseCronField("100", 0, 59)
	if err == nil {
		t.Error("expected error for out-of-bounds value")
	}
}

func TestParseCronField_Star(t *testing.T) {
	cf, err := parseCronField("*", 0, 59)
	if err != nil {
		t.Fatal(err)
	}
	if !cf.any {
		t.Error("expected any=true for *")
	}
}

func TestHistoryRecord_Fields(t *testing.T) {
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		return nil
	})
	sched, _ := s.Add("agent-x", "* * * * *", "my input", true)

	s.executeSched(context.Background(), sched)

	history := s.History(sched.ID)
	if len(history) != 1 {
		t.Fatalf("expected 1 record, got %d", len(history))
	}
	r := history[0]
	if r.AgentID != "agent-x" {
		t.Errorf("AgentID=%q, want agent-x", r.AgentID)
	}
	if r.Input != "my input" {
		t.Errorf("Input=%q, want my input", r.Input)
	}
	if r.ScheduleID != sched.ID {
		t.Errorf("ScheduleID=%q, want %q", r.ScheduleID, sched.ID)
	}
	if r.StartedAt.IsZero() || r.FinishedAt.IsZero() {
		t.Error("StartedAt and FinishedAt should be set")
	}
	if r.ID == "" {
		t.Error("RunRecord ID should not be empty")
	}
}

func TestScheduler_MultipleSchedules(t *testing.T) {
	var callCount int64
	s := New(func(ctx context.Context, agentID, input, sessionID string) error {
		atomic.AddInt64(&callCount, 1)
		return nil
	})

	// Add multiple schedules all due now
	for i := 0; i < 5; i++ {
		sched, _ := s.Add("agent", "* * * * *", "test", true)
		s.mu.Lock()
		sched.NextRunAt = time.Now().Add(-1 * time.Second)
		s.mu.Unlock()
	}

	s.checkAndRun(context.Background(), time.Now())

	if atomic.LoadInt64(&callCount) != 5 {
		t.Errorf("expected 5 calls, got %d", callCount)
	}
}
