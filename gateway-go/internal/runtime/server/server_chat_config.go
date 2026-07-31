// Provider config loading, model/workspace resolution, and Gmail polling.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

// noopGmailNotifier is a mailanalysis.Notifier that drops messages. Used in
// silent mode so the poller fills the Mini App cache + wiki (via OnAnalyzed)
// without delivering a duplicate proactive chat message. A real no-op (rather
// than a nil notifier) keeps sendNotification from logging a per-cycle warn.
type noopGmailNotifier struct{}

// Notify accepts a notification without external delivery.
func (noopGmailNotifier) Notify(context.Context, string) error { return nil }

type mailIngestHealth struct {
	Status               string         `json:"status"`
	LMTPEnabled          bool           `json:"lmtp_enabled"`
	ArchiveThreadContext bool           `json:"archive_thread_context"`
	ArchiveAddr          string         `json:"archive_addr,omitempty"`
	ArchiveStatus        string         `json:"archive_status"`
	LastError            string         `json:"last_error,omitempty"`
	QueueDir             string         `json:"queue_dir,omitempty"`
	Workers              int            `json:"workers"`
	Queue                map[string]int `json:"queue,omitempty"`
}

const (
	lmtpAnalysisWorkers     = 4
	lmtpQueueMaxAttempts    = 6
	lmtpQueueIdleDelay      = 2 * time.Second
	lmtpQueueRetryDelay     = 10 * time.Second
	lmtpAnalysisItemTimeout = 5 * time.Minute
)

func (s *Server) initGmailPoll(snap *config.ConfigSnapshot) {
	if snap == nil {
		return
	}
	pollCfg := snap.Config.GmailPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}

	stateDir := config.ResolveStateDir()

	stage2, stage2Model, stage1, stage1Model := s.mailAnalysisModels()
	cfg := mailanalysis.Config{
		StateDir:      stateDir,
		LLMClient:     stage2,
		Model:         stage2Model,
		LocalClient:   stage1,
		LocalModel:    stage1Model,
		SenderFactsFn: s.wikiSenderFacts,
		CounterpartyProjectsFn: func(domain string) []string {
			return s.cpProjects.Lookup(s.wikiStore, domain)
		},
		AttachmentExtractFn: toolbind.ExtractAttachmentText,
		PromptOverride:      s.promptOverride,
		ThinkingKwarg:       s.mailStage2ThinkingKwarg(),
		SenderTrustFn:       s.mailSenderTrustDecision,
		OnSenderReview:      s.makeMailSenderReviewSink(),
	}

	if pollCfg.IntervalMin != nil {
		cfg.IntervalMin = *pollCfg.IntervalMin
	}
	if pollCfg.Query != "" {
		cfg.Query = pollCfg.Query
	}
	if pollCfg.MaxPerCycle != nil {
		cfg.MaxPerCycle = *pollCfg.MaxPerCycle
	}
	if pollCfg.Model != "" {
		cfg.Model = pollCfg.Model // explicit override from config
	}
	if pollCfg.PromptFile != "" {
		cfg.PromptFile = pollCfg.PromptFile
	}

	// Wire diary dir for wiki knowledge synthesis.
	if s.wikiStore != nil && s.wikiStore.DiaryDir() != "" {
		cfg.DiaryDir = s.wikiStore.DiaryDir()
	}

	// Per-email persistence (Mini App cache + per-message wiki page with
	// related projects) and the project-candidate provider for
	// related-project selection. Both read the same wiki store the Mini App
	// uses, so a polled email shows up already-analyzed with its projects.
	cfg.OnAnalyzed = s.makeMailAnalysisSink()
	cfg.OnAnalysisFailed = s.makeMailAnalysisFailureSink()
	// Mirror polled Gmail messages into the local mailstore so mail_archive (and
	// the app mail get action) read them without an API round-trip. LMTP intake
	// fills the store on its own path; this covers the Gmail-fetched side.
	if s.mailStore != nil {
		cfg.MailStoreSink = s.mailStore.Put
	}
	cfg.ProjectsFn = s.projectCandidatesFn()
	// Run the synthesis as a chat agent turn so the analysis prompt's tools
	// (wiki, mail_archive) execute instead of leaking as <tool_call> text.
	cfg.AgentSynthesisFn = s.mailAnalysisAgentSynthesis
	if pollCfg.Silent == nil || !*pollCfg.Silent {
		cfg.OnDelivered = s.makeMailFeedDeliverySink()
	}

	s.gmailPollSvc = mailanalysis.NewService(cfg, s.logger)

	// Wire proactive relay as the gmail-poll notifier so email summaries
	// are delivered verbatim AND mirrored into the main session
	// transcript — follow-ups ("방금 그 메일 답장 초안 써줘") answer in a
	// session that knows what arrived.
	//
	// All proactive output goes to the native client's 업무 chat (client:main).
	// The deliverTo config field was Telegram-target-specific and is no longer
	// consulted after Telegram bot retirement.
	//
	// Silent mode: the kakao-watch email-single-analysis cron already delivers
	// the prose analysis to chat, so a duplicate mail-poll message is noise. A
	// no-op notifier suppresses delivery while OnAnalyzed still pre-warms the
	// Mini App analysis cache + per-message wiki page.
	if pollCfg.Silent != nil && *pollCfg.Silent {
		s.gmailPollSvc.SetNotifier(noopGmailNotifier{})
		s.logger.Info("gmailpoll: silent mode — cache/wiki pre-warm only, chat delivery suppressed")
	} else {
		s.gmailPollSvc.SetNotifier(s.proactiveRelay.MailNotifierForSession(proactive.NativeWorkSessionKey))
	}

	// Register as a periodic task within the autonomous service.
	// The autonomous service handles lifecycle, panic recovery, and scheduling.
	if s.autonomousSvc != nil {
		s.autonomousSvc.RegisterTask(s.gmailPollSvc)
		s.logger.Info("gmailpoll registered with autonomous service",
			"interval", fmt.Sprintf("%dm", cfg.IntervalMin))
	} else {
		s.logger.Warn("gmailpoll: autonomous service not available, polling disabled")
	}
}

