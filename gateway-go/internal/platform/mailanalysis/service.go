package mailanalysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
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

	// SenderTrustFn runs on metadata only, before autonomous intake analyzes a
	// body. nil preserves the legacy trusted-by-default behavior.
	SenderTrustFn func(msg *gmail.MessageDetail) SenderTrustDecision

	// OnSenderReview persists a workflow review item when SenderTrustFn returns
	// SenderReview. Review is terminal for autonomous LLM intake but the operator
	// must still be able to read the body (manual analysis / mail_archive).
	OnSenderReview func(msg *gmail.MessageDetail, decision SenderTrustDecision) error

	// MailStoreSink, when set, mirrors mail into the local mailstore so it is
	// searchable via mail_archive without an API round-trip. Best-effort and
	// idempotent by Message-ID. Callers must not put empty-body stubs: Put is
	// idempotent and would permanently shadow a later full copy.
	MailStoreSink func(mailarchive.ContextMessage) (bool, error)

	// ProjectsFn lists registered project wiki pages so analysis can cite
	// related projects by real path. Forwarded to PipelineDeps. nil = none.
	ProjectsFn func() []ProjectCandidate

	// SenderFactsFn resolves sender context in-process from the wiki graph.
	// Forwarded to PipelineDeps; nil = fall back to the graphify subprocess.
	SenderFactsFn func(ctx context.Context, displayName string) string

	// TopicFactsFn recalls what the wiki holds about the mail's SUBJECT.
	// Forwarded to PipelineDeps; nil = no topic recall (the slot stays empty,
	// which is how it shipped before this was wired).
	TopicFactsFn func(ctx context.Context, subject, body string) string

	// CounterpartyProjectsFn returns linked project names for an active
	// counterparty domain (party-anchor enrichment). Forwarded to
	// PipelineDeps; nil = plain side labels.
	CounterpartyProjectsFn func(domain string) []string

	// AttachmentExtractFn extracts readable text from an attachment's bytes
	// (documents + image OCR). Forwarded to PipelineDeps so the analysis can read
	// the business documents arriving as attachments. nil = attachment gate off.
	AttachmentExtractFn func(ctx context.Context, data []byte, filename, mimeType string) string

	// ThinkingKwarg is the stage-2 main-role model's chat_template_kwargs thinking
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

	pollState := s.loadPollState()
	messages, err := s.searchMessages(ctx, client.Search, pollRetryDelay)
	if err != nil {
		return err
	}
	newMessages := s.selectNewMessages(messages, pollState)

	if len(newMessages) == 0 {
		s.finishNoMessagePoll(pollState)
		return nil
	}

	s.log.Info("새 메일 발견", "count", len(newMessages))
	trustedMessages, reviewed := s.partitionPollMessages(newMessages, pollState)
	if reviewed > 0 {
		// Persist successful review decisions before any trusted mail enters the
		// fallible LLM path. Otherwise an all-failed analysis would replay review
		// items on the next cycle.
		s.saveState(pollState)
	}
	if len(trustedMessages) == 0 {
		s.finishPoll(pollState)
		return nil
	}
	details := s.fetchMessageDetails(ctx, client.GetMessage, trustedMessages, pollState)

	if len(details) == 0 {
		s.finishPoll(pollState)
		return nil
	}

	s.mirrorPollMessages(details)

	// Batch analysis: each email analyzed individually + one consolidated report.
	report, items, analysisErr := s.batchAnalyze(ctx, client, details)
	if report, ok := s.resolvePollAnalysis(details, report, items, analysisErr); ok {
		s.finishAnalyzedPoll(ctx, client, pollState, trustedMessages, details, report, items, analysisErr)
	}
	return nil
}

type gmailSearchFunc func(context.Context, string, int) ([]gmail.MessageSummary, error)

type gmailGetMessageFunc func(context.Context, string) (*gmail.MessageDetail, error)

func (s *Service) loadPollState() *PollState {
	pollState, err := s.state.Load()
	if err != nil {
		s.log.Error("폴링 상태 로드 실패", "error", err)
		return &PollState{}
	}
	return pollState
}

func pollRetryDelay(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt+1)) * time.Second // 2s, 4s
}

