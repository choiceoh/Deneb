// Package cron provides scheduled task execution for the gateway.
//
// This mirrors the cron logic in src/cron/ from the TypeScript codebase.
// The Go gateway can natively handle cron scheduling with goroutines
// instead of relying on Node.js timers.
package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// TaskFunc is the function signature for cron task handlers.
type TaskFunc func(ctx context.Context) error

// Schedule defines when and how often a task runs.
type Schedule struct {
	IntervalMs int64  `json:"intervalMs"` // milliseconds between runs (0 = one-shot)
	Label      string `json:"label"`      // human-readable label
	Immediate  bool   `json:"immediate"`  // run immediately on registration
}

// TaskStatus represents the current state of a cron task.
type TaskStatus struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	IntervalMs int64  `json:"intervalMs"`
	RunCount   int    `json:"runCount"`
	LastRunAt  int64  `json:"lastRunAt,omitempty"`
	LastError  string `json:"lastError,omitempty"`
	Running    bool   `json:"running"`
}

type task struct {
	id       string
	schedule Schedule
	fn       TaskFunc
	cancel   context.CancelFunc

	mu        sync.Mutex
	runCount  int
	lastRunAt int64
	lastError string
	running   bool
}

func (t *task) status() TaskStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return TaskStatus{
		ID:         t.id,
		Label:      t.schedule.Label,
		IntervalMs: t.schedule.IntervalMs,
		RunCount:   t.runCount,
		LastRunAt:  t.lastRunAt,
		LastError:  t.lastError,
		Running:    t.running,
	}
}

func (t *task) recordRun(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.runCount++
	t.lastRunAt = time.Now().UnixMilli()
	if err != nil {
		t.lastError = err.Error()
	} else {
		t.lastError = ""
	}
	t.running = false
}

func (t *task) setRunning() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.running = true
}

// Scheduler manages periodic task execution.
type Scheduler struct {
	mu     sync.RWMutex
	tasks  map[string]*task
	logger *slog.Logger
	wg     sync.WaitGroup
}

// NewScheduler creates a new cron scheduler.
func NewScheduler(logger *slog.Logger) *Scheduler {
	return &Scheduler{
		tasks:  make(map[string]*task),
		logger: logger,
	}
}

// Register adds a new cron task. If a task with the same ID exists, it is
// replaced (the old one is canceled).
func (s *Scheduler) Register(ctx context.Context, id string, schedule Schedule, fn TaskFunc) error {
	if id == "" {
		return fmt.Errorf("task ID is required")
	}
	if schedule.IntervalMs <= 0 && !schedule.Immediate {
		return fmt.Errorf("task must have intervalMs > 0 or immediate=true")
	}

	s.mu.Lock()
	// Cancel existing task with same ID.
	if existing, ok := s.tasks[id]; ok {
		existing.cancel()
	}

	taskCtx, cancel := context.WithCancel(ctx)
	t := &task{
		id:       id,
		schedule: schedule,
		fn:       fn,
		cancel:   cancel,
	}
	s.tasks[id] = t
	// wg.Add must happen under the lock so Close() cannot call wg.Wait()
	// before we register the goroutine.
	s.wg.Add(1)
	s.mu.Unlock()

	go s.runTask(taskCtx, t)

	s.logger.Info("cron task registered", "id", id, "label", schedule.Label, "intervalMs", schedule.IntervalMs)
	return nil
}

// Unregister removes and cancels a cron task.
func (s *Scheduler) Unregister(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return false
	}
	t.cancel()
	delete(s.tasks, id)
	return true
}

// List returns the status of all registered tasks.
func (s *Scheduler) List() []TaskStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]TaskStatus, 0, len(s.tasks))
	for _, t := range s.tasks {
		result = append(result, t.status())
	}
	return result
}

// Get returns the status of a single task.
func (s *Scheduler) Get(id string) *TaskStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil
	}
	st := t.status()
	return &st
}

// Close cancels all tasks and waits for them to finish.
func (s *Scheduler) Close() {
	s.mu.Lock()
	for _, t := range s.tasks {
		t.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Scheduler) runTask(ctx context.Context, t *task) {
	defer s.wg.Done()

	if t.schedule.Immediate {
		s.executeTask(ctx, t)
	}

	if t.schedule.IntervalMs <= 0 {
		return // one-shot task
	}

	ticker := time.NewTicker(time.Duration(t.schedule.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.executeTask(ctx, t)
		}
	}
}

func (s *Scheduler) executeTask(ctx context.Context, t *task) {
	t.setRunning()

	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("task panicked: %v", r)
				s.logger.Error("cron task panic", "id", t.id, "panic", r)
			}
		}()
		err = t.fn(ctx)
	}()

	t.recordRun(err)

	if err != nil {
		s.logger.Warn("cron task error", "id", t.id, "error", err)
	}
}
