package gmailpoll

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

const (
	defaultIntervalMin = 30
	defaultQuery       = "is:unread newer_than:1h"
	defaultMaxPerCycle = 5
	defaultModel       = "" // resolved from main config via initGmailPoll
	defaultPromptFile  = "~/.deneb/gmail-analysis-prompt.md"
	searchMaxRetries   = 2
)

// Notifier delivers messages to the user (e.g., to the native client).
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

	// PromptOverride returns a native-UI edited prompt by ID. When it returns a
	// non-empty override, it takes precedence over PromptFile; otherwise the
	// existing file/default fallback remains intact.
	PromptOverride func(id string) (string, bool)

	// Multi-stage pipeline deps (all optional — nil = skip that stage).
	LocalClient *llm.Client // local AI for stage-1 extractors
	LocalModel  string      // local AI model name

	// DiaryDir is the wiki diary directory for logging analysis results.
	// Empty = diary logging disabled.
	DiaryDir string

	// OnAnalyzed, if set, is invoked once per individually-analyzed email in
	// a poll cycle so the server layer can cache the result and write a
	// per-message wiki page (Related = identified projects). nil = skip
	// per-email persistence (consolidated report/diary still run). The poll path
	// logs callback failures and continues; LMTP ingest returns the error so the
	// durable queue can retry instead of marking the mail done.
	OnAnalyzed func(msg *gmail.MessageDetail, res AnalysisResult) error

	// OnDelivered, if set, is invoked after a poll/ingest notification is handed
	// to the configured Notifier without error. The server uses it to mark the
	// corresponding per-message workflow rows as feed-delivered.
	OnDelivered func(messageIDs []string)

	// OnAnalysisFailed, if set, is invoked for a message that could not be
	// analyzed. Polling keeps such messages retryable when appropriate; this
	// callback is only for native workflow observability.
	OnAnalysisFailed func(msg *gmail.MessageDetail, err error)

	// ProjectsFn lists registered project wiki pages so analysis can cite
	// related projects by real path. Forwarded to PipelineDeps. nil = none.
	ProjectsFn func() []ProjectCandidate

	// SenderFactsFn resolves sender context in-process from the wiki graph.
	// Forwarded to PipelineDeps; nil = fall back to the graphify subprocess.
	SenderFactsFn func(ctx context.Context, displayName string) string

	// AttachmentExtractFn extracts readable text from an attachment's bytes
	// (documents + image OCR). Forwarded to PipelineDeps so the analysis can read
	// the business documents arriving as attachments. nil = attachment gate off.
	AttachmentExtractFn func(ctx context.Context, data []byte, filename, mimeType string) string

	// ThinkingKwarg is the analysis model's chat_template_kwargs thinking
	// off-switch (modelcaps.ThinkingToggleKwarg). Forwarded to PipelineDeps so the
	// "disabled" thinking config truly stops reasoning on dual-mode vLLM models
	// (else they exhaust the budget and return empty). "" for non-vLLM models.
	ThinkingKwarg string

	// ArchiveFolder is the local file-store base folder for archived attachments
	// (default "/Deneb-Archive/메일"). The local store is always available, so
	// substantive attachments are archived every cycle.
	ArchiveFolder string

	// ThreadSource supplies thread/sender context from the on-box archive for the
	// LMTP path (no Gmail client). Forwarded to PipelineDeps. nil = none.
	ThreadSource ThreadSource

	// AgentSynthesisFn runs the final synthesis as a chat agent turn with the full
	// toolset so the analysis prompt's tool steps execute instead of leaking as
	// <tool_call> text. Forwarded to PipelineDeps; nil = legacy tool-less synthesis.
	AgentSynthesisFn func(ctx context.Context, prompt string) (string, error)
}

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*Service)(nil)

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
	if cfg.ArchiveFolder == "" {
		cfg.ArchiveFolder = "/Deneb-Archive/메일"
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
		c, err := gmail.DefaultClient()
		if err != nil {
			return fmt.Errorf("gmail 클라이언트 초기화 실패: %w", err)
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
		return fmt.Errorf("Gmail 검색 실패 (%d회 시도): %w", searchMaxRetries+1, err) //nolint:staticcheck // ST1005 — Korean error message
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
		if err := s.state.Save(pollState); err != nil {
			// LastPollAt not persisted — next poll will re-query the same
			// window (wasted API call but no data loss). Log so repeated
			// failures surface instead of silently piling up.
			s.log.Warn("gmailpoll: state persist failed (no-messages branch)", "error", err)
		}
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
			s.markAnalysisFailed(&gmail.MessageDetail{
				ID:       summary.ID,
				ThreadID: summary.ThreadID,
				From:     summary.From,
				Subject:  summary.Subject,
				Date:     summary.Date,
			}, err)
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

	// Batch analysis: each email analyzed individually + one consolidated report.
	report, items, err := s.batchAnalyze(ctx, client, details)
	if err != nil {
		s.log.Warn("배치 분석 실패", "error", err, "count", len(details))
		// Total failure — no per-email analysis survived (typically the LLM was
		// unreachable). Bail BEFORE marking these emails seen so the next cycle
		// retries them. Otherwise they are dropped silently: the "(분석 실패)"
		// stub is contentless-suppressed (no card), yet the unconditional
		// markSeen below would still bury them — exactly the lost-mail pattern.
		// When only the consolidated report failed, items still holds the
		// per-email analyses, which are persisted below; only the all-failed
		// case bails here.
		if len(items) == 0 {
			s.markSkippedAnalyses(details, nil, err)
			return nil
		}
		report = "(분석 실패)"
	}
	s.markSkippedAnalyses(details, items, nil)

	// Auto-archive substantive attachments to Dropbox (best-effort). The note is
	// added to the notification only (kept out of the diary so durable wiki
	// knowledge stays clean) and only on a successful analysis — appending to the
	// "(분석 실패)" stub would add a newline that defeats the proactive
	// contentless-floor suppression and push a failed-analysis card.
	archived := s.archiveAttachments(ctx, client, details)

	// Persist each individual analysis (cache + per-message wiki page) so the
	// Mini App shows it instantly without a manual re-run. Runs even if the
	// consolidated report failed — the per-email results are independent.
	if s.cfg.OnAnalyzed != nil {
		for _, it := range items {
			if err := s.cfg.OnAnalyzed(it.Msg, it.Result); err != nil {
				s.log.Warn("mail analysis sink 실패", "id", it.Msg.ID, "error", err)
			}
		}
	}

	// Log analysis result to diary for wiki knowledge synthesis.
	s.logToDiary(len(details), report)

	// Send single consolidated report (archive note appended on success only).
	notifyMsg := report
	if err == nil && len(archived) > 0 {
		var b strings.Builder
		b.WriteString(notifyMsg)
		fmt.Fprintf(&b, "\n\n📎 첨부 %d개를 로컬 저장소에 보관했습니다:\n", len(archived))
		for _, p := range archived {
			fmt.Fprintf(&b, "- `%s`\n", p)
		}
		notifyMsg = b.String()
	}
	if s.sendNotification(ctx, notifyMsg) && s.cfg.OnDelivered != nil {
		ids := make([]string, 0, len(items))
		for _, it := range items {
			if it.Msg != nil && it.Msg.ID != "" {
				ids = append(ids, it.Msg.ID)
			}
		}
		if len(ids) > 0 {
			s.cfg.OnDelivered(ids)
		}
	}

	// Mark seen ONLY the mails whose individual analysis succeeded (those in
	// `items`). A mail that AnalyzeBatch skipped on a per-email error is absent
	// from `items`; leaving it unseen lets the next cycle retry it instead of
	// burying it. The poll path never sets a Gmail read flag, so dedup is purely
	// local — a wrongly-marked mail leaves the `newer_than:1h` window and is lost
	// forever. The all-failed case bails above; this is the partial-failure guard.
	// (Fetch-failed summaries were already marked seen in the GetMessage loop.)
	analyzed := make(map[string]bool, len(items))
	for _, it := range items {
		analyzed[it.Msg.ID] = true
	}
	for _, summary := range newMessages {
		if analyzed[summary.ID] {
			pollState.markSeen(summary.ID)
		}
	}
	pollState.LastPollAt = time.Now().UnixMilli()
	s.saveState(pollState)
	return nil
}

// pipelineDeps assembles the PipelineDeps for an analysis run from the service
// config (shared by the batch and single-email paths).
func (s *Service) pipelineDeps(gmailClient *gmail.Client) PipelineDeps {
	deps := PipelineDeps{
		GmailClient:         gmailClient,
		LLMClient:           s.llmClient,
		LocalClient:         s.cfg.LocalClient,
		LocalModel:          s.cfg.LocalModel,
		MainModel:           s.cfg.Model,
		AnalysisPrompt:      s.analysisPrompt(),
		Logger:              s.log,
		ProjectsFn:          s.cfg.ProjectsFn,
		SenderFactsFn:       s.cfg.SenderFactsFn,
		AttachmentExtractFn: s.cfg.AttachmentExtractFn,
		ThinkingKwarg:       s.cfg.ThinkingKwarg,
		ThreadSource:        s.cfg.ThreadSource,
		AgentSynthesisFn:    s.cfg.AgentSynthesisFn,
	}
	// Poll path: the attachment gate fetches bytes lazily from Gmail. The LMTP
	// path (IngestMessage) overrides this with a closure over the inline bytes,
	// since an LMTP message id isn't a Gmail id and the bytes are in-message.
	if gmailClient != nil {
		deps.AttachmentBytesFn = gmailClient.GetAttachment
	}
	return deps
}

func (s *Service) analysisPrompt() string {
	prompt := loadPrompt(s.cfg.PromptFile)
	if s.cfg.PromptOverride == nil {
		return prompt
	}
	if override, ok := s.cfg.PromptOverride(PromptIDAutoMailAnalysis); ok {
		if override = strings.TrimSpace(override); override != "" {
			return override
		}
	}
	return prompt
}

// batchAnalyze analyzes a batch: per-email individual analyses + one
// consolidated report. Returns the report plus the per-email items so the
// caller can persist each (cache + wiki page).
func (s *Service) batchAnalyze(ctx context.Context, gmailClient *gmail.Client, msgs []*gmail.MessageDetail) (string, []BatchItem, error) {
	s.log.Debug("batch analysis 실행", "count", len(msgs))
	return AnalyzeBatch(ctx, s.pipelineDeps(gmailClient), msgs)
}

// IngestMessage analyzes one externally-delivered email — pushed via LMTP
// (internal/platform/lmtpd), replacing the IMAP poll for that source — through the
// SAME pipeline and delivery path as a polled message: AnalyzeEmailPipeline →
// OnAnalyzed (Mini App cache + per-message wiki page) → Notifier (proactive 업무
// chat). The Gmail client is optional: an LMTP message has no Gmail thread id, so
// the thread-context stage simply no-ops (it is best-effort). Safe to call
// concurrently with the poll loop.
func (s *Service) IngestMessage(ctx context.Context, msg *gmail.MessageDetail, attBytes map[string][]byte) (AnalysisResult, error) {
	// Read s.gmailClient under the lock the poll loop writes it with, so a
	// concurrent lazy-init in Run() can't race this read.
	s.mu.Lock()
	gmailClient := s.gmailClient
	s.mu.Unlock()

	// 대용량첨부: resolve large-file download links in the HTML body into real
	// attachment bytes BEFORE the attachment gate/closure below, so they are OCR'd
	// into the analysis and archived exactly like inline attachments. The poll
	// path (Gmail API) never sets LargeAttachments, so this is a no-op there.
	if attBytes == nil {
		attBytes = map[string][]byte{}
	}
	s.fetchLargeAttachmentsInto(ctx, msg, attBytes)

	deps := s.pipelineDeps(gmailClient)
	// LMTP attachments arrive inline (no Gmail fetch): always serve the attachment
	// gate from these bytes so 견적서/계약서 PDFs are OCR'd into the analysis exactly
	// like the poll path. Keyed by AttachmentID, which lmtpd.parseMessage sets to
	// the same value it puts on msg.Attachments[*].AttachmentID. Installed
	// unconditionally (never inherit gmailClient.GetAttachment) because an LMTP
	// message id is not a Gmail id — fetching with it would hit the Gmail API in
	// vain. With no inline bytes the closure finds nothing → gate degrades to
	// body-only, exactly as intended.
	deps.AttachmentBytesFn = func(_ context.Context, _, attachmentID string) ([]byte, error) {
		if b, ok := attBytes[attachmentID]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("inline attachment %q not found", attachmentID)
	}
	res, err := AnalyzeEmailPipeline(ctx, deps, msg)
	if err != nil {
		s.markAnalysisFailed(msg, err)
		return AnalysisResult{}, err
	}
	if s.cfg.OnAnalyzed != nil {
		if err := s.cfg.OnAnalyzed(msg, res); err != nil {
			return AnalysisResult{}, err
		}
	}

	notify := strings.TrimSpace(res.Text)
	// Archive substantive attachments to the local file store from their inline bytes (the LMTP
	// path has them in-message — no Gmail fetch), and note it on the report, exactly
	// like the poll path's archiveAttachments does.
	if archived := s.archiveInlineAttachments(ctx, msg, attBytes); len(archived) > 0 && notify != "" {
		var b strings.Builder
		b.WriteString(notify)
		fmt.Fprintf(&b, "\n\n📎 첨부 %d개를 로컬 저장소에 보관했습니다:\n", len(archived))
		for _, p := range archived {
			fmt.Fprintf(&b, "- `%s`\n", p)
		}
		notify = b.String()
	}
	if notify != "" && s.sendNotification(ctx, notify) && s.cfg.OnDelivered != nil {
		s.cfg.OnDelivered([]string{msg.ID})
	}
	return res, nil
}

func (s *Service) saveState(state *PollState) {
	if err := s.state.Save(state); err != nil {
		s.log.Error("폴링 상태 저장 실패", "error", err)
	}
}

func (s *Service) markSkippedAnalyses(details []*gmail.MessageDetail, items []BatchItem, err error) {
	if s.cfg.OnAnalysisFailed == nil || len(details) == 0 {
		return
	}
	ok := make(map[string]bool, len(items))
	for _, it := range items {
		if it.Msg != nil && it.Msg.ID != "" {
			ok[it.Msg.ID] = true
		}
	}
	if err == nil {
		err = errors.New("individual email analysis failed")
	}
	for _, msg := range details {
		if msg == nil || msg.ID == "" || ok[msg.ID] {
			continue
		}
		s.markAnalysisFailed(msg, err)
	}
}

func (s *Service) markAnalysisFailed(msg *gmail.MessageDetail, err error) {
	if s.cfg.OnAnalysisFailed != nil {
		s.cfg.OnAnalysisFailed(msg, err)
	}
}

// logToDiary writes the email analysis report to the wiki diary.
// WikiDreamer will later synthesize these into structured wiki knowledge.
func (s *Service) logToDiary(count int, report string) {
	if s.cfg.DiaryDir == "" {
		return
	}
	entry := fmt.Sprintf("📬 메일 분석 (%d건)\n\n%s", count, report)
	if err := wiki.AppendDiaryTo(s.cfg.DiaryDir, entry); err != nil {
		s.log.Warn("메일 분석 diary 기록 실패", "error", err)
	}
}

func (s *Service) sendNotification(ctx context.Context, message string) bool {
	s.mu.Lock()
	notifier := s.notifier
	s.mu.Unlock()

	if notifier == nil {
		s.log.Warn("알림 전송 불가: notifier가 설정되지 않음")
		return false
	}

	notifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := notifier.Notify(notifyCtx, message); err != nil {
		s.log.Error("알림 전송 실패", "error", err)
		return false
	}
	return true
}
