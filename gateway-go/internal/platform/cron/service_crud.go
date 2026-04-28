package cron

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

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

// ListPage returns a paginated list of jobs with filtering and sorting.
// Mirrors ops.ts listPage() with query search, enabled filter, sort options.
func (s *Service) ListPage(opts ListPageOptions) ListPageResult {
	storeData, err := s.store.Load()
	if err != nil {
		s.logger.Warn("failed to load cron store for list page", "error", err)
		return ListPageResult{Jobs: []StoreJob{}, Total: 0}
	}
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

// Job returns a single job by ID.
func (s *Service) Job(id string) *StoreJob {
	return s.store.Job(id)
}

// Add creates a new cron job, saves it, and schedules it.
// Validates "at" schedule timestamps and infers a name if not provided.
func (s *Service) Add(ctx context.Context, job StoreJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	NormalizeJobInput(&job)

	// Infer name from payload/schedule if not explicitly set.
	if strings.TrimSpace(job.Name) == "" {
		job.Name = InferLegacyName(job)
	}

	// Validate "at" schedule timestamps.
	now := time.Now().UnixMilli()
	if result := ValidateScheduleTimestamp(job.Schedule, now); !result.OK {
		return fmt.Errorf("invalid schedule: %s", result.Message)
	}
	if job.CreatedAtMs == 0 {
		job.CreatedAtMs = now
	}
	job.UpdatedAtMs = now
	job.State.NextRunAtMs = ComputeNextRunAtMs(job.Schedule, now)

	if err := s.store.AddJob(job); err != nil {
		return fmt.Errorf("save cron job: %w", err)
	}

	if job.Enabled && s.running {
		s.signalWake()
	}

	s.emit(CronEvent{Type: "job_added", JobID: job.ID})
	return nil
}

// Update patches a job and reschedules it.
func (s *Service) Update(ctx context.Context, id string, patch func(*StoreJob)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job := s.store.Job(id)
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

	// Nudge the scheduler loop to pick up the updated schedule.
	if job.Enabled && s.running {
		s.signalWake()
	}
	return nil
}

// Remove deletes a job and unschedules it.
func (s *Service) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.RemoveJob(id); err != nil {
		return err
	}
	if s.running {
		s.signalWake()
	}
	s.emit(CronEvent{Type: "job_removed", JobID: id})
	return nil
}

// Run executes a job immediately (or only if due, depending on mode).
//
// "manual" trigger semantics: when invoked outside the scheduler loop
// (e.g. via cron.run RPC or operator nudge), applyJobResult preserves
// a future NextRunAtMs rather than advancing it. This avoids the bug
// where running morning-letter at 22:00 yesterday pushed today's 08:00
// fire to tomorrow. If the job was overdue at the moment of manual
// run, NextRunAtMs is still recomputed forward (no point keeping a
// stale past timestamp). See applyJobResult for the policy.
func (s *Service) Run(ctx context.Context, id string, mode string) (*RunOutcome, error) {
	job := s.store.Job(id)
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
	outcome := s.executeJobFullWithTrigger(ctx, *job, triggerManual)
	return &outcome, nil
}

// EnqueueRun queues a job for async execution.
func (s *Service) EnqueueRun(ctx context.Context, id string, mode string) error {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("panic in async cron run", "job", id, "panic", r)
			}
		}()
		if _, err := s.Run(ctx, id, mode); err != nil {
			s.logger.Warn("async cron run failed", "job", id, "error", err)
		}
	}()
	return nil
}

// Wake nudges the scheduler loop to re-evaluate immediately. Used by
// external triggers that want due jobs picked up without waiting for the
// loop's current sleep to elapse.
func (s *Service) Wake(ctx context.Context, mode string, text string) {
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if running && mode == "now" {
		s.signalWake()
	}
	s.emit(CronEvent{Type: "wake", Status: mode})
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
