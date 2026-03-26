package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const (
	defaultCheckInterval = 15 * time.Minute
	checkTimeout         = 3 * time.Minute
	llmTimeout           = 90 * time.Second
	llmMaxTokens         = 2048
)

// Service is the copilot background system monitor.
// It periodically runs system health checks and uses the local sglang model
// to analyze logs and detect anomalies. Issues are reported via Telegram.
type Service struct {
	mu  sync.Mutex
	cfg ServiceConfig
	log *slog.Logger

	llmClient *llm.Client
	notifier  Notifier

	// State.
	running       bool
	enabled       bool
	lastCheckAt   int64
	lastResults   []CheckResult
	totalChecks   int
	totalWarnings int
	startedAt     time.Time

	// Lifecycle.
	svcCtx    context.Context
	svcCancel context.CancelFunc
}

// NewService creates a copilot service. Call Start() to begin background checks.
func NewService(cfg ServiceConfig, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.CheckIntervalMin <= 0 {
		cfg.CheckIntervalMin = 15
	}
	if cfg.SglangBaseURL == "" {
		cfg.SglangBaseURL = "http://127.0.0.1:30000/v1"
	}
	if cfg.SglangModel == "" {
		cfg.SglangModel = "Qwen/Qwen3.5-35B-A3B"
	}

	client := llm.NewClient(cfg.SglangBaseURL, "", llm.WithLogger(logger))

	return &Service{
		cfg:       cfg,
		log:       logger.With("pkg", "copilot"),
		llmClient: client,
		enabled:   true,
	}
}

// SetNotifier sets the Telegram (or other) notifier for delivering alerts.
func (s *Service) SetNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifier = n
}

// Start begins the background check loop. Blocks until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.svcCtx, s.svcCancel = context.WithCancel(ctx)
	s.running = true
	s.startedAt = time.Now()
	s.mu.Unlock()

	s.log.Info("copilot service started",
		"interval", fmt.Sprintf("%dm", s.cfg.CheckIntervalMin))

	interval := time.Duration(s.cfg.CheckIntervalMin) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run an initial check shortly after startup (30s grace period).
	initialTimer := time.NewTimer(30 * time.Second)
	defer initialTimer.Stop()

	for {
		select {
		case <-s.svcCtx.Done():
			s.log.Info("copilot service stopped")
			return
		case <-initialTimer.C:
			s.runCheckCycle()
		case <-ticker.C:
			s.runCheckCycle()
		}
	}
}

// Stop gracefully shuts down the copilot service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.svcCancel != nil {
		s.svcCancel()
	}
	s.running = false
}

// Enable turns on periodic checks.
func (s *Service) Enable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = true
	s.log.Info("copilot enabled")
}

// Disable turns off periodic checks. Manual RunCheck still works.
func (s *Service) Disable() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = false
	s.log.Info("copilot disabled")
}

// Status returns the current service state.
func (s *Service) Status() ServiceStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	var uptime string
	if !s.startedAt.IsZero() {
		uptime = time.Since(s.startedAt).Truncate(time.Second).String()
	}

	return ServiceStatus{
		Running:       s.running,
		Enabled:       s.enabled,
		LastCheckAt:   s.lastCheckAt,
		LastResults:   s.lastResults,
		TotalChecks:   s.totalChecks,
		TotalWarnings: s.totalWarnings,
		Uptime:        uptime,
	}
}

// RunCheck executes all checks immediately (can be called via RPC).
func (s *Service) RunCheck(ctx context.Context) []CheckResult {
	results := s.executeChecks(ctx)
	s.recordResults(results)
	return results
}

// runCheckCycle is the internal periodic check runner.
func (s *Service) runCheckCycle() {
	s.mu.Lock()
	enabled := s.enabled
	s.mu.Unlock()

	if !enabled {
		return
	}

	ctx, cancel := context.WithTimeout(s.svcCtx, checkTimeout)
	defer cancel()

	s.log.Debug("copilot check cycle starting")
	results := s.executeChecks(ctx)
	s.recordResults(results)

	// Notify on warnings/critical issues.
	s.notifyIssues(results)
}

// executeChecks runs all registered checks and returns results.
func (s *Service) executeChecks(ctx context.Context) []CheckResult {
	checks := []func(context.Context) CheckResult{
		s.checkSglangHealth,
		s.checkDiskUsage,
		s.checkGPUStatus,
		s.checkProcessHealth,
		s.checkGatewayLogs,
	}

	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		if ctx.Err() != nil {
			break
		}
		results = append(results, check(ctx))
	}
	return results
}

// recordResults saves check results and updates counters.
func (s *Service) recordResults(results []CheckResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastCheckAt = time.Now().UnixMilli()
	s.lastResults = results
	s.totalChecks++

	for _, r := range results {
		if r.Status == "warning" || r.Status == "critical" {
			s.totalWarnings++
		}
	}
}

// notifyIssues sends Telegram alerts for warning/critical results.
func (s *Service) notifyIssues(results []CheckResult) {
	s.mu.Lock()
	notifier := s.notifier
	s.mu.Unlock()

	if notifier == nil {
		return
	}

	var issues []string
	for _, r := range results {
		if r.Status == "warning" || r.Status == "critical" {
			icon := "⚠️"
			if r.Status == "critical" {
				icon = "🚨"
			}
			issues = append(issues, fmt.Sprintf("%s [%s] %s: %s", icon, r.Status, r.Name, r.Message))
		}
	}

	if len(issues) == 0 {
		return
	}

	msg := fmt.Sprintf("🤖 Copilot 시스템 점검 알림\n\n%s", strings.Join(issues, "\n"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := notifier.Notify(ctx, msg); err != nil {
		s.log.Error("copilot notification failed", "error", err)
	}
}

// askLocalLLM sends a single-turn question to the local sglang model.
// Used by checks that need AI analysis (e.g., log analysis).
func (s *Service) askLocalLLM(ctx context.Context, system, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	req := llm.ChatRequest{
		Model:     s.cfg.SglangModel,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		System:    llm.SystemString(system),
		MaxTokens: llmMaxTokens,
		Stream:    true,
	}

	events, err := s.llmClient.StreamChatOpenAI(ctx, req)
	if err != nil {
		return "", fmt.Errorf("sglang: %w", err)
	}

	// Collect the full response.
	var sb strings.Builder
	for ev := range events {
		if ctx.Err() != nil {
			break
		}
		if ev.Type == "content_block_delta" {
			var delta struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal(ev.Payload, &delta) == nil {
				sb.WriteString(delta.Delta.Text)
			}
		}
	}

	return sb.String(), nil
}