// archiveThreadSource builds the on-box archive IMAP thread source for the LMTP
// analysis path — it reconstructs a message's thread + sender history from the
// local mail archive instead of Gmail. Returns nil (thread context disabled)
// until archive IMAP credentials are configured. Env:
//
//	DENEB_ARCHIVE_IMAP_ADDR  (default 127.0.0.1:1143)
//	DENEB_ARCHIVE_IMAP_USER  / DENEB_ARCHIVE_IMAP_PASS  (required to enable)
//	DENEB_ARCHIVE_IMAP_MAILBOXES (optional, e.g. INBOX,Archive)
func (s *Server) archiveThreadSource() mailanalysis.ThreadSource {
	addr := archiveIMAPAddr()
	user := strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_USER"))
	pass := os.Getenv("DENEB_ARCHIVE_IMAP_PASS")
	if user == "" || pass == "" {
		s.logger.Warn("LMTP 분석: 아카이브 스레드 컨텍스트 비활성화 (DENEB_ARCHIVE_IMAP_USER/PASS 미설정)", "addr", addr)
		return nil // stays off until creds are set (graceful: analysis runs without it)
	}
	src := mailarchive.New(mailarchive.Config{Addr: addr, User: user, Pass: pass, Mailboxes: archiveIMAPMailboxes()})
	if src == nil {
		return nil
	}
	s.logger.Info("LMTP 분석: 아카이브 스레드 컨텍스트 활성화", "addr", addr)
	return src
}

// mailAnalysisAgentSynthesis runs the mail-analysis synthesis as a chat agent
// turn with the full toolset, so the analysis prompt's tool steps (wiki search,
// mail_archive) actually execute instead of leaking as <tool_call> text into the
// feed. It uses an isolated "system:" session (kept out of
// recall + the session drawer) with ephemeral messages so the fixed-key
// transcript can't grow unbounded. s.chatHandler is read at call time — long
// after startup — so wiring order does not matter; a nil handler returns an error
// and the pipeline falls back to its single-completion synthesis.
func (s *Server) mailAnalysisAgentSynthesis(ctx context.Context, prompt string) (string, error) {
	if s.chatHandler == nil {
		return "", fmt.Errorf("chat handler unavailable")
	}
	result, err := s.chatHandler.SendSync(ctx, "system:mailpoll", prompt, "", &chat.SyncOptions{
		AutoDeliveredOutput: true,
		EphemeralUser:       true,
		EphemeralAssistant:  true,
		BeforeToolCall:      mailAnalysisAgentToolGate,
	})
	if err != nil {
		return "", err
	}
	return result.BestText(), nil
}