func (s *Service) searchMessages(
	ctx context.Context,
	search gmailSearchFunc,
	retryDelay func(int) time.Duration,
) ([]gmail.MessageSummary, error) {
	var (
		messages []gmail.MessageSummary
		err      error
	)
	for attempt := 0; attempt <= searchMaxRetries; attempt++ {
		messages, err = search(ctx, s.cfg.Query, s.cfg.MaxPerCycle+10)
		if err == nil {
			return messages, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt == searchMaxRetries {
			break
		}
		delay := retryDelay(attempt)
		s.log.Warn("Gmail 검색 실패, 재시도", "error", err, "attempt", attempt+1, "delay", delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("Gmail 검색 실패 (%d회 시도): %w", searchMaxRetries+1, err) //nolint:staticcheck // ST1005 — Korean error message
}

func (s *Service) selectNewMessages(messages []gmail.MessageSummary, pollState *PollState) []gmail.MessageSummary {
	newMessages := make([]gmail.MessageSummary, 0, min(len(messages), s.cfg.MaxPerCycle))
	for _, msg := range messages {
		if pollState.hasSeen(msg.ID) {
			continue
		}
		newMessages = append(newMessages, msg)
		if len(newMessages) == s.cfg.MaxPerCycle {
			break
		}
	}
	return newMessages
}

func (s *Service) partitionPollMessages(messages []gmail.MessageSummary, pollState *PollState) ([]gmail.MessageSummary, int) {
	trusted := make([]gmail.MessageSummary, 0, len(messages))
	reviewed := 0
	for _, summary := range messages {
		metadata := messageDetailFromSummary(summary)
		decision := s.senderTrustDecision(metadata)
		if decision.Disposition != SenderReview {
			trusted = append(trusted, summary)
			continue
		}
		if err := s.recordSenderReview(metadata, decision); err != nil {
			s.log.Warn("발신자 검토 상태 저장 실패; 다음 폴링에서 재시도", "id", summary.ID, "error", err)
			continue
		}
		// Do not mailstore-put a metadata-only stub: Put is idempotent, so an
		// empty body would permanently hide the real message from get/archive.
		pollState.markSeen(summary.ID)
		reviewed++
		s.log.Info("미확인 발신자 메일 검토 대기", "id", summary.ID, "from", oneLine(summary.From))
	}
	return trusted, reviewed
}

func (s *Service) finishNoMessagePoll(pollState *PollState) {
	s.log.Debug("새 메일 없음")
	pollState.LastPollAt = time.Now().UnixMilli()
	if err := s.state.Save(pollState); err != nil {
		// LastPollAt not persisted — next poll will re-query the same window
		// (wasted API call but no data loss).
		s.log.Warn("gmailpoll: state persist failed (no-messages branch)", "error", err)
	}
}

func (s *Service) fetchMessageDetails(
	ctx context.Context,
	getMessage gmailGetMessageFunc,
	messages []gmail.MessageSummary,
	pollState *PollState,
) []*gmail.MessageDetail {
	details := make([]*gmail.MessageDetail, 0, len(messages))
	for _, summary := range messages {
		detail, err := getMessage(ctx, summary.ID)
		if err == nil {
			details = append(details, detail)
			continue
		}
		s.log.Warn("메일 본문 조회 실패", "id", summary.ID, "error", err)
		s.markAnalysisFailed(messageDetailFromSummary(summary), err)
		// A body-fetch failure is terminal for this poller: preserving the old
		// contract avoids retrying a permanently malformed Gmail item forever.
		pollState.markSeen(summary.ID)
		s.saveState(pollState)
	}
	return details
}

func messageDetailFromSummary(summary gmail.MessageSummary) *gmail.MessageDetail {
	return &gmail.MessageDetail{
		ID:       summary.ID,
		ThreadID: summary.ThreadID,
		From:     summary.From,
		Subject:  summary.Subject,
		Date:     summary.Date,
	}
}

func (s *Service) mirrorPollMessages(details []*gmail.MessageDetail) {
	for _, detail := range details {
		s.mirrorMessage("Gmail", detail)
	}
}

func (s *Service) mirrorMessage(mailbox string, msg *gmail.MessageDetail) {
	if s.cfg.MailStoreSink == nil || msg == nil {
		return
	}
	message := mailarchive.ContextMessageFromDetail(mailbox, msg.ID, msg, 0)
	if _, err := s.cfg.MailStoreSink(message); err != nil {
		s.log.Warn("mailstore put 실패", "id", msg.ID, "error", err)
	}
}

func (s *Service) resolvePollAnalysis(
	details []*gmail.MessageDetail,
	report string,
	items []BatchItem,
	err error,
) (string, bool) {
	if err != nil {
		s.log.Warn("배치 분석 실패", "error", err, "count", len(details))
		// No successful per-email item means nothing may be marked seen: the
		// next cycle must retry every fetched message.
		if len(items) == 0 {
			s.markSkippedAnalyses(details, nil, err)
			return "", false
		}
		report = "(분석 실패)"
	}
	s.markSkippedAnalyses(details, items, nil)
	return report, true
}

func (s *Service) finishAnalyzedPoll(
	ctx context.Context,
	client *gmail.Client,
	pollState *PollState,
	newMessages []gmail.MessageSummary,
	details []*gmail.MessageDetail,
	report string,
	items []BatchItem,
	analysisErr error,
) {
	// Auto-archive substantive attachments to the local file store (best-effort). The note is
	// added to the notification only (kept out of the diary so durable wiki
	// knowledge stays clean) and only on a successful analysis — appending to the
	// "(분석 실패)" stub would add a newline that defeats the proactive
	// contentless-floor suppression and push a failed-analysis card.
	archived := s.archiveAttachments(ctx, client, details)

	s.persistPollAnalyses(items)
	s.logToDiary(len(details), report)
	s.deliverPollReport(ctx, report, items, archived, analysisErr == nil)
	markAnalyzedMessagesSeen(pollState, newMessages, items)
	s.finishPoll(pollState)
}

func (s *Service) persistPollAnalyses(items []BatchItem) {
	if s.cfg.OnAnalyzed == nil {
		return
	}

	// Persist each individual analysis (cache + per-message wiki page) so the
	// Mini App shows it instantly without a manual re-run. Runs even if the
	// consolidated report failed — the per-email results are independent.
	for _, item := range items {
		if err := s.cfg.OnAnalyzed(item.Msg, item.Result); err != nil {
			s.log.Warn("mail analysis sink 실패", "id", item.Msg.ID, "error", err)
		}
	}
}

func (s *Service) deliverPollReport(
	ctx context.Context,
	report string,
	items []BatchItem,
	archived []string,
	includeArchive bool,
) {
	notifyMsg := report
	if includeArchive && len(archived) > 0 {
		var b strings.Builder
		b.WriteString(notifyMsg)
		fmt.Fprintf(&b, "\n\n📎 첨부 %d개를 로컬 저장소에 보관했습니다:\n", len(archived))
		for _, path := range archived {
			fmt.Fprintf(&b, "- `%s`\n", path)
		}
		notifyMsg = b.String()
	}
	if !s.sendNotification(ctx, notifyMsg) || s.cfg.OnDelivered == nil {
		return
	}
	if ids := analyzedMessageIDs(items); len(ids) > 0 {
		s.cfg.OnDelivered(ids)
	}
}

func analyzedMessageIDs(items []BatchItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if item.Msg != nil && item.Msg.ID != "" {
			ids = append(ids, item.Msg.ID)
		}
	}
	return ids
}

func markAnalyzedMessagesSeen(pollState *PollState, messages []gmail.MessageSummary, items []BatchItem) {
	// Mark seen ONLY the mails whose individual analysis succeeded (those in
	// `items`). A mail that AnalyzeBatch skipped on a per-email error is absent
	// from `items`; leaving it unseen lets the next cycle retry it instead of
	// burying it. The poll path never sets a Gmail read flag, so dedup is purely
	// local — a wrongly-marked mail leaves the `newer_than:1h` window and is lost
	// forever. The all-failed case bails above; this is the partial-failure guard.
	// (Fetch-failed summaries were already marked seen in the GetMessage loop.)
	analyzed := make(map[string]struct{}, len(items))
	for _, id := range analyzedMessageIDs(items) {
		analyzed[id] = struct{}{}
	}
	for _, summary := range messages {
		if _, ok := analyzed[summary.ID]; ok {
			pollState.markSeen(summary.ID)
		}
	}
}

func (s *Service) finishPoll(pollState *PollState) {
	pollState.LastPollAt = time.Now().UnixMilli()
	s.saveState(pollState)
}

// pipelineDeps assembles the PipelineDeps for an analysis run from the service
// config (shared by the batch and single-email paths).
func (s *Service) pipelineDeps(gmailClient *gmail.Client) PipelineDeps {
	deps := PipelineDeps{
		GmailClient:            gmailClient,
		LLMClient:              s.llmClient,
		LocalClient:            s.cfg.LocalClient,
		LocalModel:             s.cfg.LocalModel,
		MainModel:              s.cfg.Model,
		AnalysisPrompt:         s.analysisPrompt(),
		Logger:                 s.log,
		ProjectsFn:             s.cfg.ProjectsFn,
		SenderFactsFn:          s.cfg.SenderFactsFn,
		TopicFactsFn:           s.cfg.TopicFactsFn,
		CounterpartyProjectsFn: s.cfg.CounterpartyProjectsFn,
		AttachmentExtractFn:    s.cfg.AttachmentExtractFn,
		ThinkingKwarg:          s.cfg.ThinkingKwarg,
		ThreadSource:           s.cfg.ThreadSource,
		AgentSynthesisFn:       s.cfg.AgentSynthesisFn,
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
	if msg == nil {
		return AnalysisResult{}, fmt.Errorf("email message is required")
	}
	decision := s.senderTrustDecision(msg)
	if decision.Disposition == SenderReview {
		if err := s.recordSenderReview(msg, decision); err != nil {
			return AnalysisResult{}, err
		}
		// Keep the full body readable for operator review. Trust gates autonomous
		// LLM intake only — stripping the body made mail_archive/get return empty
		// forever (mailstore Put is idempotent).
		s.mirrorMessage("INBOX", msg)
		s.log.Info("미확인 발신자 메일 검토 대기", "id", msg.ID, "from", oneLine(msg.From))
		return AnalysisResult{}, nil
	}
	// Read s.gmailClient under the lock the poll loop writes it with, so a
	// concurrent lazy-init in Run() can't race this read.
	s.mu.Lock()
	gmailClient := s.gmailClient
	s.mu.Unlock()

	s.mirrorMessage("INBOX", msg)

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
