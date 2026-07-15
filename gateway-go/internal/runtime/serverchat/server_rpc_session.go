package serverchat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// RegisterSessionRPCMethods registers session state, repair, daemon status, and
// the full chat handler pipeline (init + all chat/session-exec RPC registrations).
// Called from the composition root after event infrastructure and the mail
// manager's InitMemory have run.
func (m *Manager) RegisterSessionRPCMethods() {
	m.registerSessionDomainMethods()
	transcriptStore, transcriptDir, polarisStoreForSweep := m.openSessionTranscriptStore()
	m.startSessionMemorySweep(transcriptDir, polarisStoreForSweep)
	agentLogWriter := m.newSessionAgentLogWriter()
	m.Host.SetAgentLogWriter(agentLogWriter)
	m.wireSessionInsights(agentLogWriter)

	chatCfg := m.buildSessionChatConfig(transcriptStore, agentLogWriter)

	logger := m.Host.Logger()
	m.ChatHandler = chat.NewHandler(
		m.Sessions,
		func(event string, payload any) (int, []error) {
			return m.broadcastSessionEvent(event, eventPayloadFromAny(payload))
		},
		logger,
		chatCfg,
	)

	m.configureSessionChatHandler()

	// Wire SendFn after handler creation to avoid circular deps.
	sendFn := m.sessionSendFunc()
	m.ToolDeps.Sessions.SendFn = sendFn
	m.ToolDeps.Chrono.SendFn = sendFn

	m.initSessionProactiveRelay(transcriptStore, agentLogWriter)
	m.wireSessionCronRelay(transcriptStore)
	m.wireSessionACP(transcriptStore)

	// Chat, BTW, miniapp-chat bridge, Exec, Wiki, Genesis, and GmailAnalyze are
	// registered in registerLateMethods() after this function returns; Aurora
	// (dreaming) is wired later in registerWorkflowSideEffects().
}

func (m *Manager) registerSessionDomainMethods() {
	m.Host.Dispatcher().RegisterDomain(handlersession.Methods(handlersession.Deps{
		Sessions:    m.Sessions,
		GatewaySubs: m.Host.GatewaySubs(),
	}))
}

func targetedToolRunID(event string, payload events.EventPayload) string {
	if event != "session.tool" {
		return ""
	}
	var values map[string]any
	if json.Unmarshal(payload.Bytes(), &values) != nil {
		return ""
	}
	runID, _ := values["runId"].(string)
	return runID
}

// eventPayloadFromAny converts pipeline/domain broadcast payloads into the
// typed events.EventPayload used by the runtime broadcaster. Keeps higher
// layers from importing runtime/events (Health Bench upward-import rule).
func eventPayloadFromAny(payload any) events.EventPayload {
	switch v := payload.(type) {
	case nil:
		return events.EventPayload{}
	case events.EventPayload:
		return v
	case json.RawMessage:
		return events.PayloadFromRaw(v)
	case []byte:
		return events.PayloadFromRaw(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return events.EventPayload{}
		}
		return events.PayloadFromRaw(raw)
	}
}

// broadcastSessionEvent keeps tool progress scoped to the connection that
// started the run; every other event retains the normal fan-out behavior.
func (m *Manager) broadcastSessionEvent(event string, payload events.EventPayload) (int, []error) {
	runID := targetedToolRunID(event, payload)
	broadcaster := m.Host.Broadcaster()
	if runID != "" {
		if connID := broadcaster.ToolEventRecipient(runID); connID != "" {
			return broadcaster.BroadcastToConnIDs(event, payload, map[string]struct{}{connID: {}})
		}
	}
	return broadcaster.Broadcast(event, payload)
}

func (m *Manager) openSessionTranscriptStore() (chat.TranscriptStore, string, *polaris.Store) {
	transcriptDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		transcriptDir = home + "/.deneb/transcripts"
	}
	if transcriptDir == "" {
		return nil, "", nil
	}

	cached := chat.NewCachedTranscriptStore(chat.NewFileTranscriptStore(transcriptDir), 0)
	transcriptStore, polarisStore := m.openPolarisTranscriptBridge(cached)
	if transcriptStore == nil {
		// Assembly will return an error, but the gateway can still serve other functions.
		transcriptStore = cached
	}
	return transcriptStore, transcriptDir, polarisStore
}