func mailAnalysisAgentToolGate(name, _ string, input []byte) (bool, string) {
	switch name {
	case "wiki":
		action := toolInputField(input, "action")
		if isMailAnalysisBlockedWikiAction(action) {
			return true, mailAnalysisReadOnlyBlockReason
		}
	case "knowledge":
		if strings.EqualFold(toolInputField(input, "op"), "record") {
			return true, mailAnalysisReadOnlyBlockReason
		}
	}
	return false, ""
}

func toolInputField(input []byte, field string) string {
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	value, _ := payload[field].(string)
	return strings.TrimSpace(value)
}

func isMailAnalysisBlockedWikiAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "write", "write-site", "seed-sites", "log", "close", "reopen", "ingest":
		return true
	default:
		return false
	}
}

const mailAnalysisReadOnlyBlockReason = "mail analysis synthesis is read-only; the mail analysis pipeline persists wiki/knowledge updates separately. Do not retry this write; return the final analysis."

// initLMTPServer starts the LMTP (RFC 2033) mail-ingest server when enabled. An
// on-box mail server (e.g. Maddy in Docker) PUSHES new mail over LMTP, which
// replaces IMAP polling for that source: each message is parsed and analyzed
// through the same pipeline as a polled one (Mini App cache + per-message wiki +
// proactive 업무 chat). A dedicated mailanalysis.Service — built with the same analysis
// deps but NOT registered as a periodic task and given a real chat notifier —
// carries the analysis + delivery wiring; the LMTP server just feeds it messages.
func (s *Server) initLMTPServer(snap *config.ConfigSnapshot) {
	if snap == nil {
		return
	}
	lcfg := snap.Config.MailLMTP
	if lcfg == nil || lcfg.Enabled == nil || !*lcfg.Enabled {
		return
	}
	addr := lcfg.ListenAddr
	if addr == "" {
		addr = "127.0.0.1:10024"
	}

	stateDir := config.ResolveStateDir()
	stage2, stage2Model, stage1, stage1Model := s.mailAnalysisModels()
	threadSource := s.archiveThreadSource()
	cfg := mailanalysis.Config{
		StateDir:      stateDir,
		LLMClient:     stage2,
		Model:         stage2Model,
		LocalClient:   stage1,
		LocalModel:    stage1Model,
		SenderFactsFn: s.wikiSenderFacts,
		CounterpartyProjectsFn: func(domain string) []string {
			return s.cpProjects.Lookup(s.wikiStore, domain)
		},
		AttachmentExtractFn: toolbind.ExtractAttachmentText,
		PromptOverride:      s.promptOverride,
		OnAnalyzed:          s.makeMailAnalysisSink(),
		OnDelivered:         s.makeMailFeedDeliverySink(),
		OnAnalysisFailed:    s.makeMailAnalysisFailureSink(),
		SenderTrustFn:       s.mailSenderTrustDecision,
		OnSenderReview:      s.makeMailSenderReviewSink(),
		ProjectsFn:          s.projectCandidatesFn(),
		ThreadSource:        threadSource,
		ThinkingKwarg:       s.mailStage2ThinkingKwarg(),
	}
	if s.mailStore != nil {
		cfg.MailStoreSink = s.mailStore.Put
	}
	if s.wikiStore != nil && s.wikiStore.DiaryDir() != "" {
		cfg.DiaryDir = s.wikiStore.DiaryDir()
	}
	// Run the synthesis as a chat agent turn so the analysis prompt's tools
	// (wiki, mail_archive) execute instead of leaking as <tool_call> text.
	cfg.AgentSynthesisFn = s.mailAnalysisAgentSynthesis
	svc := mailanalysis.NewService(cfg, s.logger)
	svc.SetNotifier(s.proactiveRelay.MailNotifierForSession(proactive.NativeWorkSessionKey))

	queue, err := lmtpd.NewQueue(filepath.Join(stateDir, "lmtp-queue"))
	if err != nil {
		s.mailIngestHealth.Store(mailIngestHealth{
			Status:        "queue_error",
			LMTPEnabled:   false,
			ArchiveAddr:   archiveIMAPAddr(),
			ArchiveStatus: archiveStatus(threadSource != nil),
			LastError:     err.Error(),
		})
		s.logger.Error("LMTP queue 초기화 실패; 메일 수신 비활성화", "error", err)
		return
	}
	s.mailIngestHealth.Store(mailIngestHealth{
		Status:               "running",
		LMTPEnabled:          true,
		ArchiveThreadContext: threadSource != nil,
		ArchiveAddr:          archiveIMAPAddr(),
		ArchiveStatus:        archiveStatus(threadSource != nil),
		QueueDir:             queue.Dir(),
		Workers:              lmtpAnalysisWorkers,
	})
	s.mailIngestQueueStats = func() map[string]int {
		st := queue.Stats()
		return map[string]int{
			"pending":    st.Pending,
			"processing": st.Processing,
			"failed":     st.Failed,
		}
	}

	// Dedup by Message-ID across restarts so an MTA re-delivery isn't analyzed
	// (or wiki-paged / chat-reported) twice. Marked only after queued analysis
	// succeeds; queued/processing duplicates are suppressed by the durable queue.
	seen := lmtpd.NewSeenStore(filepath.Join(stateDir, "lmtp-seen.json"), 2000)

	s.startLMTPAnalysisWorkers(queue, seen, svc)

	// ACK delivery only after the parsed message is durably queued. Analysis is an
	// LLM call, too slow for the LMTP transaction; the queue gives us post-ACK retry
	// semantics without holding decoded attachment bytes in unbounded goroutines.
	handler := func(_ context.Context, m *lmtpd.Message) error {
		if seen.Seen(m.DedupKey) {
			s.logger.Info("LMTP 중복 메일 건너뜀 (분석 완료)", "key", m.DedupKey, "subject", m.Detail.Subject)
			return nil // ACK — already analyzed on an earlier delivery
		}
		queued, err := queue.Enqueue(m)
		if err != nil {
			return err // 451: MTA retries; no post-ACK loss
		}
		if !queued {
			s.logger.Info("LMTP 중복 메일 건너뜀 (이미 큐에 있음)", "key", m.DedupKey, "subject", m.Detail.Subject)
		}
		return nil
	}

	srv := lmtpd.New(addr, handler, s.logger)
	s.safeGo("lmtp-server", func() {
		if err := srv.Serve(s.ShutdownCtx()); err != nil {
			s.logger.Error("LMTP 서버 종료(오류)", "error", err)
		}
	})
	s.logger.Info("LMTP mail ingest 활성화 (IMAP 폴링 대체)", "addr", addr)
}

