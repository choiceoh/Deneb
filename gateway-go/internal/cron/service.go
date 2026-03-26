// service.go — Full cron service with CRUD, scheduling, execution, and delivery.
// Mirrors src/cron/service.ts (61 LOC), service/ops.ts (593 LOC),
// service/jobs.ts (909 LOC), service/job-runner.ts (290 LOC),
// service/execute-job.ts (298 LOC), service/job-result.ts (285 LOC),
// service/timer.ts (227 LOC), service/timer-helpers.ts (232 LOC),
// service/state.ts (170 LOC).
package cron

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// CronEvent describes a cron system event for listeners.
type CronEvent struct {
	Type   string `json:"type"` // "job_started", "job_finished", "job_failed", "job_added", "job_removed"
	JobID  string `json:"jobId,omitempty"`
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	Ts     int64  `json:"ts"`
}

// CronEventListener receives cron events.
type CronEventListener func(event CronEvent)

// Service manages the full cron job lifecycle: CRUD, scheduling, execution, delivery.
type Service struct {
	mu        sync.Mutex
	scheduler *Scheduler
	store     *Store
	runLog    *PersistentRunLog
	reaper    *SessionReaper
	agent     AgentRunner
	logger    *slog.Logger
	cfg       ServiceConfig
	running   bool
	stopCh    chan struct{}

	// Timer state (mirrors TS timer.ts).
	timerMu      sync.Mutex
	timerCancel  context.CancelFunc
	nextWakeAtMs int64

	// Event listeners.
	listeners []CronEventListener

	// Error backoff: minimum 2s between refire to prevent spin loops (#17821).
	lastFireAtMs  int64
	minRefireGap  int64 // milliseconds (default 2000)
	maxTimerDelay int64 // milliseconds (default 60000)
}

// ServiceConfig configures the cron service.
type ServiceConfig struct {
	StorePath      string
	DefaultChannel string
	DefaultTo      string
	Enabled        bool
	RetentionMs    int64 // session retention (0 = default 24h)
}

// NewService creates a new cron service.
func NewService(cfg ServiceConfig, agent AgentRunner, logger *slog.Logger) *Service {
	retentionMs := ResolveRetentionMs(cfg.RetentionMs)
	return &Service{
		scheduler:     NewScheduler(logger),
		store:         NewStore(cfg.StorePath),
		runLog:        NewPersistentRunLog(cfg.StorePath),
		reaper:        NewSessionReaper(retentionMs, logger),
		agent:         agent,
		logger:        logger,
		cfg:           cfg,
		stopCh:        make(chan struct{}),
		minRefireGap:  2000,
		maxTimerDelay: 60000,
	}
}

// OnEvent registers an event listener.
func (s *Service) OnEvent(listener CronEventListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

func (s *Service) emit(event CronEvent) {
	event.Ts = time.Now().UnixMilli()
	s.mu.Lock()
	listeners := make([]CronEventListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()
	for _, l := range listeners {
		l(event)
	}
}

// Start loads jobs from the store and begins scheduling.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	storeData, err := s.store.Load()
	if err != nil {
		return fmt.Errorf("load cron store: %w", err)
	}

	// Schedule all enabled jobs.
	scheduled := 0
	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if err := s.scheduleJobLocked(ctx, job); err != nil {
			s.logger.Warn("failed to schedule cron job", "id", job.ID, "error", err)
			continue
		}
		scheduled++
	}

	// Check for missed jobs (jobs that should have fired during downtime).
	s.recoverMissedJobsLocked(ctx, storeData)

	// Arm the next-wake timer.
	s.armTimerLocked(ctx)

	s.running = true
	s.logger.Info("cron service started", "total", len(storeData.Jobs), "scheduled", scheduled)
	return nil
}

// Stop cancels all scheduled jobs and the timer.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.scheduler.Close()
	s.disarmTimerLocked()
	s.running = false
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.logger.Info("cron service stopped")
}

