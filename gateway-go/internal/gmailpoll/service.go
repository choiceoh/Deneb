package gmailpoll

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

const (
	defaultIntervalMin = 30
	defaultQuery       = "is:unread newer_than:1h"
	defaultMaxPerCycle = 5
	defaultModel       = "" // resolved from main config via initGmailPoll
	defaultPromptFile  = "~/.deneb/gmail-analysis-prompt.md"
	searchMaxRetries   = 2
)

// Notifier delivers messages to the user (e.g., via Telegram).
type Notifier interface {
	Notify(ctx context.Context, message string) error
}

// Config holds the service configuration.
type Config struct {
	IntervalMin int
	Query       string
	MaxPerCycle int
	Model       string
	PromptFile  string
	StateDir    string      // directory for state persistence (default ~/.deneb)
	LLMClient   *llm.Client // pre-configured LLM client from modelrole registry

	// Multi-stage pipeline deps (all optional — nil = skip that stage).
	LocalClient *llm.Client      // local AI for stage-1 extractors
	LocalModel  string           // local AI model name
	MemStore *memory.Store // for memory recall
}

// Service implements autonomous.PeriodicTask for Gmail polling.
// It fetches new unread emails, analyzes them via LLM, and sends reports
// through the configured notifier.
type Service struct {
	mu  sync.Mutex
	cfg Config
	log *slog.Logger

	gmailClient *gmail.Client
	llmClient   *llm.Client
	notifier    Notifier
	state       *stateStore
}

