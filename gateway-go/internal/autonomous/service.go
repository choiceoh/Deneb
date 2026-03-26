package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// AgentRunner abstracts the agent execution so the autonomous package does not
// depend on chat.Handler or protocol (which pull in CGo/FFI).
type AgentRunner interface {
	// RunAgentTurn executes an agent turn and blocks until completion.
	// Returns the agent's text output.
	RunAgentTurn(ctx context.Context, sessionKey, message string) (output string, err error)
}

// ServiceConfig configures the autonomous service.
type ServiceConfig struct {
	GoalStorePath  string
	CycleTimeoutMs int64 // per-cycle timeout (default 10 min)
}

// Service manages the autonomous goal-driven execution lifecycle.
type Service struct {
	mu     sync.Mutex
	goals  *GoalStore
	agent  AgentRunner
	logger *slog.Logger
	cfg    ServiceConfig
	runLog *RunLog

	// Cycle state (in-memory, synced to disk via GoalStore.CycleState).
	cycleRunning   bool
	cycleCancel    context.CancelFunc
	lastCycleAt    int64
	consecutiveErr int
	lastOutcome    *CycleOutcome
	totalCycles    int
	totalErrors    int

	// Service-level context for propagation to async operations.
	svcCtx    context.Context
	svcCancel context.CancelFunc

	// Phase 2: attention-based triggering.
	attention *Attention
	enabled   bool // false = timer paused, manual cycle.run still works
	listeners []EventListener
}

// EventListener receives autonomous cycle events.
type EventListener func(event CycleEvent)

// CycleEvent describes a cycle lifecycle event for external consumers.
type CycleEvent struct {
	Type       string       `json:"type"` // "cycle_started", "cycle_completed", "cycle_failed", "cycle_skipped"
	Outcome    *CycleOutcome `json:"outcome,omitempty"`
	Ts         int64        `json:"ts"`
}

// CycleOutcome describes the result of a single decision cycle.
type CycleOutcome struct {
	Status      string       `json:"status"` // "ok", "error", "skipped"
	Output      string       `json:"output,omitempty"`
	GoalUpdates []GoalUpdate `json:"goalUpdates,omitempty"`
	DurationMs  int64        `json:"durationMs"`
	Error       string       `json:"error,omitempty"`
	GoalWorked  string       `json:"goalWorked,omitempty"` // ID of the goal acted on
}

// ServiceStatus is the snapshot returned by Status().
type ServiceStatus struct {
	Running        bool          `json:"running"`
	Enabled        bool          `json:"enabled"`
	CycleRunning   bool          `json:"cycleRunning"`
	ActiveGoals    int           `json:"activeGoals"`
	TotalGoals     int           `json:"totalGoals"`
	LastCycleAt    int64         `json:"lastCycleAt,omitempty"`
	LastOutcome    *CycleOutcome `json:"lastOutcome,omitempty"`
	ConsecutiveErr int           `json:"consecutiveErrors"`
	TotalCycles    int           `json:"totalCycles"`
	TotalErrors    int           `json:"totalErrors"`
}

// NewService creates a new autonomous service.
func NewService(cfg ServiceConfig, agent AgentRunner, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CycleTimeoutMs <= 0 {
		cfg.CycleTimeoutMs = 10 * 60 * 1000 // 10 minutes
	}
	store := NewGoalStore(cfg.GoalStorePath)
	svcCtx, svcCancel := context.WithCancel(context.Background())
	return &Service{
		goals:     store,
		agent:     agent,
		logger:    logger.With("pkg", "autonomous"),
		cfg:       cfg,
		runLog:    NewRunLog(cfg.GoalStorePath, logger),
		enabled:   true,
		svcCtx:    svcCtx,
		svcCancel: svcCancel,
	}
}

// Start initializes the service, restores persisted state, and starts the attention timer.
func (s *Service) Start(ctx context.Context, attentionCfg AttentionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Restore persisted cycle state from disk.
	if cs, err := s.goals.LoadCycleState(); err == nil {
		s.consecutiveErr = cs.ConsecutiveErrors
		s.lastCycleAt = cs.LastRunAtMs
		s.totalCycles = cs.TotalCycles
		s.totalErrors = cs.TotalErrors
	}

	s.attention = NewAttention(s, attentionCfg, s.logger)
	s.attention.StartTimer(ctx)
	s.logger.Info("autonomous service started",
		"totalCycles", s.totalCycles,
		"consecutiveErrors", s.consecutiveErr)
}

// Stop shuts down the service and cancels any running cycle.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.attention != nil {
		s.attention.StopTimer()
	}
	if s.cycleCancel != nil {
		s.cycleCancel()
		s.cycleCancel = nil
	}
	// Cancel service-level context to stop any in-flight async cycles.
	if s.svcCancel != nil {
		s.svcCancel()
	}
	s.logger.Info("autonomous service stopped")
}