func (m *Manager) openPolarisTranscriptBridge(cached chat.TranscriptStore) (chat.TranscriptStore, *polaris.Store) {
	logger := m.Host.Logger()
	home, err := os.UserHomeDir()
	if err != nil {
		logger.Error("polaris: cannot determine home directory", "error", err)
		return nil, nil
	}
	polarisStore, err := polaris.NewStore(home + "/.deneb/polaris.db")
	if err != nil {
		logger.Error("polaris: failed to open store", "error", err)
		return nil, nil
	}
	m.PolarisStore = polarisStore // read by the opt-in compaction tuner
	return polaris.NewBridge(cached, polarisStore, logger), polarisStore
}

func (m *Manager) startSessionMemorySweep(transcriptDir string, polarisStore *polaris.Store) {
	logger := m.Host.Logger()
	maxAge := memorySweepRetention()
	if maxAge <= 0 {
		return
	}
	safego.GoWithSlog(logger, "memory-sweep", func() {
		if polarisStore != nil {
			polarisStore.SweepExpired(maxAge, logger)
		}
		sweepAutomatedTranscripts(transcriptDir, maxAge, logger)
	})
}

func (m *Manager) newSessionAgentLogWriter() *agentlog.Writer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return agentlog.NewWriter(home + "/.deneb/agent-logs")
}

func (m *Manager) wireSessionInsights(agentLogWriter *agentlog.Writer) {
	insightsEngine := m.Host.Insights()
	if insightsEngine == nil || agentLogWriter == nil {
		return
	}
	insightsEngine.SetToolAggregator(func(_ context.Context, since time.Time) []insights.ToolStat {
		return sessionInsightToolStats(agentLogWriter.Aggregate(since.UnixMilli()).Tools)
	})
}

func sessionInsightToolStats(stats []agentlog.ToolStat) []insights.ToolStat {
	out := make([]insights.ToolStat, 0, len(stats))
	for _, stat := range stats {
		errorRate := 0.0
		if stat.Calls > 0 {
			errorRate = float64(stat.Errors) / float64(stat.Calls)
		}
		out = append(out, insights.ToolStat{
			Name:      stat.Name,
			Calls:     stat.Calls,
			ErrorRate: errorRate,
			AvgMs:     stat.AvgMs,
		})
	}
	return out
}

func (m *Manager) buildSessionChatConfig(
	transcriptStore chat.TranscriptStore,
	agentLogWriter *agentlog.Writer,
) chat.HandlerConfig {
	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	m.Host.SetGenesisTranscripts(transcriptStore)
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = m.Host.JobTracker()
	chatCfg.AgentLog = agentLogWriter
	chatCfg.Ambient.TopicResolver = newTopicResolver(m.Host.Logger())

	var registry *modelrole.Registry
	m.InitModelRegistry(&chatCfg, &registry)
	m.initSessionAI(&chatCfg, registry)
	m.InitToolsAndDeps(&chatCfg, registry, transcriptStore, agentLogWriter)
	m.configureSessionChatCallbacks(&chatCfg)
	return chatCfg
}

func (m *Manager) initSessionAI(chatCfg *chat.HandlerConfig, registry *modelrole.Registry) {
	logger := m.Host.Logger()
	m.LocalAIHub = localai.New(localai.Config{}, registry, logger)
	chatCfg.LocalAIHub = m.LocalAIHub

	m.EmbeddingClient = embedding.New("", logger)
	chatCfg.Memory.Embedding = m.EmbeddingClient
	if m.PolarisStore != nil {
		m.PolarisStore.SetSummaryEmbedder(m.EmbeddingClient)
	}
	if wikiStore := m.Mail.WikiStore; wikiStore != nil {
		wikiStore.SetEmbedder(m.EmbeddingClient)
		m.Host.SafeGo("wiki-semantic-warm", func() {
			m.warmSessionSemanticIndexes(wikiStore)
		})
	}
}