// Status returns the service status.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	running := s.running
	nextWake := s.nextWakeAtMs
	s.mu.Unlock()

	tasks := s.scheduler.List()
	return ServiceStatus{
		Running:     running,
		TaskCount:   len(tasks),
		NextRunAtMs: nextWake,
		Tasks:       tasks,
	}
}

type ServiceStatus struct {
	Running     bool         `json:"running"`
	TaskCount   int          `json:"taskCount"`
	NextRunAtMs int64        `json:"nextRunAtMs,omitempty"`
	Tasks       []TaskStatus `json:"tasks,omitempty"`
}

// --- CRUD Operations (mirrors ops.ts) ---

// List returns all jobs from the store.
func (s *Service) List(opts *ListOptions) ([]StoreJob, error) {
	storeData, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	if opts != nil && !opts.IncludeDisabled {
		var filtered []StoreJob
		for _, j := range storeData.Jobs {
			if j.Enabled {
				filtered = append(filtered, j)
			}
		}
		return filtered, nil
	}
	return storeData.Jobs, nil
}

type ListOptions struct {
	IncludeDisabled bool
}

// ListPage returns a paginated list of jobs with filtering and sorting.
// Mirrors ops.ts listPage() with query search, enabled filter, sort options.
func (s *Service) ListPage(opts ListPageOptions) ListPageResult {
	storeData, _ := s.store.Load()
	if storeData == nil {
		return ListPageResult{Jobs: []StoreJob{}, Total: 0}
	}

	jobs := storeData.Jobs

	// Filter by enabled/disabled.
	if !opts.IncludeDisabled {
		var filtered []StoreJob
		for _, j := range jobs {
			if j.Enabled {
				filtered = append(filtered, j)
			}
		}
		jobs = filtered
	}

	// Text search across name, ID, payload text/message.
	if opts.Query != "" {
		query := strings.ToLower(opts.Query)
		var filtered []StoreJob
		for _, j := range jobs {
			if strings.Contains(strings.ToLower(j.Name), query) ||
				strings.Contains(strings.ToLower(j.ID), query) ||
				strings.Contains(strings.ToLower(j.Payload.Text), query) ||
				strings.Contains(strings.ToLower(j.Payload.Message), query) {
				filtered = append(filtered, j)
			}
		}
		jobs = filtered
	}

	// Sort.
	sortJobs(jobs, opts.SortBy, opts.SortDir)

	total := len(jobs)
	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return ListPageResult{Jobs: []StoreJob{}, Total: total, Offset: offset, Limit: limit}
	}
	end := offset + limit
	if end > total {
		end = total
	}

	return ListPageResult{
		Jobs:    jobs[offset:end],
		Total:   total,
		Offset:  offset,
		Limit:   limit,
		HasMore: end < total,
	}
}

type ListPageOptions struct {
	Limit           int
	Offset          int
	IncludeDisabled bool
	Query           string // text search across name, ID, payload
	SortBy          string // "name", "nextRunAtMs", "updatedAtMs" (default: nextRunAtMs)
	SortDir         string // "asc" or "desc" (default: asc)
}

type ListPageResult struct {
	Jobs    []StoreJob `json:"jobs"`
	Total   int        `json:"total"`
	Offset  int        `json:"offset"`
	Limit   int        `json:"limit"`
	HasMore bool       `json:"hasMore"`
}

// GetJob returns a single job by ID.
func (s *Service) GetJob(id string) *StoreJob {
	return s.store.GetJob(id)
}

