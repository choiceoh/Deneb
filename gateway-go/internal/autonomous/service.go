package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	GoalStorePath string
}

// Service manages the autonomous goal-driven execution lifecycle.
type Service struct {
	mu     sync.Mutex
	goals  *GoalStore
	agent  AgentRunner
	logger *slog.Logger
	cfg    ServiceConfig

	// Cycle state.
	cycleRunning   bool
	cycleCancel    context.CancelFunc
	lastCycleAt    int64
	consecutiveErr int
	lastOutcome    *CycleOutcome

	// Phase 2: attention-based triggering.
	attention *Attention
}

// CycleOutcome describes the result of a single decision cycle.
type CycleOutcome struct {
	Status      string       `json:"status"` // "ok", "error", "skipped"
	Output      string       `json:"output,omitempty"`
	GoalUpdates []GoalUpdate `json:"goalUpdates,omitempty"`
	DurationMs  int64        `json:"durationMs"`
	Error       string       `json:"error,omitempty"`
}

// ServiceStatus is the snapshot returned by Status().
type ServiceStatus struct {
	Running        bool         `json:"running"`
	CycleRunning   bool         `json:"cycleRunning"`
	ActiveGoals    int          `json:"activeGoals"`
	TotalGoals     int          `json:"totalGoals"`
	LastCycleAt    int64        `json:"lastCycleAt,omitempty"`
	LastOutcome    *CycleOutcome `json:"lastOutcome,omitempty"`
	ConsecutiveErr int          `json:"consecutiveErrors"`
}

// NewService creates a new autonomous service.
func NewService(cfg ServiceConfig, agent AgentRunner, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		goals:  NewGoalStore(cfg.GoalStorePath),
		agent:  agent,
		logger: logger.With("pkg", "autonomous"),
		cfg:    cfg,
	}
}

// Start initializes the service and starts the attention timer (Phase 2).
func (s *Service) Start(ctx context.Context, attentionCfg AttentionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attention = NewAttention(s, attentionCfg, s.logger)
	s.attention.StartTimer(ctx)
	s.logger.Info("autonomous service started")
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
	s.logger.Info("autonomous service stopped")
}

// Status returns the current service state.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, _ := s.goals.List()
	active, _ := s.goals.ActiveGoals()

	return ServiceStatus{
		Running:        s.attention != nil,
		CycleRunning:   s.cycleRunning,
		ActiveGoals:    len(active),
		TotalGoals:     len(all),
		LastCycleAt:    s.lastCycleAt,
		LastOutcome:    s.lastOutcome,
		ConsecutiveErr: s.consecutiveErr,
	}
}

// Goals returns the goal store for direct CRUD operations.
func (s *Service) Goals() *GoalStore {
	return s.goals
}

// AddGoal adds a goal and triggers attention if available.
func (s *Service) AddGoal(description, priority string) (Goal, error) {
	goal, err := s.goals.Add(description, priority)
	if err != nil {
		return Goal{}, err
	}
	// Trigger immediate cycle via attention (Phase 2).
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

	cycleCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
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

	s.mu.Lock()
	s.lastCycleAt = time.Now().UnixMilli()
	s.lastOutcome = outcome
	if outcome.Status == "error" {
		s.consecutiveErr++
	} else {
		s.consecutiveErr = 0
	}
	s.mu.Unlock()

	return outcome, nil
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

	// 3. Build decision prompt and run agent turn.
	prompt := buildDecisionPrompt(active)
	s.logger.Info("running autonomous cycle", "goals", len(active))

	output, runErr := s.agent.RunAgentTurn(ctx, autonomousSessionKey, prompt)
	if runErr != nil {
		return &CycleOutcome{
			Status:     "error",
			Error:      runErr.Error(),
			DurationMs: time.Now().UnixMilli() - startedAt,
		}
	}

	// 4. Parse goal updates from output.
	updates := parseGoalUpdates(output)
	for _, u := range updates {
		if updateErr := s.goals.Update(u.ID, u.Status, u.Note); updateErr != nil {
			s.logger.Warn("failed to apply goal update", "id", u.ID, "error", updateErr)
		}
	}

	return &CycleOutcome{
		Status:      "ok",
		Output:      truncateOutput(output, 2000),
		GoalUpdates: updates,
		DurationMs:  time.Now().UnixMilli() - startedAt,
	}
}

// MarshalStatus returns the status as JSON bytes (for RPC responses).
func (s *Service) MarshalStatus() json.RawMessage {
	status := s.Status()
	data, _ := json.Marshal(status)
	return data
}

func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
