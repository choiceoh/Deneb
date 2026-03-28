package gmailpoll

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	defaultIntervalMin = 30
	defaultQuery       = "is:unread newer_than:1h"
	defaultMaxPerCycle = 5
	defaultModel       = "openrouter/meta-llama/llama-3.3-70b-instruct:free"
	defaultPromptFile  = "~/.deneb/gmail-analysis-prompt.md"
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
	StateDir    string // directory for state persistence (default ~/.deneb)
	LLMBaseURL  string // LLM API endpoint (e.g., sglang or OpenRouter)
	LLMAPIKey   string // optional API key for LLM endpoint
}

// Service periodically polls Gmail for new emails, analyzes them via LLM,
// and sends reports through the configured notifier.
type Service struct {
	mu  sync.Mutex
	cfg Config
	log *slog.Logger

	gmailClient *gmail.Client
	llmClient   *llm.Client
	notifier    Notifier
	state       *stateStore

	running   bool
	svcCtx    context.Context
	svcCancel context.CancelFunc
}

// NewService creates a gmail poll service. Call Start() to begin polling.
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

	var llmOpts []llm.ClientOption
	llmOpts = append(llmOpts, llm.WithLogger(logger))
	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMAPIKey, llmOpts...)

	return &Service{
		cfg:       cfg,
		log:       logger.With("pkg", "gmailpoll"),
		llmClient: llmClient,
		state:     newStateStore(cfg.StateDir),
	}
}

// SetNotifier sets the notifier for delivering analysis reports.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// Start begins the polling loop. Blocks until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.svcCtx, s.svcCancel = context.WithCancel(ctx)
	s.running = true
	s.mu.Unlock()

	// Initialize Gmail client.
	client, err := gmail.GetClient()
	if err != nil {
		s.log.Error("Gmail 클라이언트 초기화 실패 — gmailpoll 비활성화", "error", err)
		return
	}
	s.gmailClient = client

	s.log.Info("gmailpoll 서비스 시작",
		"interval", fmt.Sprintf("%d분", s.cfg.IntervalMin),
		"query", s.cfg.Query,
		"model", s.cfg.Model)

	interval := time.Duration(s.cfg.IntervalMin) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial poll after a short grace period.
	initialTimer := time.NewTimer(30 * time.Second)
	defer initialTimer.Stop()

	for {
		select {
		case <-s.svcCtx.Done():
			s.log.Info("gmailpoll 서비스 종료")
			return
		case <-initialTimer.C:
			s.poll()
		case <-ticker.C:
			s.poll()
		}
	}
}

// Stop gracefully shuts down the service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.svcCancel != nil {
		s.svcCancel()
	}
	s.running = false
}

// poll executes a single polling cycle: fetch new emails, analyze, and report.
func (s *Service) poll() {
	s.log.Debug("Gmail 폴링 시작")

	ctx, cancel := context.WithTimeout(s.svcCtx, 5*time.Minute)
	defer cancel()

	// Load persisted state.
	pollState, err := s.state.Load()
	if err != nil {
		s.log.Error("폴링 상태 로드 실패", "error", err)
		pollState = &PollState{}
	}

	// Search for emails.
	messages, err := s.gmailClient.Search(s.cfg.Query, s.cfg.MaxPerCycle+10)
	if err != nil {
		s.log.Warn("Gmail 검색 실패", "error", err)
		return
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
		return
	}

	// Cap to MaxPerCycle.
	if len(newMessages) > s.cfg.MaxPerCycle {
		newMessages = newMessages[:s.cfg.MaxPerCycle]
	}

	s.log.Info("새 메일 발견", "count", len(newMessages))

	// Load the analysis prompt (re-read each cycle so edits take effect).
	prompt := loadPrompt(s.cfg.PromptFile)

	for _, summary := range newMessages {
		// Fetch full message.
		detail, err := s.gmailClient.GetMessage(summary.ID)
		if err != nil {
			s.log.Warn("메일 본문 조회 실패", "id", summary.ID, "error", err)
			pollState.markSeen(summary.ID)
			continue
		}

		// Analyze via LLM.
		analysis, err := analyzeEmail(ctx, s.llmClient, s.cfg.Model, prompt, detail)
		if err != nil {
			s.log.Warn("메일 분석 실패", "id", summary.ID, "error", err)
			// Still report the email without analysis.
			analysis = "(분석 실패)"
		}

		// Send report.
		report := formatReport(detail, analysis)
		s.sendNotification(ctx, report)

		pollState.markSeen(summary.ID)
	}

	pollState.LastPollAt = time.Now().UnixMilli()
	if err := s.state.Save(pollState); err != nil {
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
