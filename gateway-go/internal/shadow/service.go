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

	// Extended modules (initialized in NewService).
	buildWatcher       *BuildWatcher
	contextPrefetcher  *ContextPrefetcher
	memoryConsolidator *MemoryConsolidator
	sessionContinuity  *SessionContinuity
	usageAnalytics     *UsageAnalytics
	errorLearner       *ErrorLearner
	codeReviewer       *CodeReviewer
	cronSuggester      *CronSuggester

	listeners []EventListener
}

// NewService creates a shadow monitoring service. Call Start() to begin.
func NewService(cfg Config) *Service {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	svcCtx, svcCancel := context.WithCancel(context.Background())
	svc := &Service{
		cfg:        cfg,
		svcCtx:     svcCtx,
		svcCancel:  svcCancel,
		sessionKey: "shadow:" + cfg.MainSessionKey,
	}
	// Initialize all sub-modules.
	svc.buildWatcher = newBuildWatcher(svc)
	svc.contextPrefetcher = newContextPrefetcher(svc)
	svc.memoryConsolidator = newMemoryConsolidator(svc)
	svc.sessionContinuity = newSessionContinuity(svc)
	svc.usageAnalytics = newUsageAnalytics(svc)
	svc.errorLearner = newErrorLearner(svc)
	svc.codeReviewer = newCodeReviewer(svc)
	svc.cronSuggester = newCronSuggester(svc)
	return svc
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
	}

	// Subscribe to transcript appends for real-time monitoring.
	if s.cfg.TranscriptWriter != nil {
		s.unsubAppend = s.cfg.TranscriptWriter.OnAppend(s.onTranscriptAppend)
	}

	// Subscribe to session lifecycle events.
	if s.cfg.Sessions != nil {
		s.unsubEvents = s.cfg.Sessions.EventBusRef().Subscribe(s.onSessionEvent)
	}

	// Start periodic digest loop.
	go s.digestLoop()

	s.cfg.Logger.Info("shadow monitoring started", "session", s.cfg.MainSessionKey)
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
// Filters to only monitor the main session's messages, then dispatches
// to all analysis modules.
func (s *Service) onTranscriptAppend(sessionKey string, msg json.RawMessage) {
	if sessionKey != s.cfg.MainSessionKey {
		return
	}
	s.mu.Lock()
	s.lastActivity = time.Now().UnixMilli()
	s.mu.Unlock()

	// Core task detection.
	s.analyzeMessage(sessionKey, msg)

	// Parse once for all modules.
	var parsed struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg, &parsed); err != nil || parsed.Content == "" {
		return
	}

	// Dispatch to extended modules.
	s.usageAnalytics.RecordActivity()
	s.memoryConsolidator.OnMessageForMemory(sessionKey, msg)
	s.sessionContinuity.OnMessage(msg)
	s.contextPrefetcher.OnMessageForTopic(parsed.Content)
	s.errorLearner.OnMessageForErrors(sessionKey, parsed.Content)
	s.cronSuggester.OnMessageForCron(parsed.Content)

	// Detect code changes for background review.
	if DetectCodeChange(parsed.Content) {
		s.codeReviewer.OnCodeChangeDetected(truncate(parsed.Content, 100))
	}

	// Detect git push for build watching.
	if branch, detected := detectPush(parsed.Content); detected {
		s.buildWatcher.OnPushDetected(branch)
	}

	// Record topic for analytics.
	if topic := detectTopic(parsed.Content); topic != "" {
		s.usageAnalytics.RecordTopic(topic)
	}
}

// onSessionEvent handles session lifecycle events.
func (s *Service) onSessionEvent(event session.Event) {
	// Track all session transitions for analytics (not just main session).
	if event.NewStatus != "" && event.OldStatus == session.StatusRunning {
		sess := s.cfg.Sessions.Get(event.Key)
		if sess != nil && !sess.Kind.IsInternal() {
			var startedAt, endedAt int64
			if sess.StartedAt != nil {
				startedAt = *sess.StartedAt
			}
			endedAt = time.Now().UnixMilli()
			s.usageAnalytics.RecordSessionRun(event.Key, string(event.NewStatus), startedAt, endedAt)
		}
	}

	// Track error resolution (failed → running → done).
	if event.NewStatus == session.StatusDone && event.OldStatus == session.StatusRunning {
		s.errorLearner.RecordResolution(event.Key, "세션 정상 완료")
	}

	// Health monitoring only for main session.
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

	// Append analytics summary.
	analyticsDigest := s.usageAnalytics.FormatDailyDigest()
	if analyticsDigest != "" {
		msg += "\n\n" + analyticsDigest
	}

	// Append cron suggestions count.
	cronSuggestions := s.cronSuggester.GetSuggestions()
	if len(cronSuggestions) > 0 {
		msg += fmt.Sprintf("\n\n⏰ 크론 작업 제안: %d건", len(cronSuggestions))
	}

	// Save continuity snapshot before digest.
	s.sessionContinuity.SaveSnapshot()

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

// ExtendedStatus returns the full shadow status including all module states.
func (s *Service) ExtendedStatus() ExtendedStatus {
	base := s.Status()
	ext := ExtendedStatus{ServiceStatus: base}

	report := s.usageAnalytics.GetReport()
	ext.Analytics = &report
	ext.CronSuggestions = s.cronSuggester.GetSuggestions()
	ext.RecentReviews = s.codeReviewer.GetRecentReviews()
	ext.ExtractedFacts = len(s.memoryConsolidator.GetExtractedFacts(""))
	ext.RecurringErrors = len(s.errorLearner.GetRecurringErrors())
	ext.Continuity = s.sessionContinuity.LoadSnapshot()
	ext.PrefetchedCtx = s.contextPrefetcher.GetPrefetchedContexts()

	return ext
}

// BuildWatcher returns the build watcher module.
func (s *Service) BuildWatcher() *BuildWatcher { return s.buildWatcher }

// ContextPrefetcher returns the context prefetcher module.
func (s *Service) ContextPrefetcher() *ContextPrefetcher { return s.contextPrefetcher }

// MemoryConsolidator returns the memory consolidator module.
func (s *Service) MemoryConsolidator() *MemoryConsolidator { return s.memoryConsolidator }

// SessionContinuity returns the session continuity module.
func (s *Service) SessionContinuity() *SessionContinuity { return s.sessionContinuity }

// UsageAnalytics returns the usage analytics module.
func (s *Service) UsageAnalytics() *UsageAnalytics { return s.usageAnalytics }

// ErrorLearner returns the error learner module.
func (s *Service) ErrorLearner() *ErrorLearner { return s.errorLearner }

// CodeReviewer returns the code reviewer module.
func (s *Service) CodeReviewer() *CodeReviewer { return s.codeReviewer }

// CronSuggester returns the cron suggester module.
func (s *Service) CronSuggester() *CronSuggester { return s.cronSuggester }

// notifyCtx returns a context with a 15-second timeout for notifications.
func (s *Service) notifyCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(s.svcCtx, 15*time.Second)
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
