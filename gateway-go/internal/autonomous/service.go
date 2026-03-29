package autonomous

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Notifier delivers significant events to the user.
// Implemented by the server layer to send messages via Telegram or other channels.
type Notifier interface {
	Notify(ctx context.Context, message string) error
}

// EventListener receives lifecycle events.
type EventListener func(event CycleEvent)

// CycleEvent describes a lifecycle event for external consumers.
type CycleEvent struct {
	Type        string       `json:"type"` // "dreaming_started", "dreaming_completed", "dreaming_failed"
	DreamReport *DreamReport `json:"dreamReport,omitempty"`
	Ts          int64        `json:"ts"`
}

// Service manages the AuroraDream memory consolidation lifecycle
// and registered periodic tasks (e.g., Gmail polling).
type Service struct {
	mu     sync.Mutex
	logger *slog.Logger

	// Service-level context for propagation to async operations.
	svcCtx    context.Context
	svcCancel context.CancelFunc
	started   bool

	listeners []EventListener
	notifier  Notifier // optional: delivers significant events to the user

	// AuroraDream: memory consolidation.
	dreamer          Dreamer
	dreamRunning     bool
	dreamTimerCancel context.CancelFunc // independent dreaming check timer

	// Periodic tasks (gmail polling, etc.).
	tasks       []PeriodicTask
	taskCancels []context.CancelFunc
	taskStatus  map[string]*TaskStatus
}

// NewService creates a new autonomous service (dreaming + periodic tasks).
func NewService(logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	svcCtx, svcCancel := context.WithCancel(context.Background())
	return &Service{
		logger:     logger.With("pkg", "autonomous"),
		svcCtx:     svcCtx,
		svcCancel:  svcCancel,
		taskStatus: make(map[string]*TaskStatus),
	}
}

// RegisterTask adds a periodic task. Must be called before Start().
func (s *Service) RegisterTask(task PeriodicTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	s.taskStatus[task.Name()] = &TaskStatus{Name: task.Name()}
}

// GetTaskStatus returns the status of a registered task, or nil if not found.
func (s *Service) GetTaskStatus(name string) *TaskStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.taskStatus[name]
	if !ok {
		return nil
	}
	// Return a copy.
	cp := *st
	return &cp
}

// Start initializes the service and starts all registered periodic tasks.
// Dreaming timer is started when SetDreamer is called.
func (s *Service) Start() {
	s.mu.Lock()
	s.started = true
	tasks := make([]PeriodicTask, len(s.tasks))
	copy(tasks, s.tasks)
	s.mu.Unlock()

	for _, task := range tasks {
		ctx, cancel := context.WithCancel(s.svcCtx)
		s.mu.Lock()
		s.taskCancels = append(s.taskCancels, cancel)
		s.mu.Unlock()
		go s.runTaskLoop(ctx, task)
	}

	s.logger.Info("autonomous service started", "tasks", len(tasks))
}

// Stop shuts down the service and all periodic tasks.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel all periodic task loops.
	for _, cancel := range s.taskCancels {
		cancel()
	}
	s.taskCancels = nil

	if s.dreamTimerCancel != nil {
		s.dreamTimerCancel()
		s.dreamTimerCancel = nil
	}
	// Cancel service-level context to stop any in-flight async operations.
	if s.svcCancel != nil {
		s.svcCancel()
	}
	s.logger.Info("autonomous service stopped")
}

// OnEvent registers a listener for lifecycle events (dreaming).
func (s *Service) OnEvent(listener EventListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

// SetNotifier sets the optional notifier for delivering significant events
// to the user (e.g., via Telegram).
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// SetDreamer sets the optional dreamer for AuroraDream memory consolidation.
// When set, an independent periodic timer (every 30 min) checks dreaming
// conditions even when the user is inactive.
func (s *Service) SetDreamer(d Dreamer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dreamer = d

	// Start independent dreaming check timer if not already running.
	if d != nil && s.dreamTimerCancel == nil && s.svcCtx != nil {
		ctx, cancel := context.WithCancel(s.svcCtx)
		s.dreamTimerCancel = cancel
		go s.dreamTimerLoop(ctx)
	}
}

// dreamTimerLoop periodically checks dreaming conditions independently of
// user activity. This ensures time-based and data-volume triggers fire
// even when the user is idle.
func (s *Service) dreamTimerLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			dreamer := s.dreamer
			dreamRunning := s.dreamRunning
			s.mu.Unlock()

			if dreamer != nil && !dreamRunning && dreamer.ShouldDream(ctx) {
				s.runDreamingAsync()
			}
		}
	}
}

