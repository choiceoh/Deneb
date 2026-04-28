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
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// Service manages the full cron job lifecycle: CRUD, scheduling, execution, delivery.
// All schedule kinds (at/every/cron) use a single timer-based system.
// Session GC for cron runs is handled by session.Manager's Kind-based retention
// (KindCron → 24h), so no separate reaper is needed.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	Service.mu  →  Store.mu  →  TrackedJob/runLog.mu
//	Service.listenersMu (independent — safe to hold under Service.mu)
//
// Any code that touches *Store directly must NOT call back into Service
// methods that acquire Service.mu, or it will deadlock.
type Service struct {
	mu sync.Mutex
	// store has its own Store.mu; always acquired under Service.mu per the
	// hierarchy above. Do not add callsites outside this package without
	// reviewing the ordering invariant.
	store   *Store
	runLog  *PersistentRunLog
	agent   AgentRunner
	logger  *slog.Logger
	cfg     ServiceConfig
	running bool
	stopCh  chan struct{}

	// Per-job execution guard: prevents concurrent execution of the same job.
	// Key = job ID, value = true while executing. Uses LoadOrStore for atomicity.
	runningJobs sync.Map

	// Scheduler loop coordination.
	//
	// wakeCh is a buffered (cap=1) channel used by Add/Update/Remove/Run/Wake
	// /applyJobResult to nudge the worker goroutine when state changes.
	// Sends are non-blocking; duplicate wakes coalesce. See signalWake.
	//
	// loopDone closes when the worker goroutine exits. Used by Stop to wait
	// for clean shutdown.
	//
	// nextWakeAtMs is the timestamp the worker plans to wake at next.
	// Updated by computeSleep; read by Status. Guarded by s.mu.
	wakeCh       chan struct{}
	loopDone     chan struct{}
	nextWakeAtMs int64

	// Event listeners. Guarded by listenersMu (separate from s.mu) so that
	// emit() can run while callers hold s.mu without re-entering the main
	// service mutex — otherwise Add/Remove/Wake deadlock on their own emit.
	listenersMu sync.RWMutex
	listeners   []CronEventListener

	// Loop tunables; see service_scheduler.go for semantics.
	idleInterval time.Duration
	errBackoff   time.Duration
	minLoopGap   time.Duration
}

// SetAgentRunner sets the agent runner after construction.
// This allows the cron service to be created before the chat handler is ready,
// then wired up once the chat handler is initialized.
func (s *Service) SetAgentRunner(agent AgentRunner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agent = agent
}

// SetTelegramPlugin sets the Telegram plugin for cron output delivery.
// Called after the Telegram plugin is created (late-bind pattern).
// Also sets DefaultTo from the plugin's primary chat ID if not already set.
func (s *Service) SetTelegramPlugin(plugin *telegram.Plugin) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.TelegramPlugin = plugin
	if s.cfg.DefaultTo == "" {
		if chatID := plugin.PrimaryChatID(); chatID != "" {
			s.cfg.DefaultTo = chatID
		}
	}
}

// SetSubagentPoller sets the poller for detecting descendant subagent completion.
// When set, cron jobs that produce interim responses will wait for subagent results.
func (s *Service) SetSubagentPoller(poller SubagentPoller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.SubagentPoller = poller
}

// SetTranscriptCloner sets the transcript cloner for subagent cron session support.
// Called after the chat handler's transcript store is available.
func (s *Service) SetTranscriptCloner(cloner TranscriptCloner, mainSessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.TranscriptCloner = cloner
	s.cfg.MainSessionKey = mainSessionKey
}

// SetMainSessionHandoff sets the callback that routes cron output through
// the main user session instead of delivering directly to the channel.
// See ServiceConfig.MainSessionHandoff for rationale and contract.
func (s *Service) SetMainSessionHandoff(fn func(ctx context.Context, channel, to, jobID, analysis string) (handled bool, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.MainSessionHandoff = fn
}

// NewService creates a new cron service.
func NewService(cfg ServiceConfig, agent AgentRunner, logger *slog.Logger) *Service {
	rl := NewPersistentRunLog(cfg.StorePath)
	rl.SetLogger(logger)
	return &Service{
		store:        NewStore(cfg.StorePath),
		runLog:       rl,
		agent:        agent,
		logger:       logger,
		cfg:          cfg,
		stopCh:       make(chan struct{}),
		wakeCh:       make(chan struct{}, 1),
		idleInterval: defaultIdleInterval,
		errBackoff:   defaultErrBackoff,
		minLoopGap:   defaultMinLoopGap,
	}
}