// Add creates a new cron job, saves it, and schedules it.
func (s *Service) Add(ctx context.Context, job StoreJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	NormalizeJobInput(&job)

	now := time.Now().UnixMilli()
	if job.CreatedAtMs == 0 {
		job.CreatedAtMs = now
	}
	job.UpdatedAtMs = now
	job.State.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, now)

	if err := s.store.AddJob(job); err != nil {
		return fmt.Errorf("save cron job: %w", err)
	}

	if job.Enabled && s.running {
		s.scheduleJobLocked(ctx, job)
		s.armTimerLocked(ctx)
	}

	s.emit(CronEvent{Type: "job_added", JobID: job.ID})
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
	NormalizeJobInput(job)
	job.UpdatedAtMs = time.Now().UnixMilli()
	job.State.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, time.Now().UnixMilli())

	if err := s.store.AddJob(*job); err != nil {
		return err
	}

	// Reschedule.
	s.scheduler.Unregister(id)
	if job.Enabled && s.running {
		s.scheduleJobLocked(ctx, *job)
		s.armTimerLocked(ctx)
	}
	return nil
}

// Remove deletes a job and unschedules it.
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.scheduler.Unregister(id)
	if err := s.store.RemoveJob(id); err != nil {
		return err
	}
	s.emit(CronEvent{Type: "job_removed", JobID: id})
	return nil
}

// Run executes a job immediately (or only if due, depending on mode).
func (s *Service) Run(ctx context.Context, id string, mode string) (*RunOutcome, error) {
	job := s.store.GetJob(id)
	if job == nil {
		return nil, fmt.Errorf("job %q not found", id)
	}

	if mode == "due" {
		now := time.Now().UnixMilli()
		if job.State.NextRunAtMs > now {
			return &RunOutcome{Status: "skipped"}, nil
		}
	}

	s.emit(CronEvent{Type: "job_started", JobID: id})
	outcome := s.executeJobFull(ctx, *job)
	return &outcome, nil
}

// EnqueueRun queues a job for async execution.
func (s *Service) EnqueueRun(ctx context.Context, id string, mode string) error {
	go func() {
		s.Run(ctx, id, mode)
	}()
	return nil
}

// Wake triggers immediate processing of due jobs.
func (s *Service) Wake(ctx context.Context, mode string, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mode == "now" {
		s.fireTimerLocked(ctx)
	}
	s.emit(CronEvent{Type: "wake", Status: mode})
}

// --- Timer management (mirrors timer.ts, timer-helpers.ts) ---

func (s *Service) armTimerLocked(ctx context.Context) {
	s.disarmTimerLocked()

	// Compute next wake time across all jobs.
	storeData, _ := s.store.Load()
	if storeData == nil {
		return
	}

	now := time.Now().UnixMilli()
	var nextWake int64
	for _, job := range storeData.Jobs {
		if !job.Enabled || job.State.NextRunAtMs <= 0 {
			continue
		}
		if nextWake == 0 || job.State.NextRunAtMs < nextWake {
			nextWake = job.State.NextRunAtMs
		}
	}

	if nextWake == 0 {
		s.nextWakeAtMs = 0
		return
	}

	delayMs := nextWake - now
	if delayMs < s.minRefireGap {
		delayMs = s.minRefireGap
	}
	if delayMs > s.maxTimerDelay {
		delayMs = s.maxTimerDelay
	}

	s.nextWakeAtMs = now + delayMs

	timerCtx, cancel := context.WithCancel(ctx)
	s.timerCancel = cancel

	go func() {
		timer := time.NewTimer(time.Duration(delayMs) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timerCtx.Done():
			return
		case <-timer.C:
			s.mu.Lock()
			s.fireTimerLocked(ctx)
			s.armTimerLocked(ctx)
			s.mu.Unlock()
		}
	}()
}

func (s *Service) disarmTimerLocked() {
	if s.timerCancel != nil {
		s.timerCancel()
		s.timerCancel = nil
	}
	s.nextWakeAtMs = 0
}

func (s *Service) fireTimerLocked(ctx context.Context) {
	now := time.Now().UnixMilli()

	// Enforce minimum refire gap.
	if now-s.lastFireAtMs < s.minRefireGap {
		return
	}
	s.lastFireAtMs = now

	// Find and run due jobs.
	storeData, _ := s.store.Load()
	if storeData == nil {
		return
	}

	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs > 0 && job.State.NextRunAtMs <= now {
			// Execute asynchronously.
			jobCopy := job
			go func() {
				s.executeJobFull(ctx, jobCopy)
			}()
		}
	}
}

