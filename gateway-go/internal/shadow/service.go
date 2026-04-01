package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
)

// Config holds the dependencies for the shadow monitoring service.
type Config struct {
	// MainSessionKey is the primary session to monitor (e.g., "direct:telegram:main").
	MainSessionKey string
	// Sessions provides session lifecycle tracking.
	Sessions *session.Manager
	// TranscriptWriter provides real-time message append notifications.
	TranscriptWriter *transcript.Writer
	// Notifier delivers significant events to the user (e.g., Telegram).
	Notifier Notifier
	// Logger is the structured logger.
	Logger *slog.Logger
}

// Service monitors main session conversations in the background and performs
// bookkeeping: task detection, health monitoring, and periodic digests.
type Service struct {
	cfg Config
	mu  sync.Mutex

	svcCtx    context.Context
	svcCancel context.CancelFunc
	started   bool

	// Shadow session identity.
	sessionKey string // "shadow:<mainKey>"
	startedAt  int64  // unix ms

	// Unsub functions for cleanup.
	unsubAppend func()
	unsubEvents func()

	// Tracked insights (guarded by mu).
	pendingTasks []TrackedTask
	topicHistory []TopicChange
	healthAlerts []HealthAlert
	lastActivity int64 // unix ms of last observed message

	// Failure tracking for escalation.
	recentFailures []int64 // timestamps of recent failures

	listeners []EventListener
}

// NewService creates a shadow monitoring service. Call Start() to begin.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	svcCtx, svcCancel := context.WithCancel(context.Background())
	return &Service{
		cfg:        cfg,
		svcCtx:     svcCtx,
		svcCancel:  svcCancel,
		sessionKey: "shadow:" + cfg.MainSessionKey,
	}
}

// Start creates the shadow session and begins monitoring.
func (s *Service) Start() {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.startedAt = time.Now().UnixMilli()
	s.mu.Unlock()

	// Create shadow session in the session manager.
	if s.cfg.Sessions != nil {
		s.cfg.Sessions.Create(s.sessionKey, session.KindShadow)
		s.cfg.Logger.Info("shadow session created", "key", s.sessionKey, "monitoring", s.cfg.MainSessionKey)
	}

	// Subscribe to transcript appends for real-time monitoring.
	if s.cfg.TranscriptWriter != nil {
		s.unsubAppend = s.cfg.TranscriptWriter.OnAppend(s.onTranscriptAppend)
		s.cfg.Logger.Info("shadow monitoring: subscribed to transcript appends")
	}

	// Subscribe to session lifecycle events.
	if s.cfg.Sessions != nil {
		s.unsubEvents = s.cfg.Sessions.EventBusRef().Subscribe(s.onSessionEvent)
		s.cfg.Logger.Info("shadow monitoring: subscribed to session events")
	}

	// Start periodic digest loop.
	go s.digestLoop()

	s.cfg.Logger.Info("shadow monitoring service started",
		"sessionKey", s.sessionKey,
		"mainSession", s.cfg.MainSessionKey,
	)
}

// Stop shuts down the shadow monitoring service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.unsubAppend != nil {
		s.unsubAppend()
		s.unsubAppend = nil
	}
	if s.unsubEvents != nil {
		s.unsubEvents()
		s.unsubEvents = nil
	}
	if s.svcCancel != nil {
		s.svcCancel()
	}
	s.cfg.Logger.Info("shadow monitoring service stopped")
}

// OnEvent registers a listener for shadow lifecycle events (for broadcast).
func (s *Service) OnEvent(listener EventListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

// Status returns a snapshot of the shadow service state.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ServiceStatus{
		Active:       s.started,
		SessionKey:   s.sessionKey,
		MonitoredKey: s.cfg.MainSessionKey,
		StartedAt:    s.startedAt,
		PendingTasks: countPending(s.pendingTasks),
		Alerts:       len(s.healthAlerts),
		LastActivity: s.lastActivity,
	}
}

// PendingTasks returns a copy of all pending tracked tasks.
func (s *Service) PendingTasks() []TrackedTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []TrackedTask
	for _, t := range s.pendingTasks {
		if t.Status == "pending" {
			result = append(result, t)
		}
	}
	return result
}

// DismissTask marks a task as dismissed by ID.
func (s *Service) DismissTask(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.pendingTasks {
		if s.pendingTasks[i].ID == id && s.pendingTasks[i].Status == "pending" {
			s.pendingTasks[i].Status = "dismissed"
			return true
		}
	}
	return false
}

// onTranscriptAppend is the callback for transcript.Writer.OnAppend.
// Filters to only monitor the main session's messages.
func (s *Service) onTranscriptAppend(sessionKey string, msg json.RawMessage) {
	if sessionKey != s.cfg.MainSessionKey {
		return
	}
	s.mu.Lock()
	s.lastActivity = time.Now().UnixMilli()
	s.mu.Unlock()

	s.analyzeMessage(sessionKey, msg)
}

// onSessionEvent handles session lifecycle events.
func (s *Service) onSessionEvent(event session.Event) {
	// Only monitor main session events.
	if event.Key != s.cfg.MainSessionKey {
		return
	}
	s.checkHealthIndicators(event)
}

// digestLoop sends periodic summaries every digestInterval.
func (s *Service) digestLoop() {
	ticker := time.NewTicker(digestInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.svcCtx.Done():
			return
		case <-ticker.C:
			s.sendDigest()
		}
	}
}

// sendDigest sends a Telegram notification summarizing pending tasks and alerts.
func (s *Service) sendDigest() {
	s.mu.Lock()
	pending := countPending(s.pendingTasks)
	alertCount := len(s.healthAlerts)
	notifier := s.cfg.Notifier
	s.mu.Unlock()

	if pending == 0 && alertCount == 0 {
		return // nothing to report
	}
	if notifier == nil {
		return
	}

	msg := fmt.Sprintf("📋 Shadow 모니터링 요약\n• 대기 중인 작업: %d건\n• 건강 알림: %d건", pending, alertCount)

	// Append top 3 pending tasks.
	s.mu.Lock()
	var topTasks []TrackedTask
	for _, t := range s.pendingTasks {
		if t.Status == "pending" {
			topTasks = append(topTasks, t)
			if len(topTasks) >= 3 {
				break
			}
		}
	}
	s.mu.Unlock()

	for i, t := range topTasks {
		msg += fmt.Sprintf("\n  %d. %s", i+1, truncate(t.Content, 60))
	}

	ctx, cancel := context.WithTimeout(s.svcCtx, 15*time.Second)
	defer cancel()
	if err := notifier.Notify(ctx, msg); err != nil {
		s.cfg.Logger.Warn("shadow digest notification failed", "error", err)
	}

	s.emit(ShadowEvent{Type: "digest", Payload: map[string]any{
		"pendingTasks": pending,
		"alerts":       alertCount,
	}})
}

// emit broadcasts a ShadowEvent to all registered listeners.
func (s *Service) emit(event ShadowEvent) {
	event.Ts = time.Now().UnixMilli()
	s.mu.Lock()
	listeners := make([]EventListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()
	for _, l := range listeners {
		l(event)
	}
}

func countPending(tasks []TrackedTask) int {
	n := 0
	for _, t := range tasks {
		if t.Status == "pending" {
			n++
		}
	}
	return n
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