// NewService creates a gmail poll service.
// Register it with autonomous.Service via RegisterTask() to start polling.
func NewService(cfg Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.IntervalMin <= 0 {
		cfg.IntervalMin = defaultIntervalMin
	}
	if cfg.Query == "" {
		cfg.Query = defaultQuery
	}
	if cfg.MaxPerCycle <= 0 {
		cfg.MaxPerCycle = defaultMaxPerCycle
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.PromptFile == "" {
		cfg.PromptFile = defaultPromptFile
	}

	return &Service{
		cfg:       cfg,
		log:       logger.With("pkg", "gmailpoll"),
		llmClient: cfg.LLMClient,
		state:     newStateStore(cfg.StateDir),
	}
}

// SetNotifier sets the notifier for delivering analysis reports.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// --- autonomous.PeriodicTask interface ---

// Name returns the task identifier.
func (s *Service) Name() string { return "gmailpoll" }

// Interval returns the polling interval.
func (s *Service) Interval() time.Duration {
	return time.Duration(s.cfg.IntervalMin) * time.Minute
}

// isBusinessHours checks if the current time in KST is within weekday business hours (9:00~19:00).
func isBusinessHours() bool {
	kst := time.FixedZone("KST", 9*60*60)
	now := time.Now().In(kst)

	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	hour := now.Hour()
	return hour >= 9 && hour < 19
}

// Run executes a single polling cycle. Called by the autonomous service.
// Skips polling outside weekday business hours (KST 09:00~19:00).
func (s *Service) Run(ctx context.Context) error {
	if !isBusinessHours() {
		s.log.Debug("업무 시간 외 — 폴링 건너뜀")
		return nil
	}
	// Lazy-init Gmail client (retries on each call if previous init failed).
	s.mu.Lock()
	client := s.gmailClient
	s.mu.Unlock()

	if client == nil {
		c, err := gmail.GetClient()
		if err != nil {
			return fmt.Errorf("Gmail 클라이언트 초기화 실패: %w", err)
		}
		s.mu.Lock()
		s.gmailClient = c
		s.mu.Unlock()
		client = c
	}

	return s.poll(ctx, client)
}

// poll executes a single polling cycle: fetch new emails, analyze, and report.
func (s *Service) poll(ctx context.Context, client *gmail.Client) error {
	s.log.Debug("Gmail 폴링 시작")

	// Load persisted state.
	pollState, err := s.state.Load()
	if err != nil {
		s.log.Error("폴링 상태 로드 실패", "error", err)
		pollState = &PollState{}
	}

	// Search for emails with retry on transient failures.
	var messages []gmail.MessageSummary
	for attempt := 0; attempt <= searchMaxRetries; attempt++ {
		messages, err = client.Search(ctx, s.cfg.Query, s.cfg.MaxPerCycle+10)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt < searchMaxRetries {
			delay := time.Duration(1<<uint(attempt+1)) * time.Second // 2s, 4s
			s.log.Warn("Gmail 검색 실패, 재시도", "error", err, "attempt", attempt+1, "delay", delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	if err != nil {
		return fmt.Errorf("Gmail 검색 실패 (%d회 시도): %w", searchMaxRetries+1, err)
	}

	// Filter out already-seen messages.
	var newMessages []gmail.MessageSummary
	for _, msg := range messages {
		if !pollState.hasSeen(msg.ID) {
			newMessages = append(newMessages, msg)
		}
	}

	if len(newMessages) == 0 {
		s.log.Debug("새 메일 없음")
		pollState.LastPollAt = time.Now().UnixMilli()
		s.state.Save(pollState)
		return nil
	}

	// Cap to MaxPerCycle.
	if len(newMessages) > s.cfg.MaxPerCycle {
		newMessages = newMessages[:s.cfg.MaxPerCycle]
	}

	s.log.Info("새 메일 발견", "count", len(newMessages))

	// Fetch full details for all new messages.
	var details []*gmail.MessageDetail
	for _, summary := range newMessages {
		detail, err := client.GetMessage(ctx, summary.ID)
		if err != nil {
			s.log.Warn("메일 본문 조회 실패", "id", summary.ID, "error", err)
			pollState.markSeen(summary.ID)
			s.saveState(pollState)
			continue
		}
		details = append(details, detail)
	}

	if len(details) == 0 {
		pollState.LastPollAt = time.Now().UnixMilli()
		s.saveState(pollState)
		return nil
	}

	// Batch analysis: all emails → one consolidated report.
	report, err := s.batchAnalyze(ctx, client, details)
	if err != nil {
		s.log.Warn("배치 분석 실패", "error", err, "count", len(details))
		report = "(분석 실패)"
	}

	// Send single consolidated report.
	s.sendNotification(ctx, report)

	// Mark all as seen after successful notification.
	for _, summary := range newMessages {
		pollState.markSeen(summary.ID)
	}
	pollState.LastPollAt = time.Now().UnixMilli()
	s.saveState(pollState)
	return nil
}

// batchAnalyze runs the multi-stage pipeline on a batch of emails.
// For a single email, AnalyzeBatch delegates to AnalyzeEmailPipeline internally.
func (s *Service) batchAnalyze(ctx context.Context, gmailClient *gmail.Client, msgs []*gmail.MessageDetail) (string, error) {
	deps := PipelineDeps{
		GmailClient: gmailClient,
		LLMClient:   s.llmClient,
		LocalClient: s.cfg.LocalClient,
		LocalModel:  s.cfg.LocalModel,
		MainModel:   s.cfg.Model,
		MemStore: s.cfg.MemStore,
		Logger:      s.log,
	}

	s.log.Debug("batch analysis 실행", "count", len(msgs))
	return AnalyzeBatch(ctx, deps, msgs)
}

func (s *Service) saveState(state *PollState) {
	if err := s.state.Save(state); err != nil {
		s.log.Error("폴링 상태 저장 실패", "error", err)
	}
}

func (s *Service) sendNotification(ctx context.Context, message string) {
	s.mu.Lock()
	notifier := s.notifier
	s.mu.Unlock()

	if notifier == nil {
		s.log.Warn("알림 전송 불가: notifier가 설정되지 않음")
		return
	}

	notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := notifier.Notify(notifyCtx, message); err != nil {
		s.log.Error("알림 전송 실패", "error", err)
	}
}