// recoverMissedJobsLocked runs jobs that should have fired during downtime.
func (s *Service) recoverMissedJobsLocked(ctx context.Context, storeData *CronStoreFile) {
	now := time.Now().UnixMilli()
	for _, job := range storeData.Jobs {
		if !job.Enabled {
			continue
		}
		if job.State.NextRunAtMs > 0 && job.State.NextRunAtMs <= now {
			s.logger.Info("recovering missed cron job", "id", job.ID,
				"scheduledAt", job.State.NextRunAtMs, "missedBy", now-job.State.NextRunAtMs)
			jobCopy := job
			go func() {
				s.executeJobFull(ctx, jobCopy)
			}()
		}
	}
}

// --- Job execution (mirrors execute-job.ts, job-result.ts) ---

func (s *Service) executeJobFull(ctx context.Context, job StoreJob) RunOutcome {
	startedAt := time.Now().UnixMilli()
	s.emit(CronEvent{Type: "job_started", JobID: job.ID})

	// Apply timeout (mirrors timeout-policy.ts).
	timeoutMs := ResolveCronJobTimeoutMs(job)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	// Resolve delivery target.
	deliveryCfg := job.Delivery
	target, targetErr := ResolveDeliveryTarget(deliveryCfg, s.cfg.DefaultChannel, s.cfg.DefaultTo)

	// Build agent turn params.
	command := job.Payload.Message
	if job.Payload.Kind == "systemEvent" {
		command = job.Payload.Text
	}

	var outcome RunOutcome

	if targetErr != nil && !isBestEffort(deliveryCfg) {
		outcome = RunOutcome{
			Status:     "error",
			Error:      fmt.Sprintf("delivery target error: %s", targetErr),
			StartedAt:  startedAt,
			EndedAt:    time.Now().UnixMilli(),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	} else if s.agent != nil {
		output, runErr := s.agent.RunAgentTurn(runCtx, AgentTurnParams{
			SessionKey: fmt.Sprintf("cron:%s:%d", job.ID, startedAt),
			AgentID:    job.AgentID,
			Command:    command,
			Channel:    safeStr(target, func(t *DeliveryTarget) string { return t.Channel }),
			To:         safeStr(target, func(t *DeliveryTarget) string { return t.To }),
			AccountID:  safeStr(target, func(t *DeliveryTarget) string { return t.AccountID }),
		})

		if runErr != nil {
			outcome = RunOutcome{
				Status:     "error",
				Error:      runErr.Error(),
				StartedAt:  startedAt,
				EndedAt:    time.Now().UnixMilli(),
				DurationMs: time.Now().UnixMilli() - startedAt,
			}
		} else {
			outcome = RunOutcome{
				Status:     "ok",
				Output:     output,
				StartedAt:  startedAt,
				EndedAt:    time.Now().UnixMilli(),
				DurationMs: time.Now().UnixMilli() - startedAt,
			}
		}
	} else {
		outcome = RunOutcome{
			Status:     "error",
			Error:      "no agent runner configured",
			StartedAt:  startedAt,
			EndedAt:    time.Now().UnixMilli(),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	}

	// Apply result to job state (mirrors job-result.ts applyJobResult).
	s.applyJobResult(job, outcome)

	// Log run.
	s.runLog.Append(RunLogEntry{
		Ts:          time.Now().UnixMilli(),
		JobID:       job.ID,
		Action:      "finished",
		Status:      outcome.Status,
		Error:       outcome.Error,
		Summary:     PickSummaryFromOutput(outcome.Output),
		DurationMs:  outcome.DurationMs,
		NextRunAtMs: ComputeNextRunAtMs(job.Schedule, time.Now().UnixMilli()),
	})

	s.emit(CronEvent{Type: "job_finished", JobID: job.ID, Status: outcome.Status})
	return outcome
}

// applyJobResult updates the job state after execution (mirrors job-result.ts).
func (s *Service) applyJobResult(job StoreJob, outcome RunOutcome) {
	state := job.State
	state.LastRunAtMs = outcome.StartedAt
	state.LastDurationMs = outcome.DurationMs
	state.LastRunStatus = outcome.Status
	state.RunningAtMs = 0

	if outcome.Status == "ok" {
		state.ConsecutiveErrors = 0
		state.LastError = ""
	} else {
		state.ConsecutiveErrors++
		state.LastError = outcome.Error
		// Auto-disable after 10 consecutive schedule errors.
		if state.ConsecutiveErrors >= 10 {
			state.ScheduleErrorCount++
		}
	}

	if outcome.Delivery != nil {
		if outcome.Delivery.Delivered {
			state.LastDeliveryStatus = "delivered"
			state.LastDeliveryError = ""
		} else {
			state.LastDeliveryStatus = "not-delivered"
			state.LastDeliveryError = outcome.Delivery.Error
		}
	}

	// Handle deleteAfterRun one-shot jobs.
	nowMs := time.Now().UnixMilli()
	state.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, nowMs)

	s.store.UpdateJobState(job.ID, state)
}

// ShouldSendFailureAlert checks if a failure alert should be sent for a job.
// Respects cooldown period and error status.
func ShouldSendFailureAlert(state JobState, failureAlert *CronFailureAlert, outcomeStatus string, nowMs int64) bool {
	if outcomeStatus != "error" {
		return false
	}
	if failureAlert == nil {
		return false
	}
	// Respect "after" threshold.
	if failureAlert.After > 0 && state.ConsecutiveErrors < failureAlert.After {
		return false
	}
	// Respect cooldown.
	if failureAlert.CooldownMs > 0 && state.LastFailureAlertAtMs > 0 {
		if nowMs-state.LastFailureAlertAtMs < failureAlert.CooldownMs {
			return false
		}
	}
	return true
}

// sortJobs sorts jobs by the given field and direction (mirrors ops.ts sortJobs).
func sortJobs(jobs []StoreJob, sortBy, sortDir string) {
	if sortBy == "" {
		sortBy = "nextRunAtMs"
	}
	asc := sortDir != "desc"

	sort.SliceStable(jobs, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "name":
			less = strings.ToLower(jobs[i].Name) < strings.ToLower(jobs[j].Name)
		case "updatedAtMs":
			less = jobs[i].UpdatedAtMs < jobs[j].UpdatedAtMs
		default: // "nextRunAtMs"
			// 0 (no next run) sorts last.
			ni, nj := jobs[i].State.NextRunAtMs, jobs[j].State.NextRunAtMs
			if ni == 0 && nj == 0 {
				less = jobs[i].ID < jobs[j].ID
			} else if ni == 0 {
				less = false
			} else if nj == 0 {
				less = true
			} else {
				less = ni < nj
			}
		}
		if !asc {
			return !less
		}
		return less
	})
}

// --- Internal helpers ---

func (s *Service) scheduleJobLocked(ctx context.Context, job StoreJob) error {
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
		outcome := s.executeJobFull(taskCtx, jobCopy)
		if outcome.Status == "error" {
			return errors.New(outcome.Error)
		}
		return nil
	})
}

func resolveJobIntervalMs(job StoreJob) int64 {
	switch job.Schedule.Kind {
	case "every":
		return job.Schedule.EveryMs
	case "cron":
		return 60000 // poll every 60s, actual timing via ComputeNextRunAtMs
	case "at":
		return 0
	}
	return 0
}

func resolveJobCommand(job StoreJob) string {
	if job.Payload.Kind == "systemEvent" {
		return job.Payload.Text
	}
	return job.Payload.Message
}

func isBestEffort(cfg *JobDeliveryConfig) bool {
	return cfg != nil && cfg.BestEffort
}

func safeStr(target *DeliveryTarget, fn func(*DeliveryTarget) string) string {
	if target == nil {
		return ""
	}
	return fn(target)
}
