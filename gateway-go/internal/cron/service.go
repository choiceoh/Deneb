package cron

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Service manages the full cron job lifecycle: CRUD, scheduling, execution, and delivery.
// This mirrors the TS CronService from src/cron/service.ts.
type Service struct {
	mu        sync.Mutex
	scheduler *Scheduler
	store     *Store
	runLog    *PersistentRunLog
	agent     AgentRunner
	logger    *slog.Logger
	cfg       ServiceConfig
	running   bool
	stopCh    chan struct{}
}

// ServiceConfig configures the cron service.
type ServiceConfig struct {
	StorePath      string
	DefaultChannel string
	DefaultTo      string
	Enabled        bool
}

// NewService creates a new cron service.
func NewService(cfg ServiceConfig, agent AgentRunner, logger *slog.Logger) *Service {
	return &Service{
		scheduler: NewScheduler(logger),
		store:     NewStore(cfg.StorePath),
		runLog:    NewPersistentRunLog(cfg.StorePath),
		agent:     agent,
		logger:    logger,
		cfg:       cfg,
		stopCh:    make(chan struct{}),
	}
}

// Start loads jobs from the store and schedules them.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	store, err := s.store.Load()
	if err != nil {
		return fmt.Errorf("load cron store: %w", err)
	}

	for _, job := range store.Jobs {
		if !job.Enabled {
			continue
		}
		if err := s.scheduleJob(ctx, job); err != nil {
			s.logger.Warn("failed to schedule cron job", "id", job.ID, "error", err)
		}
	}

	s.running = true
	s.logger.Info("cron service started", "jobs", len(store.Jobs))
	return nil
}

// Stop cancels all scheduled jobs.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.scheduler.Close()
	s.running = false
	s.logger.Info("cron service stopped")
}

// List returns all jobs from the store.
func (s *Service) List() ([]StoreJob, error) {
	store, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	return store.Jobs, nil
}

// GetJob returns a single job by ID.
func (s *Service) GetJob(id string) *StoreJob {
	return s.store.GetJob(id)
}

// Add creates a new cron job, saves it, and schedules it.
func (s *Service) Add(ctx context.Context, job StoreJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if job.CreatedAtMs == 0 {
		job.CreatedAtMs = now
	}
	job.UpdatedAtMs = now

	// Compute next run time.
	job.State.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, now)

	if err := s.store.AddJob(job); err != nil {
		return fmt.Errorf("save cron job: %w", err)
	}

	if job.Enabled {
		if err := s.scheduleJob(ctx, job); err != nil {
			s.logger.Warn("failed to schedule new job", "id", job.ID, "error", err)
		}
	}
	return nil
}

// Update patches a job and reschedules it.
func (s *Service) Update(ctx context.Context, id string, patch func(*StoreJob)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.store.GetJob(id)
	if job == nil {
		return fmt.Errorf("job %q not found", id)
	}

	patch(job)
	job.UpdatedAtMs = time.Now().UnixMilli()

	if err := s.store.AddJob(*job); err != nil {
		return err
	}

	// Reschedule.
	s.scheduler.Unregister(id)
	if job.Enabled {
		s.scheduleJob(ctx, *job)
	}
	return nil
}

// Remove deletes a job and unschedules it.
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scheduler.Unregister(id)
	return s.store.RemoveJob(id)
}

// Run executes a job immediately.
func (s *Service) Run(ctx context.Context, id string) (*RunOutcome, error) {
	job := s.store.GetJob(id)
	if job == nil {
		return nil, fmt.Errorf("job %q not found", id)
	}

	return s.executeJob(ctx, *job)
}

// Status returns the service status.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	tasks := s.scheduler.List()
	return ServiceStatus{
		Running:   running,
		TaskCount: len(tasks),
		Tasks:     tasks,
	}
}

// ServiceStatus reports the cron service state.
type ServiceStatus struct {
	Running   bool         `json:"running"`
	TaskCount int          `json:"taskCount"`
	Tasks     []TaskStatus `json:"tasks,omitempty"`
}

func (s *Service) scheduleJob(ctx context.Context, job StoreJob) error {
	intervalMs := resolveJobIntervalMs(job)
	if intervalMs <= 0 && job.Schedule.Kind != "at" {
		return fmt.Errorf("job %q has no schedulable interval", job.ID)
	}

	immediate := job.Schedule.Kind == "at"
	sched := Schedule{
		IntervalMs: intervalMs,
		Label:      job.Name,
		Immediate:  immediate,
	}

	jobCopy := job
	return s.scheduler.Register(ctx, job.ID, sched, func(taskCtx context.Context) error {
		outcome, err := s.executeJob(taskCtx, jobCopy)
		if err != nil {
			return err
		}
		if outcome != nil && outcome.Status == "error" {
			return fmt.Errorf("%s", outcome.Error)
		}
		return nil
	})
}

func (s *Service) executeJob(ctx context.Context, job StoreJob) (*RunOutcome, error) {
	startedAt := time.Now().UnixMilli()

	// Build the Job struct for RunJob.
	runJob := Job{
		ID:        job.ID,
		AgentID:   job.AgentID,
		Command:   resolveJobCommand(job),
		Delivery:  job.Delivery,
		TimeoutMs: int64(job.Payload.TimeoutSeconds) * 1000,
		Enabled:   job.Enabled,
	}

	deps := RunnerDeps{
		Sessions:       nil, // Set by caller.
		Channels:       nil,
		Agent:          s.agent,
		Logger:         s.logger,
		DefaultChannel: s.cfg.DefaultChannel,
		DefaultTo:      s.cfg.DefaultTo,
	}

	outcome := RunJob(ctx, runJob, deps)

	// Update job state.
	state := job.State
	state.LastRunAtMs = startedAt
	state.LastDurationMs = outcome.DurationMs
	state.LastRunStatus = outcome.Status
	state.LastError = outcome.Error
	if outcome.Status == "ok" {
		state.ConsecutiveErrors = 0
	} else {
		state.ConsecutiveErrors++
	}
	if outcome.Delivery != nil {
		if outcome.Delivery.Delivered {
			state.LastDeliveryStatus = "delivered"
		} else {
			state.LastDeliveryStatus = "not-delivered"
			state.LastDeliveryError = outcome.Delivery.Error
		}
	}
	state.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, time.Now().UnixMilli())
	s.store.UpdateJobState(job.ID, state)

	// Append to run log.
	s.runLog.Append(RunLogEntry{
		Ts:             time.Now().UnixMilli(),
		JobID:          job.ID,
		Action:         "finished",
		Status:         outcome.Status,
		Error:          outcome.Error,
		Summary:        PickSummaryFromOutput(outcome.Output),
		DurationMs:     outcome.DurationMs,
		NextRunAtMs:    state.NextRunAtMs,
	})

	return &outcome, nil
}

func resolveJobIntervalMs(job StoreJob) int64 {
	switch job.Schedule.Kind {
	case "every":
		return job.Schedule.EveryMs
	case "cron":
		// For cron expressions, use 60s polling as the scheduler interval.
		// The actual timing is controlled by ComputeNextRunAtMs.
		return 60000
	case "at":
		return 0 // one-shot
	}
	return 0
}

func resolveJobCommand(job StoreJob) string {
	switch job.Payload.Kind {
	case "agentTurn":
		return job.Payload.Message
	case "systemEvent":
		return job.Payload.Text
	}
	return job.Payload.Message
}
