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

// SetAgentRunner sets the agent runner after construction.
// This allows the cron service to be created before the chat handler is ready,
// then wired up once the chat handler is initialized.
func (s *Service) SetAgentRunner(agent AgentRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agent = agent
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
