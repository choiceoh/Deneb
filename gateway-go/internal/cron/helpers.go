package cron

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSchedule parses a cron schedule string into a Schedule.
// Supports formats: "every 5m", "every 1h", "every 30s", or raw milliseconds.
func ParseSchedule(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Schedule{}, fmt.Errorf("empty schedule specification")
	}

	// Try raw milliseconds.
	if ms, err := strconv.ParseInt(spec, 10, 64); err == nil && ms > 0 {
		return Schedule{IntervalMs: ms, Label: fmt.Sprintf("every %dms", ms)}, nil
	}

	// Try "every Xunit" format.
	lower := strings.ToLower(spec)
	if strings.HasPrefix(lower, "every ") {
		durStr := strings.TrimSpace(lower[6:])
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return Schedule{}, fmt.Errorf("invalid duration %q: %w", durStr, err)
		}
		if dur <= 0 {
			return Schedule{}, fmt.Errorf("schedule duration must be positive")
		}
		return Schedule{
			IntervalMs: dur.Milliseconds(),
			Label:      spec,
		}, nil
	}

	// Try Go duration directly.
	dur, err := time.ParseDuration(spec)
	if err != nil {
		return Schedule{}, fmt.Errorf("unrecognized schedule format %q", spec)
	}
	if dur <= 0 {
		return Schedule{}, fmt.Errorf("schedule duration must be positive")
	}
	return Schedule{IntervalMs: dur.Milliseconds(), Label: spec}, nil
}

// RunResult holds the result of an immediate cron run.
type RunResult struct {
	ID        string `json:"id"`
	Status    string `json:"status"` // "ok" | "error" | "not_found"
	Error     string `json:"error,omitempty"`
	RuntimeMs int64  `json:"runtimeMs,omitempty"`
}

// RunLog holds a historical run entry.
type RunLog struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	RanAtMs   int64  `json:"ranAtMs"`
	RuntimeMs int64  `json:"runtimeMs,omitempty"`
}

// Running returns true if the scheduler has any active tasks.
func (s *Scheduler) Running() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tasks) > 0
}

// NextRunAt returns the approximate next-run timestamp (based on task intervals).
func (s *Scheduler) NextRunAt() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var earliest int64
	now := time.Now().UnixMilli()
	for _, t := range s.tasks {
		st := t.status()
		if st.IntervalMs <= 0 {
			continue
		}
		var nextRun int64
		if st.LastRunAt > 0 {
			nextRun = st.LastRunAt + st.IntervalMs
		} else {
			nextRun = now + st.IntervalMs
		}
		if earliest == 0 || nextRun < earliest {
			earliest = nextRun
		}
	}
	return earliest
}

// Update modifies properties of a registered cron task.
func (s *Scheduler) Update(id string, patch map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("cron task %q not found", id)
	}

	if label, ok := patch["label"]; ok {
		if str, ok := label.(string); ok {
			t.schedule.Label = str
		}
	}
	if immediate, ok := patch["immediate"]; ok {
		if b, ok := immediate.(bool); ok {
			t.schedule.Immediate = b
		}
	}
	return nil
}

// RunNow immediately executes a task by ID.
func (s *Scheduler) RunNow(ctx context.Context, id string) (*RunResult, error) {
	s.mu.RLock()
	t, ok := s.tasks[id]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("cron task %q not found", id)
	}

	start := time.Now()
	t.setRunning()
	err := t.fn(ctx)
	t.recordRun(err)
	runtimeMs := time.Since(start).Milliseconds()

	result := &RunResult{
		ID:        id,
		Status:    "ok",
		RuntimeMs: runtimeMs,
	}
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
	}
	return result, nil
}

// Runs returns recent run history for a task (or all tasks if id is empty).
// This is a simplified in-memory implementation; the full TS version persists to disk.
func (s *Scheduler) Runs(id string, limit, offset int) []RunLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var logs []RunLog
	for _, t := range s.tasks {
		if id != "" && t.id != id {
			continue
		}
		st := t.status()
		if st.LastRunAt > 0 {
			status := "ok"
			if st.LastError != "" {
				status = "error"
			}
			logs = append(logs, RunLog{
				ID:      t.id + "-last",
				TaskID:  t.id,
				Status:  status,
				Error:   st.LastError,
				RanAtMs: st.LastRunAt,
			})
		}
	}

	// Apply offset + limit.
	if offset > 0 && offset < len(logs) {
		logs = logs[offset:]
	} else if offset >= len(logs) {
		return nil
	}
	if limit > 0 && limit < len(logs) {
		logs = logs[:limit]
	}
	return logs
}