// Status returns the current service state.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, _ := s.goals.List()
	active, _ := s.goals.ActiveGoals()

	return ServiceStatus{
		Running:        s.attention != nil && s.attention.IsTimerActive(),
		Enabled:        s.enabled,
		CycleRunning:   s.cycleRunning,
		ActiveGoals:    len(active),
		TotalGoals:     len(all),
		LastCycleAt:    s.lastCycleAt,
		LastOutcome:    s.lastOutcome,
		ConsecutiveErr: s.consecutiveErr,
		TotalCycles:    s.totalCycles,
		TotalErrors:    s.totalErrors,
	}
}

// Goals returns the goal store for direct CRUD operations.
func (s *Service) Goals() *GoalStore {
	return s.goals
}

// SetEnabled toggles the autonomous timer. When disabled, the timer doesn't
// trigger cycles but manual RunCycle/RunCycleAsync still works.
func (s *Service) SetEnabled(enabled bool) {
	s.mu.Lock()
	s.enabled = enabled
	s.mu.Unlock()
	s.logger.Info("autonomous mode toggled", "enabled", enabled)
}

// Enabled returns whether the timer is active.
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// OnEvent registers a listener for cycle events.
func (s *Service) OnEvent(listener EventListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

func (s *Service) emit(event CycleEvent) {
	event.Ts = time.Now().UnixMilli()
	s.mu.Lock()
	listeners := make([]EventListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()
	for _, l := range listeners {
		l(event)
	}
}

// AddGoal adds a goal and triggers attention if available.
func (s *Service) AddGoal(description, priority string) (Goal, error) {
	goal, err := s.goals.Add(description, priority)
	if err != nil {
		return Goal{}, err
	}
	s.logger.Info("goal added", "id", goal.ID, "priority", goal.Priority)
	// Trigger immediate cycle via attention.
	if s.attention != nil {
		s.attention.Push(Signal{
			Kind:     SignalGoalAdded,
			Priority: SignalPriorityHigh,
			Context:  goal.Description,
		})
	}
	return goal, nil
}

// RunCycle executes a single decision cycle. Returns immediately if a cycle is
// already running. Safe to call concurrently.
func (s *Service) RunCycle(ctx context.Context) (*CycleOutcome, error) {
	s.mu.Lock()
	if s.cycleRunning {
		s.mu.Unlock()
		return nil, fmt.Errorf("cycle already running")
	}
	if s.agent == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent runner not configured")
	}

	timeout := time.Duration(s.cfg.CycleTimeoutMs) * time.Millisecond
	cycleCtx, cancel := context.WithTimeout(ctx, timeout)
	s.cycleRunning = true
	s.cycleCancel = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.cycleRunning = false
		s.cycleCancel = nil
		s.mu.Unlock()
	}()

	outcome := s.executeCycle(cycleCtx)
	s.applyCycleOutcome(outcome)

	return outcome, nil
}

// RunCycleAsync starts a cycle in the background. Returns immediately.
// Used by the RPC handler to avoid blocking. Uses the service-level context
// so async cycles are cancelled when the service stops.
func (s *Service) RunCycleAsync() error {
	s.mu.Lock()
	if s.cycleRunning {
		s.mu.Unlock()
		return fmt.Errorf("cycle already running")
	}
	if s.agent == nil {
		s.mu.Unlock()
		return fmt.Errorf("agent runner not configured")
	}
	svcCtx := s.svcCtx
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(svcCtx,
			time.Duration(s.cfg.CycleTimeoutMs)*time.Millisecond)
		defer cancel()
		if _, err := s.RunCycle(ctx); err != nil {
			s.logger.Warn("async cycle failed", "error", err)
		}
	}()
	return nil
}

// StopCycle cancels a running cycle.
func (s *Service) StopCycle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cycleCancel != nil {
		s.cycleCancel()
	}
}

// ConsecutiveErrors returns the current consecutive error count (for attention backoff).
func (s *Service) ConsecutiveErrors() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consecutiveErr
}