func (s *Server) startLMTPAnalysisWorkers(queue *lmtpd.Queue, seen *lmtpd.SeenStore, svc *mailanalysis.Service) {
	for i := 0; i < lmtpAnalysisWorkers; i++ {
		workerID := i + 1
		s.safeGo("lmtp-analyze-worker", func() {
			ctx := s.ShutdownCtx()
			for {
				if ctx.Err() != nil {
					return
				}
				item, err := queue.Claim()
				if err != nil {
					s.logger.Warn("LMTP queue claim 실패", "worker", workerID, "error", err)
					if !sleepOrDone(ctx, lmtpQueueIdleDelay) {
						return
					}
					continue
				}
				if item == nil {
					if !sleepOrDone(ctx, lmtpQueueIdleDelay) {
						return
					}
					continue
				}
				err = s.processLMTPQueueItem(ctx, svc, item)
				if err == nil {
					seen.Mark(item.Key)
					if cerr := queue.Complete(item); cerr != nil {
						s.logger.Warn("LMTP queue complete 실패", "worker", workerID, "key", item.Key, "error", cerr)
					}
					continue
				}
				if ctx.Err() != nil {
					return // leave processing file; queue recovery will retry on restart
				}
				finalAttempt := item.Attempts+1 >= lmtpQueueMaxAttempts
				if ferr := queue.Fail(item, err, lmtpQueueMaxAttempts); ferr != nil {
					s.logger.Error("LMTP queue fail 기록 실패", "worker", workerID, "key", item.Key, "error", ferr)
				} else if finalAttempt {
					s.logger.Error("LMTP 메일 분석 최종 실패; failed queue로 이동", "worker", workerID, "key", item.Key, "attempts", item.Attempts, "error", err)
				} else {
					s.logger.Warn("LMTP 메일 분석 실패; 큐 재시도 예정", "worker", workerID, "key", item.Key, "attempts", item.Attempts, "error", err)
				}
				if !sleepOrDone(ctx, lmtpQueueRetryDelay) {
					return
				}
			}
		})
	}
}

