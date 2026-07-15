// Provider config loading, model/workspace resolution, and Gmail polling.
package serverchat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
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

// MailIngestHealthSnapshot returns the latest LMTP/mail-ingest health for the
// composition root's /health contract, with live queue depth filled in, or nil
// when ingest is off. Keeps the mailIngestHealth type private to serverchat.
func (m *Manager) MailIngestHealthSnapshot() any {
	value := m.MailIngestHealth.Load()
	if value == nil {
		return nil
	}
	ingestHealth, ok := value.(mailIngestHealth)
	if !ok {
		return value
	}
	if m.MailIngestQueueStats != nil {
		ingestHealth.Queue = m.MailIngestQueueStats()
	}
	return ingestHealth
}

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

// RegisterMailIngestWorkflows wires the Gmail-poll and LMTP mail-ingest
// lifecycles from the current on-disk config snapshot. Called from the
// composition root's registerWorkflowSideEffects (server_workflow_side_effects.go).
func (m *Manager) RegisterMailIngestWorkflows() {
	cfgSnap, _ := config.LoadConfigFromDefaultPath()
	m.initGmailPoll(cfgSnap)
	m.initLMTPServer(cfgSnap)
}

func (m *Manager) initGmailPoll(snap *config.ConfigSnapshot) {
	if snap == nil {
		return
	}
	pollCfg := snap.Config.GmailPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}
	logger := m.Host.Logger()
	stateDir := config.ResolveStateDir()

	stage2, stage2Model, stage1, stage1Model := m.MailAnalysisModels()
	cfg := mailanalysis.Config{
		StateDir:      stateDir,
		LLMClient:     stage2,
		Model:         stage2Model,
		LocalClient:   stage1,
		LocalModel:    stage1Model,
		SenderFactsFn: m.Mail.WikiSenderFacts,
		CounterpartyProjectsFn: func(domain string) []string {
			return m.Mail.CPProjects.Lookup(m.Mail.WikiStore, domain)
		},
		AttachmentExtractFn: document.ExtractAttachmentText,
		PromptOverride:      m.Host.PromptOverride,
		ThinkingKwarg:       m.mailStage2ThinkingKwarg(),
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
	if m.Mail.WikiStore != nil && m.Mail.WikiStore.DiaryDir() != "" {
		cfg.DiaryDir = m.Mail.WikiStore.DiaryDir()
	}

	// Per-email persistence (Mini App cache + per-message wiki page with
	// related projects) and the project-candidate provider for
	// related-project selection. Both read the same wiki store the Mini App
	// uses, so a polled email shows up already-analyzed with its projects.
	cfg.OnAnalyzed = m.Mail.MakeMailAnalysisSink()
	cfg.OnAnalysisFailed = m.Mail.MakeMailAnalysisFailureSink()
	// Mirror polled Gmail messages into the local mailstore so mail_archive (and
	// the app mail get action) read them without an API round-trip. LMTP intake
	// fills the store on its own path; this covers the Gmail-fetched side.
	if m.Mail.MailStore != nil {
		cfg.MailStoreSink = m.Mail.MailStore.Put
	}
	cfg.ProjectsFn = m.Mail.ProjectCandidatesFn()
	// Run the synthesis as a chat agent turn so the analysis prompt's tools
	// (wiki, mail_archive) execute instead of leaking as <tool_call> text.
	cfg.AgentSynthesisFn = m.mailAnalysisAgentSynthesis
	if pollCfg.Silent == nil || !*pollCfg.Silent {
		cfg.OnDelivered = m.Mail.MakeMailFeedDeliverySink()
	}

	gmailPollSvc := mailanalysis.NewService(cfg, logger)

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
		gmailPollSvc.SetNotifier(noopGmailNotifier{})
		logger.Info("gmailpoll: silent mode — cache/wiki pre-warm only, chat delivery suppressed")
	} else {
		gmailPollSvc.SetNotifier(m.ProactiveRelay.MailNotifierForSession(proactive.NativeWorkSessionKey))
	}

	// Register as a periodic task within the autonomous service. serverauto's
	// AutonomousSubsystem owns the task's lifecycle (panic recovery,
	// scheduling) and the gmailPollSvc field itself — SetGmailPollSvc writes
	// through Host since chat init builds the service but auto owns it.
	if autonomousSvc := m.Host.AutonomousSvc(); autonomousSvc != nil {
		m.Host.SetGmailPollSvc(gmailPollSvc)
		autonomousSvc.RegisterTask(gmailPollSvc)
		logger.Info("gmailpoll registered with autonomous service",
			"interval", fmt.Sprintf("%dm", cfg.IntervalMin))
	} else {
		logger.Warn("gmailpoll: autonomous service not available, polling disabled")
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
func (m *Manager) archiveThreadSource() mailanalysis.ThreadSource {
	logger := m.Host.Logger()
	addr := archiveIMAPAddr()
	user := strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_IMAP_USER"))
	pass := os.Getenv("DENEB_ARCHIVE_IMAP_PASS")
	if user == "" || pass == "" {
		logger.Warn("LMTP 분석: 아카이브 스레드 컨텍스트 비활성화 (DENEB_ARCHIVE_IMAP_USER/PASS 미설정)", "addr", addr)
		return nil // stays off until creds are set (graceful: analysis runs without it)
	}
	src := mailarchive.New(mailarchive.Config{Addr: addr, User: user, Pass: pass, Mailboxes: archiveIMAPMailboxes()})
	if src == nil {
		return nil
	}
	logger.Info("LMTP 분석: 아카이브 스레드 컨텍스트 활성화", "addr", addr)
	return src
}

// mailAnalysisAgentSynthesis runs the mail-analysis synthesis as a chat agent
// turn with the full toolset, so the analysis prompt's tool steps (wiki search,
// mail_archive) actually execute instead of leaking as <tool_call> text into the
// feed. It uses an isolated "system:" session (kept out of
// recall + the session drawer) with ephemeral messages so the fixed-key
// transcript can't grow unbounded. m.ChatHandler is read at call time — long
// after startup — so wiring order does not matter; a nil handler returns an error
// and the pipeline falls back to its single-completion synthesis.
func (m *Manager) mailAnalysisAgentSynthesis(ctx context.Context, prompt string) (string, error) {
	if m.ChatHandler == nil {
		return "", fmt.Errorf("chat handler unavailable")
	}
	result, err := m.ChatHandler.SendSync(ctx, "system:mailpoll", prompt, "", &chat.SyncOptions{
		AutoDeliveredOutput: true,
		EphemeralUser:       true,
		EphemeralAssistant:  true,
	})
	if err != nil {
		return "", err
	}
	return result.BestText(), nil
}

// initLMTPServer starts the LMTP (RFC 2033) mail-ingest server when enabled. An
// on-box mail server (e.g. Maddy in Docker) PUSHES new mail over LMTP, which
// replaces IMAP polling for that source: each message is parsed and analyzed
// through the same pipeline as a polled one (Mini App cache + per-message wiki +
// proactive 업무 chat). A dedicated mailanalysis.Service — built with the same analysis
// deps but NOT registered as a periodic task and given a real chat notifier —
// carries the analysis + delivery wiring; the LMTP server just feeds it messages.
func (m *Manager) initLMTPServer(snap *config.ConfigSnapshot) {
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
	logger := m.Host.Logger()

	stateDir := config.ResolveStateDir()
	stage2, stage2Model, stage1, stage1Model := m.MailAnalysisModels()
	threadSource := m.archiveThreadSource()
	cfg := mailanalysis.Config{
		StateDir:      stateDir,
		LLMClient:     stage2,
		Model:         stage2Model,
		LocalClient:   stage1,
		LocalModel:    stage1Model,
		SenderFactsFn: m.Mail.WikiSenderFacts,
		CounterpartyProjectsFn: func(domain string) []string {
			return m.Mail.CPProjects.Lookup(m.Mail.WikiStore, domain)
		},
		AttachmentExtractFn: document.ExtractAttachmentText,
		PromptOverride:      m.Host.PromptOverride,
		OnAnalyzed:          m.Mail.MakeMailAnalysisSink(),
		OnDelivered:         m.Mail.MakeMailFeedDeliverySink(),
		OnAnalysisFailed:    m.Mail.MakeMailAnalysisFailureSink(),
		ProjectsFn:          m.Mail.ProjectCandidatesFn(),
		ThreadSource:        threadSource,
		ThinkingKwarg:       m.mailStage2ThinkingKwarg(),
	}
	if m.Mail.WikiStore != nil && m.Mail.WikiStore.DiaryDir() != "" {
		cfg.DiaryDir = m.Mail.WikiStore.DiaryDir()
	}
	// Run the synthesis as a chat agent turn so the analysis prompt's tools
	// (wiki, mail_archive) execute instead of leaking as <tool_call> text.
	cfg.AgentSynthesisFn = m.mailAnalysisAgentSynthesis
	svc := mailanalysis.NewService(cfg, logger)
	svc.SetNotifier(m.ProactiveRelay.MailNotifierForSession(proactive.NativeWorkSessionKey))

	queue, err := lmtpd.NewQueue(filepath.Join(stateDir, "lmtp-queue"))
	if err != nil {
		m.MailIngestHealth.Store(mailIngestHealth{
			Status:        "queue_error",
			LMTPEnabled:   false,
			ArchiveAddr:   archiveIMAPAddr(),
			ArchiveStatus: archiveStatus(threadSource != nil),
			LastError:     err.Error(),
		})
		logger.Error("LMTP queue 초기화 실패; 메일 수신 비활성화", "error", err)
		return
	}
	m.MailIngestHealth.Store(mailIngestHealth{
		Status:               "running",
		LMTPEnabled:          true,
		ArchiveThreadContext: threadSource != nil,
		ArchiveAddr:          archiveIMAPAddr(),
		ArchiveStatus:        archiveStatus(threadSource != nil),
		QueueDir:             queue.Dir(),
		Workers:              lmtpAnalysisWorkers,
	})
	m.MailIngestQueueStats = func() map[string]int {
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

	m.startLMTPAnalysisWorkers(queue, seen, svc)

	// ACK delivery only after the parsed message is durably queued. Analysis is an
	// LLM call, too slow for the LMTP transaction; the queue gives us post-ACK retry
	// semantics without holding decoded attachment bytes in unbounded goroutines.
	handler := func(_ context.Context, msg *lmtpd.Message) error {
		if seen.Seen(msg.DedupKey) {
			logger.Info("LMTP 중복 메일 건너뜀 (분석 완료)", "key", msg.DedupKey, "subject", msg.Detail.Subject)
			return nil // ACK — already analyzed on an earlier delivery
		}
		queued, err := queue.Enqueue(msg)
		if err != nil {
			return err // 451: MTA retries; no post-ACK loss
		}
		// Mirror the parsed message into the local store so the mail_archive tool
		// reads it without an IMAP round-trip. Best-effort: a store failure must
		// not fail delivery (the durable queue + IMAP archive stay the records of
		// record). Idempotent by Message-ID, so a re-delivery re-Puts harmlessly.
		if m.Mail.MailStore != nil && msg.Detail != nil {
			cm := mailarchive.ContextMessageFromDetail("INBOX", "", msg.Detail, 0)
			if _, perr := m.Mail.MailStore.Put(cm); perr != nil {
				logger.Warn("mailstore put 실패(분석은 계속)", "key", msg.DedupKey, "error", perr)
			}
		}
		if !queued {
			logger.Info("LMTP 중복 메일 건너뜀 (이미 큐에 있음)", "key", msg.DedupKey, "subject", msg.Detail.Subject)
		}
		return nil
	}

	srv := lmtpd.New(addr, handler, logger)
	m.Host.SafeGo("lmtp-server", func() {
		if err := srv.Serve(m.Host.ShutdownCtx()); err != nil {
			logger.Error("LMTP 서버 종료(오류)", "error", err)
		}
	})
	logger.Info("LMTP mail ingest 활성화 (IMAP 폴링 대체)", "addr", addr)
}

func (m *Manager) startLMTPAnalysisWorkers(queue *lmtpd.Queue, seen *lmtpd.SeenStore, svc *mailanalysis.Service) {
	logger := m.Host.Logger()
	for i := 0; i < lmtpAnalysisWorkers; i++ {
		workerID := i + 1
		m.Host.SafeGo("lmtp-analyze-worker", func() {
			ctx := m.Host.ShutdownCtx()
			for {
				if ctx.Err() != nil {
					return
				}
				item, err := queue.Claim()
				if err != nil {
					logger.Warn("LMTP queue claim 실패", "worker", workerID, "error", err)
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
				err = m.processLMTPQueueItem(ctx, svc, item)
				if err == nil {
					seen.Mark(item.Key)
					if cerr := queue.Complete(item); cerr != nil {
						logger.Warn("LMTP queue complete 실패", "worker", workerID, "key", item.Key, "error", cerr)
					}
					continue
				}
				if ctx.Err() != nil {
					return // leave processing file; queue recovery will retry on restart
				}
				finalAttempt := item.Attempts+1 >= lmtpQueueMaxAttempts
				if ferr := queue.Fail(item, err, lmtpQueueMaxAttempts); ferr != nil {
					logger.Error("LMTP queue fail 기록 실패", "worker", workerID, "key", item.Key, "error", ferr)
				} else if finalAttempt {
					logger.Error("LMTP 메일 분석 최종 실패; failed queue로 이동", "worker", workerID, "key", item.Key, "attempts", item.Attempts, "error", err)
				} else {
					logger.Warn("LMTP 메일 분석 실패; 큐 재시도 예정", "worker", workerID, "key", item.Key, "attempts", item.Attempts, "error", err)
				}
				if !sleepOrDone(ctx, lmtpQueueRetryDelay) {
					return
				}
			}
		})
	}
}

func (m *Manager) processLMTPQueueItem(ctx context.Context, svc *mailanalysis.Service, item *lmtpd.QueueItem) error {
	if item == nil {
		return nil
	}
	msg, err := lmtpd.ParseMessage(item.Raw, item.Key)
	if err != nil {
		return err
	}
	actx, cancel := context.WithTimeout(ctx, lmtpAnalysisItemTimeout)
	defer cancel()
	_, err = svc.IngestMessage(actx, msg.Detail, msg.AttachmentBytes)
	return err
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

func archiveIMAPMailboxes() []string {
	return mailarchive.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES"))
}

func archiveStatus(enabled bool) string {
	if enabled {
		return "ok"
	}
	return "disabled_missing_credentials"
}

// MailAnalysisModels returns the role-resolved clients used by mail analysis
// (Gmail poll + LMTP ingest) and the miniapp gmail.analyze factory
// (method_registry.go). Stage-2 synthesis is the user-facing report,
// quality-first → main role (the analysis role was retired 2026-07-07);
// stage-1 extractors are trivial classification → tiny role. Keeping the
// choice in ONE place prevents the two paths drifting apart: the #2045
// tiny/analysis upgrade reached only the poller, and the miniapp button
// stayed pinned to the fallback role until that provider's key died (401,
// 2026-06-10). Exported for the composition root's Host implementation.
func (m *Manager) MailAnalysisModels() (stage2 *llm.Client, stage2Model string, stage1 *llm.Client, stage1Model string) {
	if m.ModelRegistry == nil {
		return nil, "", nil, ""
	}
	return m.ModelRegistry.Client(modelrole.RoleMain),
		m.ModelRegistry.Model(modelrole.RoleMain),
		m.ModelRegistry.Client(modelrole.RoleTiny),
		m.ModelRegistry.Model(modelrole.RoleTiny)
}

// mailStage2ThinkingKwarg returns the chat_template_kwargs thinking off-switch for
// the mail-analysis stage-2 model (RoleMain), or "" when the model has none
// (non-vLLM, e.g. an Anthropic-wire cloud model). Threaded into the mailanalysis
// synthesis so its "disabled" thinking config truly stops reasoning on dual-mode
// vLLM models (dsv4) instead of exhausting the budget and returning empty — the
// analysis-path equivalent of what applyModelTuning does for the main chat.
func (m *Manager) mailStage2ThinkingKwarg() string {
	if m.ModelRegistry == nil {
		return ""
	}
	c := m.ModelRegistry.Config(modelrole.RoleMain)
	return m.ModelRegistry.CapabilityForModel(c.ProviderID, c.Model).ThinkingToggleKwarg
}
