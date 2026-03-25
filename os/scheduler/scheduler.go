// Package scheduler provides cron-based scheduling for agent runs.
package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Schedule defines a cron-scheduled agent run.
type Schedule struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	CronExpr   string    `json:"cron_expr"`
	Input      string    `json:"input"`
	NewSession bool      `json:"new_session"` // true = new session per run, false = reuse
	SessionID  string    `json:"session_id,omitempty"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	LastRunAt  time.Time `json:"last_run_at,omitempty"`
	NextRunAt  time.Time `json:"next_run_at,omitempty"`
	RunCount   int64     `json:"run_count"`
}

// RunRecord is a historical record of a scheduled run.
type RunRecord struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	AgentID    string    `json:"agent_id"`
	SessionID  string    `json:"session_id"`
	Input      string    `json:"input"`
	Status     string    `json:"status"` // success, error
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
}

// RunFunc is called when a schedule fires. It receives the agent ID, input, and session ID.
type RunFunc func(ctx context.Context, agentID, input, sessionID string) error

// Scheduler manages cron-scheduled agent runs.
type Scheduler struct {
	mu        sync.RWMutex
	schedules map[string]*Schedule
	history   map[string][]RunRecord // schedule_id -> records
	runFn     RunFunc
	stopCh    chan struct{}
	stopped   bool
	counter   int64
	tick      time.Duration
}

// New creates a new Scheduler. runFn is called when a schedule fires.
func New(runFn RunFunc) *Scheduler {
	return &Scheduler{
		schedules: make(map[string]*Schedule),
		history:   make(map[string][]RunRecord),
		runFn:     runFn,
		stopCh:    make(chan struct{}),
		tick:      time.Minute,
	}
}

// WithTickInterval sets the polling interval (for testing). Default is 1 minute.
func (s *Scheduler) WithTickInterval(d time.Duration) *Scheduler {
	s.tick = d
	return s
}

// Add creates a new schedule.
func (s *Scheduler) Add(agentID, cronExpr, input string, newSession bool) (*Schedule, error) {
	if err := validateCron(cronExpr); err != nil {
		return nil, fmt.Errorf("scheduler: invalid cron expression: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.counter++
	sched := &Schedule{
		ID:         fmt.Sprintf("sched_%d", s.counter),
		AgentID:    agentID,
		CronExpr:   cronExpr,
		Input:      input,
		NewSession: newSession,
		Enabled:    true,
		CreatedAt:  time.Now(),
		NextRunAt:  nextCronTime(cronExpr, time.Now()),
	}
	s.schedules[sched.ID] = sched
	return sched, nil
}

// Remove deletes a schedule.
func (s *Scheduler) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.schedules[id]; !ok {
		return fmt.Errorf("scheduler: schedule %q not found", id)
	}
	delete(s.schedules, id)
	return nil
}

// List returns all schedules.
func (s *Scheduler) List() []*Schedule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Schedule, 0, len(s.schedules))
	for _, sched := range s.schedules {
		result = append(result, sched)
	}
	return result
}

// Get returns a schedule by ID.
func (s *Scheduler) Get(id string) (*Schedule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sched, ok := s.schedules[id]
	if !ok {
		return nil, fmt.Errorf("scheduler: schedule %q not found", id)
	}
	return sched, nil
}

// History returns run records for a schedule.
func (s *Scheduler) History(scheduleID string) []RunRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.history[scheduleID]
}

// Start begins the scheduler loop. It checks for due schedules every tick interval.
func (s *Scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.checkAndRun(ctx, now)
		}
	}
}

// Stop halts the scheduler loop.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		close(s.stopCh)
		s.stopped = true
	}
}

func (s *Scheduler) checkAndRun(ctx context.Context, now time.Time) {
	s.mu.Lock()
	var due []*Schedule
	for _, sched := range s.schedules {
		if sched.Enabled && !sched.NextRunAt.IsZero() && !now.Before(sched.NextRunAt) {
			due = append(due, sched)
		}
	}
	s.mu.Unlock()

	for _, sched := range due {
		s.executeSched(ctx, sched)
	}
}

func (s *Scheduler) executeSched(ctx context.Context, sched *Schedule) {
	sessionID := sched.SessionID
	if sched.NewSession || sessionID == "" {
		sessionID = fmt.Sprintf("sched_%s_%d", sched.ID, time.Now().UnixNano())
	}

	record := RunRecord{
		ID:         fmt.Sprintf("run_%d", time.Now().UnixNano()),
		ScheduleID: sched.ID,
		AgentID:    sched.AgentID,
		SessionID:  sessionID,
		Input:      sched.Input,
		StartedAt:  time.Now(),
	}

	err := s.runFn(ctx, sched.AgentID, sched.Input, sessionID)
	record.FinishedAt = time.Now()
	if err != nil {
		record.Status = "error"
		record.Error = err.Error()
	} else {
		record.Status = "success"
	}

	s.mu.Lock()
	sched.LastRunAt = record.StartedAt
	sched.RunCount++
	sched.NextRunAt = nextCronTime(sched.CronExpr, time.Now())
	if !sched.NewSession {
		sched.SessionID = sessionID
	}
	s.history[sched.ID] = append(s.history[sched.ID], record)
	s.mu.Unlock()
}

// CronField represents a parsed cron field.
type cronField struct {
	values map[int]bool
	any    bool
}

// validateCron validates a 5-field cron expression (minute hour dom month dow).
func validateCron(expr string) error {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("expected 5 fields, got %d", len(fields))
	}
	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for i, f := range fields {
		if _, err := parseCronField(f, limits[i][0], limits[i][1]); err != nil {
			return fmt.Errorf("field %d (%q): %w", i, f, err)
		}
	}
	return nil
}

func parseCronField(field string, min, max int) (*cronField, error) {
	if field == "*" {
		return &cronField{any: true}, nil
	}

	cf := &cronField{values: make(map[int]bool)}

	for _, part := range strings.Split(field, ",") {
		if strings.Contains(part, "/") {
			// Step: */5 or 1-30/5
			stepParts := strings.SplitN(part, "/", 2)
			step, err := strconv.Atoi(stepParts[1])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step %q", stepParts[1])
			}
			rangeStart, rangeEnd := min, max
			if stepParts[0] != "*" {
				rangeParts := strings.SplitN(stepParts[0], "-", 2)
				rangeStart, err = strconv.Atoi(rangeParts[0])
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", rangeParts[0])
				}
				if len(rangeParts) == 2 {
					rangeEnd, err = strconv.Atoi(rangeParts[1])
					if err != nil {
						return nil, fmt.Errorf("invalid value %q", rangeParts[1])
					}
				}
			}
			for i := rangeStart; i <= rangeEnd; i += step {
				cf.values[i] = true
			}
		} else if strings.Contains(part, "-") {
			// Range: 1-5
			rangeParts := strings.SplitN(part, "-", 2)
			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", rangeParts[0])
			}
			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", rangeParts[1])
			}
			if start < min || end > max || start > end {
				return nil, fmt.Errorf("range %d-%d out of bounds [%d,%d]", start, end, min, max)
			}
			for i := start; i <= end; i++ {
				cf.values[i] = true
			}
		} else {
			// Single value
			v, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", part)
			}
			if v < min || v > max {
				return nil, fmt.Errorf("value %d out of bounds [%d,%d]", v, min, max)
			}
			cf.values[v] = true
		}
	}
	return cf, nil
}

// nextCronTime calculates the next time after `after` that matches the cron expression.
func nextCronTime(expr string, after time.Time) time.Time {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}
	}

	limits := [][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	parsed := make([]*cronField, 5)
	for i, f := range fields {
		cf, err := parseCronField(f, limits[i][0], limits[i][1])
		if err != nil {
			return time.Time{}
		}
		parsed[i] = cf
	}

	// Start from the next minute
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Search up to 1 year ahead
	deadline := after.Add(366 * 24 * time.Hour)
	for t.Before(deadline) {
		if matches(parsed, t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func matches(fields []*cronField, t time.Time) bool {
	checks := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	for i, cf := range fields {
		if cf.any {
			continue
		}
		if !cf.values[checks[i]] {
			return false
		}
	}
	return true
}