// executeCycle performs the actual decision cycle work.
func (s *Service) executeCycle(ctx context.Context) *CycleOutcome {
	startedAt := time.Now().UnixMilli()

	// 1. Load active goals.
	active, err := s.goals.ActiveGoals()
	if err != nil {
		return &CycleOutcome{
			Status:     "error",
			Error:      fmt.Sprintf("failed to load goals: %s", err),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	}

	// 2. Skip if no active goals.
	if len(active) == 0 {
		s.logger.Debug("skipping cycle: no active goals")
		return &CycleOutcome{
			Status:     "skipped",
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	}

	// 3. Load last cycle state for prompt continuity.
	cycleState, _ := s.goals.LoadCycleState()

	// 4. Build decision prompt with recently-changed goals for context.
	recentlyChanged, _ := s.goals.RecentlyChanged(30 * 60 * 1000) // last 30 min
	prompt := buildDecisionPrompt(active, &cycleState, recentlyChanged...)
	s.logger.Info("running autonomous cycle",
		"goals", len(active),
		"topGoal", active[0].Description)

	s.emit(CycleEvent{Type: "cycle_started"})

	output, runErr := s.agent.RunAgentTurn(ctx, autonomousSessionKey, prompt)
	if runErr != nil {
		errMsg := runErr.Error()
		if ctx.Err() != nil {
			errMsg = "cycle timeout: " + errMsg
		}
		return &CycleOutcome{
			Status:     "error",
			Error:      errMsg,
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	}

	// 5. Parse goal updates (with fallback).
	activeIDs := make([]string, len(active))
	for i, g := range active {
		activeIDs[i] = g.ID
	}
	updates := parseGoalUpdates(output, activeIDs)

	// 6. Validate and apply updates to goal store.
	activeIDSet := make(map[string]bool, len(active))
	for _, g := range active {
		activeIDSet[g.ID] = true
	}

	var goalWorked string
	for _, u := range updates {
		// Reject updates referencing non-existent goal IDs.
		if !activeIDSet[u.ID] {
			s.logger.Warn("ignoring goal update for unknown ID", "id", u.ID)
			continue
		}
		if updateErr := s.goals.Update(u.ID, u.Status, u.Note); updateErr != nil {
			s.logger.Warn("failed to apply goal update", "id", u.ID, "error", updateErr)
		} else if goalWorked == "" {
			goalWorked = u.ID
		}
	}

	return &CycleOutcome{
		Status:      "ok",
		Output:      truncateOutput(output, 2000),
		GoalUpdates: updates,
		DurationMs:  time.Now().UnixMilli() - startedAt,
		GoalWorked:  goalWorked,
	}
}

// applyCycleOutcome updates in-memory and persistent state after a cycle.
func (s *Service) applyCycleOutcome(outcome *CycleOutcome) {
	s.mu.Lock()
	now := time.Now().UnixMilli()
	s.lastCycleAt = now
	s.lastOutcome = outcome
	s.totalCycles++

	if outcome.Status == "error" {
		s.consecutiveErr++
		s.totalErrors++
	} else {
		s.consecutiveErr = 0
	}

	// Build summary for next cycle's prompt.
	summary := buildCycleSummary(outcome)

	cs := CycleState{
		LastRunAtMs:       now,
		LastStatus:        outcome.Status,
		LastError:         outcome.Error,
		LastSummary:       summary,
		ConsecutiveErrors: s.consecutiveErr,
		TotalCycles:       s.totalCycles,
		TotalErrors:       s.totalErrors,
	}

	// Persist cycle state to disk while still holding the lock,
	// preventing a concurrent cycle from seeing stale persisted state.
	if err := s.goals.UpdateCycleState(cs); err != nil {
		s.logger.Warn("failed to persist cycle state", "error", err)
	}
	s.mu.Unlock()

	// Append to run log.
	s.runLog.Append(RunLogEntry{
		Timestamp:  now,
		Status:     outcome.Status,
		DurationMs: outcome.DurationMs,
		GoalWorked: outcome.GoalWorked,
		Error:      outcome.Error,
		UpdateCount: len(outcome.GoalUpdates),
	})

	// Purge old completed goals periodically (every 50 cycles).
	if s.totalCycles%50 == 0 {
		if purged, err := s.goals.PurgeCompleted(); err == nil && purged > 0 {
			s.logger.Info("purged completed goals", "count", purged)
		}
	}

	// Emit cycle event for external consumers.
	eventType := "cycle_completed"
	if outcome.Status == "error" {
		eventType = "cycle_failed"
	} else if outcome.Status == "skipped" {
		eventType = "cycle_skipped"
	}
	s.emit(CycleEvent{Type: eventType, Outcome: outcome})

	s.logger.Info("cycle completed",
		"status", outcome.Status,
		"durationMs", outcome.DurationMs,
		"goalUpdates", len(outcome.GoalUpdates),
		"consecutiveErrors", s.consecutiveErr)
}

// buildCycleSummary creates a short summary for the next cycle's prompt.
func buildCycleSummary(outcome *CycleOutcome) string {
	switch outcome.Status {
	case "skipped":
		return "이전 사이클: 활성 목표 없어 건너뜀"
	case "error":
		return fmt.Sprintf("이전 사이클: 오류 발생 — %s", truncateOutput(outcome.Error, 100))
	case "ok":
		if len(outcome.GoalUpdates) == 0 {
			return "이전 사이클: 완료 (목표 업데이트 없음)"
		}
		var parts []string
		for _, u := range outcome.GoalUpdates {
			if u.Note != "" {
				parts = append(parts, fmt.Sprintf("[%s] %s", u.ID, u.Note))
			}
		}
		if len(parts) == 0 {
			return "이전 사이클: 완료"
		}
		return "이전 사이클 진행: " + truncateOutput(strings.Join(parts, "; "), 300)
	default:
		return ""
	}
}

// MarshalStatus returns the status as JSON bytes (for RPC responses).
func (s *Service) MarshalStatus() json.RawMessage {
	status := s.Status()
	data, _ := json.Marshal(status)
	return data
}

// RecentRuns returns the last N run log entries.
func (s *Service) RecentRuns(n int) []RunLogEntry {
	return s.runLog.Recent(n)
}

// truncateOutput truncates a string to maxRunes runes, preserving UTF-8 boundaries.
func truncateOutput(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