func (m *Manager) warmSessionSemanticIndexes(store interface {
	WarmSemanticIndex(context.Context) error
	WarmDiarySemantic(context.Context) error
},
) {
	logger := m.Host.Logger()
	ctx, cancel := context.WithTimeout(m.Host.ShutdownCtx(), 10*time.Minute)
	defer cancel()
	if err := store.WarmSemanticIndex(ctx); err != nil {
		logger.Warn("wiki semantic warm incomplete", "error", err)
	} else {
		logger.Info("wiki semantic index warmed")
	}
	if err := store.WarmDiarySemantic(ctx); err != nil {
		logger.Warn("diary semantic warm incomplete", "error", err)
	} else {
		logger.Info("diary semantic index warmed")
	}
}

func (m *Manager) configureSessionChatCallbacks(chatCfg *chat.HandlerConfig) {
	logger := m.Host.Logger()
	if authManager := m.Host.AuthManager(); authManager != nil {
		chatCfg.AuthManager = authManager
	}
	chatCfg.ProviderConfigs = configresolve.LoadProviderConfigs(logger)
	chatCfg.ProviderRuntime = m.Host.ProviderRuntime()
	broadcaster := m.Host.Broadcaster()
	chatCfg.BroadcastRaw = streaming.BroadcastRawFunc(func(event string, data []byte) int {
		return broadcaster.BroadcastRaw(event, data)
	})
	if gatewaySubs := m.Host.GatewaySubs(); gatewaySubs != nil {
		chatCfg.EmitAgentFn = func(kind, sessionKey, runID string, payload map[string]any) {
			gatewaySubs.EmitAgent(events.AgentEvent{
				Kind:       kind,
				SessionKey: sessionKey,
				RunID:      runID,
				Payload:    payload,
			})
		}
		chatCfg.EmitTranscriptFn = func(sessionKey string, message any, messageID string) {
			gatewaySubs.EmitTranscript(events.TranscriptUpdate{
				SessionKey: sessionKey,
				Message:    message,
				MessageID:  messageID,
			})
		}
	}
	chatCfg.DreamTurnFn = func(ctx context.Context) {
		if autonomousSvc := m.Host.AutonomousSvc(); autonomousSvc != nil {
			autonomousSvc.IncrementDreamTurn(ctx)
		}
	}
	chatCfg.PreferenceSignalFn = func() {
		if dreamer := m.Host.WikiDreamer(); dreamer != nil {
			dreamer.NotePreferenceSignal()
		}
	}
	chatCfg.DeliverablePublisher = func(text string) (bool, error) {
		return m.ProactiveRelay.PublishDeliverable(text)
	}
	chatCfg.RecordActivity = m.recordChatActivity
}

func (m *Manager) configureSessionChatHandler() {
	m.ChatHandler.SetStatusDepsFunc(func(sessionKey string) chat.StatusDeps {
		status := chat.StatusDeps{Version: m.Host.Version(), StartedAt: m.Host.StartedAt()}
		if m.Sessions != nil {
			status.SessionCount = m.Sessions.Count()
		}
		if sess := m.Sessions.Get(sessionKey); sess != nil && sess.FailureReason != "" {
			status.LastFailureReason = sess.FailureReason
		}
		return status
	})

	denebDir := m.Host.DenebDir()
	if denebDir == "" {
		return
	}
	checkpointRoot := filepath.Join(denebDir, "checkpoints")
	m.ChatHandler.SetCheckpointRoot(checkpointRoot)
	m.initCheckpointLifecycle(checkpointRoot)
}

func (m *Manager) sessionSendFunc() func(sessionKey, message string) error {
	return func(sessionKey, message string) error {
		request := &protocol.RequestFrame{
			ID:     shortid.New("tool_send"),
			Method: "sessions.send",
		}
		params := map[string]string{"key": sessionKey, "message": message}
		request.Params, _ = json.Marshal(params) // marshal of known-good types cannot fail
		response := m.ChatHandler.SessionsSend(context.Background(), request)
		if response != nil && response.Error != nil {
			return errors.New(response.Error.Message)
		}
		return nil
	}
}

