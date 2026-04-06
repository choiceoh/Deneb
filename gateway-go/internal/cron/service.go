// service.go — Full cron service with CRUD, scheduling, execution, and delivery.
// Mirrors src/cron/service.ts (61 LOC), service/ops.ts (593 LOC),
// service/jobs.ts (909 LOC), service/job-runner.ts (290 LOC),
// service/execute-job.ts (298 LOC), service/job-result.ts (285 LOC),
// service/timer.ts (227 LOC), service/timer-helpers.ts (232 LOC),
// service/state.ts (170 LOC).
package cron

import (
	"context"
	"log/slog"
	"sync"
)

// Service manages the full cron job lifecycle: CRUD, scheduling, execution, delivery.
// Session GC for cron runs is handled by session.Manager's Kind-based retention
// (KindCron → 24h), so no separate reaper is needed.
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

	// Per-job execution guard: prevents concurrent execution of the same job.
	// Key = job ID, value = true while executing. Uses LoadOrStore for atomicity.
	runningJobs sync.Map

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

// SetAgentRunner sets the agent runner after construction.
// This allows the cron service to be created before the chat handler is ready,
// then wired up once the chat handler is initialized.
func (s *Service) SetAgentRunner(agent AgentRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agent = agent
}

// SetTranscriptCloner sets the transcript cloner for shadow session support.
// Called after the chat handler's transcript store is available.
func (s *Service) SetTranscriptCloner(cloner TranscriptCloner, mainSessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.TranscriptCloner = cloner
	s.cfg.MainSessionKey = mainSessionKey
}

// NewService creates a new cron service.
func NewService(cfg ServiceConfig, agent AgentRunner, logger *slog.Logger) *Service {
	rl := NewPersistentRunLog(cfg.StorePath)
	rl.SetLogger(logger)
	return &Service{
		scheduler:     NewScheduler(logger),
		store:         NewStore(cfg.StorePath),
		runLog:        rl,
		agent:         agent,
		logger:        logger,
		cfg:           cfg,
		stopCh:        make(chan struct{}),
		minRefireGap:  2000,
		maxTimerDelay: 60000,
	}
}