func (s *Server) processLMTPQueueItem(ctx context.Context, svc *mailanalysis.Service, item *lmtpd.QueueItem) error {
	if item == nil {
		return nil
	}
	msg, err := lmtpd.ParseMessage(item.Raw, item.Key)
	if err != nil {
		return err
	}
	actx, cancel := context.WithTimeout(ctx, lmtpAnalysisItemTimeout)
	defer cancel()
	if _, err = svc.IngestMessage(actx, msg.Detail, msg.AttachmentBytes); err != nil {
		return err
	}
	// New mail is now in the store — mirror it to clients (mail.changed) so
	// their lists force-warm instead of waiting out the activation TTL. Warn on
	// failure: the TTL revalidation backstops, no user-observable loss.
	if s.nativeSyncStore != nil {
		if _, appendErr := s.nativeSyncStore.Append(nativesync.MailChanged(item.Key)); appendErr != nil {
			s.logger.Warn("native sync: mail arrival append failed", "key", item.Key, "error", appendErr)
		}
	}
	return nil
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func archiveIMAPAddr() string {
	if addr := strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:1143"
}

func archiveStatus(enabled bool) string {
	if enabled {
		return "ok"
	}
	return "disabled_missing_credentials"
}

// registerNativeSystemMethods registers native Go system RPC methods:
// usage, logs, doctor, maintenance, update.

// and the interactive miniapp gmail.analyze factory (method_registry.go).
// Stage-2 synthesis is the user-facing report, quality-first → main role
// (the analysis role was retired 2026-07-07); stage-1 extractors are trivial
// classification → tiny role. Keeping the choice in ONE place prevents the two
// paths drifting apart: the #2045 tiny/analysis upgrade reached only the poller,
// and the miniapp button stayed pinned to the fallback role until that
// provider's key died (401, 2026-06-10).
func (s *Server) mailAnalysisModels() (stage2 *llm.Client, stage2Model string, stage1 *llm.Client, stage1Model string) {
	if s.modelRegistry == nil {
		return nil, "", nil, ""
	}
	return s.modelRegistry.Client(modelrole.RoleMain),
		s.modelRegistry.Model(modelrole.RoleMain),
		s.modelRegistry.Client(modelrole.RoleTiny),
		s.modelRegistry.Model(modelrole.RoleTiny)
}

// mailStage2ThinkingKwarg returns the chat_template_kwargs thinking off-switch for
// the mail-analysis stage-2 model (RoleMain), or "" when the model has none
// (non-vLLM, e.g. an Anthropic-wire cloud model). Threaded into the mailanalysis
// synthesis so its "disabled" thinking config truly stops reasoning on dual-mode
// vLLM models (dsv4) instead of exhausting the budget and returning empty — the
// analysis-path equivalent of what applyModelTuning does for the main chat.
func (s *Server) mailStage2ThinkingKwarg() string {
	if s.modelRegistry == nil {
		return ""
	}
	c := s.modelRegistry.Config(modelrole.RoleMain)
	return s.modelRegistry.CapabilityForModel(c.ProviderID, c.Model).ThinkingToggleKwarg
}