func (m *Manager) initSessionProactiveRelay(
	transcriptStore chat.TranscriptStore,
	agentLogWriter *agentlog.Writer,
) {
	logger := m.Host.Logger()
	m.ProactiveRelay = proactive.NewRelay(proactive.Deps{
		TranscriptStore: transcriptStore,
		Logger:          logger,
		PushHub:         m.Host.PushHub(),
		PushFCM:         m.Host.PushNotifier(),
		WorkFeed:        m.Mail.NativeWorkFeedStore(),
		NativeSync:      m.Mail.NativeSyncStore,
		BehaviorLog:     agentLogWriter,
		Sessions:        m.Sessions,
		CardTitler: func(content string) (string, string) {
			return proactive.CardTitleSummary(m.Host.ShutdownCtx(), content)
		},
		WorkModel: m.resolveFeedWorkModel,
	})
}

func (m *Manager) wireSessionCronRelay(transcriptStore chat.TranscriptStore) {
	if m.CronService == nil || transcriptStore == nil {
		return
	}
	m.CronService.SetTranscriptCloner(transcriptStore, "")
	m.CronService.SetMainSessionHandoff(m.relayCronAnalysis)
}

func cronJobUsesCollapsedRelay(jobID string) bool {
	return strings.HasPrefix(jobID, "email-")
}

func proactiveAnalysisPreview(analysis string) string {
	if len(analysis) <= 120 {
		return analysis
	}
	return analysis[:120] + "…"
}

func (m *Manager) relayCronAnalysis(
	ctx context.Context,
	channel, to, jobID, analysis string,
) (bool, error) {
	logger := m.Host.Logger()
	if to == "" || strings.TrimSpace(analysis) == "" {
		return false, nil
	}
	sessionKey := channel + ":" + to
	relayFn := m.ProactiveRelay.Relay
	if cronJobUsesCollapsedRelay(jobID) {
		relayFn = m.ProactiveRelay.RelayCollapsed
	}
	delivered, err := relayFn(ctx, sessionKey, analysis)
	if err != nil {
		logger.Error("cron proactive relay failed",
			"jobId", jobID, "sessionKey", sessionKey, "error", err)
		return false, err
	}
	if delivered {
		logger.Info("cron proactive relay delivered",
			"jobId", jobID,
			"sessionKey", sessionKey,
			"bytes", len(analysis),
			"preview", proactiveAnalysisPreview(analysis))
	}
	return delivered, nil
}

func (m *Manager) wireSessionACP(transcriptStore chat.TranscriptStore) {
	m.wireACPTranscriptLoader(transcriptStore)
	m.wireACPResultInjection(transcriptStore)
}

func (m *Manager) wireACPTranscriptLoader(transcriptStore chat.TranscriptStore) {
	if m.ACPDeps == nil || transcriptStore == nil {
		return
	}
	m.ACPDeps.TranscriptLoader = func(sessionKey string, limit int) ([]string, []string, error) {
		messages, _, err := transcriptStore.Load(sessionKey, limit)
		if err != nil {
			return nil, nil, err
		}
		roles := make([]string, len(messages))
		contents := make([]string, len(messages))
		for i, message := range messages {
			roles[i] = message.Role
			contents[i] = message.TextContent()
		}
		return roles, contents, nil
	}
}

// resolveFeedWorkModel returns the display name of the model behind proactive
// 업무 feed reports — the main agent-turn model. Cron morning letter, mail
// analysis synthesis, heartbeat, goal, and event ingest all run as main-role
// turns, so the main model is the "작업 모델" for the feed. Returns "" when the
// model registry is unwired (older tests), which leaves the feed footer off.
func (m *Manager) resolveFeedWorkModel() string {
	if m.ModelRegistry == nil {
		return ""
	}
	return m.ModelRegistry.Model(modelrole.RoleMain)
}

func (m *Manager) wireACPResultInjection(transcriptStore chat.TranscriptStore) {
	if m.ACPDeps == nil || transcriptStore == nil {
		return
	}
	logger := m.Host.Logger()
	projector := acp.NewACPProjector(m.ACPDeps.Registry)
	m.ACPResultInjectionUnsub = acp.StartSubagentResultInjection(acp.ResultInjectionDeps{
		Registry:  m.ACPDeps.Registry,
		Projector: projector,
		Sessions:  m.Sessions,
		Transcript: acp.TranscriptAppendFunc(func(sessionKey, text string) error {
			message := chat.NewTextChatMessage("system", text, 0)
			return transcriptStore.Append(sessionKey, message)
		}),
		Logger: logger,
	})
}