// IncrementDreamTurn records a conversation turn and triggers dreaming if conditions are met.
// Called from the chat handler after each agent turn.
func (s *Service) IncrementDreamTurn(ctx context.Context) {
	s.mu.Lock()
	dreamer := s.dreamer
	dreamRunning := s.dreamRunning
	s.mu.Unlock()

	if dreamer == nil {
		s.logger.Debug("aurora-dream: skipping turn increment, dreamer not configured")
		return
	}
	if dreamRunning {
		s.logger.Debug("aurora-dream: skipping turn increment, dream cycle in progress")
		return
	}

	dreamer.IncrementTurn(ctx)

	if dreamer.ShouldDream(ctx) {
		s.runDreamingAsync()
	}
}

// runDreamingAsync launches a dreaming cycle in a background goroutine.
func (s *Service) runDreamingAsync() {
	s.mu.Lock()
	if s.dreamRunning || s.dreamer == nil {
		s.mu.Unlock()
		return
	}
	s.dreamRunning = true
	dreamer := s.dreamer
	svcCtx := s.svcCtx
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.dreamRunning = false
			s.mu.Unlock()
		}()

		s.emit(CycleEvent{Type: "dreaming_started"})

		report, err := dreamer.RunDream(svcCtx)
		if err != nil {
			s.logger.Error("aurora-dream: cycle failed", "error", err)
			s.emit(CycleEvent{Type: "dreaming_failed"})
			s.notifyDreaming(nil, err)
			return
		}

		s.logger.Info("aurora-dream: cycle finished",
			"verified", report.FactsVerified,
			"merged", report.FactsMerged,
			"expired", report.FactsExpired,
			"patterns", report.PatternsExtracted,
			"durationMs", report.DurationMs,
		)
		s.emit(CycleEvent{Type: "dreaming_completed", DreamReport: report})
		s.notifyDreaming(report, nil)
	}()
}

// notifyDreaming sends a Telegram notification for dreaming cycle results.
func (s *Service) notifyDreaming(report *DreamReport, err error) {
	s.mu.Lock()
	notifier := s.notifier
	s.mu.Unlock()
	if notifier == nil {
		return
	}

	ctx, cancel := context.WithTimeout(s.svcCtx, 15*time.Second)
	defer cancel()

	var msg string
	if err != nil {
		msg = fmt.Sprintf("⚠️ Aurora Dream 실패: %s", truncateOutput(err.Error(), 100))
	} else if report != nil {
		msg = fmt.Sprintf("🌙 Aurora Dream 완료: 검증 %d, 병합 %d, 만료 %d, 패턴 %d (%.1fs)",
			report.FactsVerified, report.FactsMerged, report.FactsExpired,
			report.PatternsExtracted, float64(report.DurationMs)/1000)
	}
	if msg != "" {
		if notifyErr := notifier.Notify(ctx, msg); notifyErr != nil {
			s.logger.Warn("aurora-dream: notification failed", "error", notifyErr)
		}
	}
}

// runTaskLoop runs a periodic task with panic recovery and status tracking.
func (s *Service) runTaskLoop(ctx context.Context, task PeriodicTask) {
	name := task.Name()
	interval := task.Interval()
	s.logger.Info("periodic task started", "task", name, "interval", interval)

	// Initial grace period before first run (30 seconds).
	initialTimer := time.NewTimer(30 * time.Second)
	defer initialTimer.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	runOnce := func() {
		s.executeTask(ctx, task)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("periodic task stopped", "task", name)
			return
		case <-initialTimer.C:
			runOnce()
		case <-ticker.C:
			runOnce()
		}
	}
}

// executeTask runs a single task cycle with panic recovery and status bookkeeping.
func (s *Service) executeTask(ctx context.Context, task PeriodicTask) {
	name := task.Name()

	// Mark running.
	s.mu.Lock()
	st := s.taskStatus[name]
	if st != nil && st.Running {
		s.mu.Unlock()
		s.logger.Debug("periodic task still running, skipping", "task", name)
		return
	}
	if st != nil {
		st.Running = true
	}
	s.mu.Unlock()

	defer func() {
		// Panic recovery.
		if r := recover(); r != nil {
			s.logger.Error("periodic task panic recovered", "task", name, "panic", r)
			s.mu.Lock()
			if st != nil {
				st.Running = false
				st.ErrorCount++
				st.LastError = fmt.Sprintf("panic: %v", r)
			}
			s.mu.Unlock()
		}
	}()

	err := task.Run(ctx)

	s.mu.Lock()
	if st != nil {
		st.Running = false
		st.RunCount++
		st.LastRunAt = time.Now().UnixMilli()
		if err != nil {
			st.ErrorCount++
			st.LastError = err.Error()
		} else {
			st.LastError = ""
		}
	}
	s.mu.Unlock()

	if err != nil {
		s.logger.Warn("periodic task failed", "task", name, "error", err)
	}
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

// truncateOutput truncates a string to maxRunes runes, preserving UTF-8 boundaries.
func truncateOutput(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
