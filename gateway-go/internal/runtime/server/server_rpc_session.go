package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	handlerwire "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

// registerSessionRPCMethods registers session state, repair, daemon status, and
// the full chat handler pipeline (init + all chat/session-exec RPC registrations).
func (s *Server) registerSessionRPCMethods() {
	s.registerSessionDomainMethods()
	transcriptStore, transcriptDir, polarisStoreForSweep := s.openSessionTranscriptStore()
	s.startSessionMemorySweep(transcriptDir, polarisStoreForSweep)
	agentLogWriter := s.newSessionAgentLogWriter()
	s.agentLogWriter = agentLogWriter
	s.wireSessionInsights(agentLogWriter)

	chatCfg := s.buildSessionChatConfig(transcriptStore, agentLogWriter)

	s.chatHandler = pipebind.NewHandler(
		s.sessions,
		func(event string, payload json.RawMessage) (int, []error) {
			return s.broadcastSessionEvent(event, svcbind.PayloadFromRaw(payload))
		},
		s.logger,
		chatCfg,
	)

	s.configureSessionChatHandler()

	// Wire SendFn after handler creation to avoid circular deps.
	sendFn := s.sessionSendFunc()
	s.toolDeps.Sessions.SendFn = sendFn
	s.toolDeps.Chrono.SendFn = sendFn

	s.initSessionProactiveRelay(transcriptStore, agentLogWriter)
	s.wireSessionCronRelay(transcriptStore)
	s.wireSessionACP(transcriptStore)

	// Chat, BTW, miniapp-chat bridge, Exec, Wiki, Genesis, and GmailAnalyze are
	// registered in registerLateMethods() after this function returns; Aurora
	// (dreaming) is wired later in registerWorkflowSideEffects().
}

func (s *Server) registerSessionDomainMethods() {
	s.dispatcher.RegisterDomain(handlerwire.SessionMethods(handlerwire.SessionDeps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
	}))
}

func targetedToolRunID(event string, payload svcbind.EventPayload) string {
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
// typed svcbind.EventPayload used by the runtime broadcaster. Keeps higher
// layers from importing runtime/events (Health Bench upward-import rule).
func eventPayloadFromAny(payload any) svcbind.EventPayload {
	switch v := payload.(type) {
	case nil:
		return svcbind.EventPayload{}
	case svcbind.EventPayload:
		return v
	case json.RawMessage:
		return svcbind.PayloadFromRaw(v)
	case []byte:
		return svcbind.PayloadFromRaw(v)
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return svcbind.EventPayload{}
		}
		return svcbind.PayloadFromRaw(raw)
	}
}

// broadcastSessionEvent keeps tool progress scoped to the connection that
// started the run; every other event retains the normal fan-out behavior.
func (s *Server) broadcastSessionEvent(event string, payload svcbind.EventPayload) (int, []error) {
	runID := targetedToolRunID(event, payload)
	if runID != "" {
		if connID := s.broadcaster.ToolEventRecipient(runID); connID != "" {
			return s.broadcaster.BroadcastToConnIDs(event, payload, map[string]struct{}{connID: {}})
		}
	}
	return s.broadcaster.Broadcast(event, payload)
}

func (s *Server) openSessionTranscriptStore() (pipebind.TranscriptStore, string, *pipebind.Store) {
	transcriptDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		transcriptDir = home + "/.deneb/transcripts"
	}
	if transcriptDir == "" {
		return nil, "", nil
	}

	cached := pipebind.NewCachedTranscriptStore(pipebind.NewFileTranscriptStore(transcriptDir), 0)
	transcriptStore, polarisStore := s.openPolarisTranscriptBridge(cached)
	if transcriptStore == nil {
		// Assembly will return an error, but the gateway can still serve other functions.
		transcriptStore = cached
	}
	return transcriptStore, transcriptDir, polarisStore
}

func (s *Server) openPolarisTranscriptBridge(cached pipebind.TranscriptStore) (pipebind.TranscriptStore, *pipebind.Store) {
	home, err := os.UserHomeDir()
	if err != nil {
		s.logger.Error("polaris: cannot determine home directory", "error", err)
		return nil, nil
	}
	polarisStore, err := pipebind.NewStore(home + "/.deneb/pipebind.db")
	if err != nil {
		s.logger.Error("polaris: failed to open store", "error", err)
		return nil, nil
	}
	s.polarisStore = polarisStore // read by the opt-in compaction tuner
	return pipebind.NewBridge(cached, polarisStore, s.logger), polarisStore
}

func (s *Server) startSessionMemorySweep(transcriptDir string, polarisStore *pipebind.Store) {
	maxAge := memorySweepRetention()
	if maxAge <= 0 {
		return
	}
	safego.GoWithSlog(s.logger, "memory-sweep", func() {
		if polarisStore != nil {
			polarisStore.SweepExpired(maxAge, s.logger)
		}
		sweepAutomatedTranscripts(transcriptDir, maxAge, s.logger)
	})
}

func (s *Server) newSessionAgentLogWriter() *infrabind.Writer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return infrabind.NewWriter(home + "/.deneb/agent-logs")
}

func (s *Server) wireSessionInsights(agentLogWriter *infrabind.Writer) {
	if s.insights == nil || agentLogWriter == nil {
		return
	}
	s.insights.SetToolAggregator(func(_ context.Context, since time.Time) []svcbind.ToolStat {
		return sessionInsightToolStats(agentLogWriter.Aggregate(since.UnixMilli()).Tools)
	})
}

func sessionInsightToolStats(stats []infrabind.ToolStat) []svcbind.ToolStat {
	out := make([]svcbind.ToolStat, 0, len(stats))
	for _, stat := range stats {
		errorRate := 0.0
		if stat.Calls > 0 {
			errorRate = float64(stat.Errors) / float64(stat.Calls)
		}
		out = append(out, svcbind.ToolStat{
			Name:      stat.Name,
			Calls:     stat.Calls,
			ErrorRate: errorRate,
			AvgMs:     stat.AvgMs,
		})
	}
	return out
}

func (s *Server) buildSessionChatConfig(
	transcriptStore pipebind.TranscriptStore,
	agentLogWriter *infrabind.Writer,
) pipebind.HandlerConfig {
	chatCfg := pipebind.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	s.genesisTranscripts = transcriptStore
	chatCfg.Tools = pipebind.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter
	chatCfg.Ambient.TopicResolver = newTopicResolver(s.logger)

	var registry *aibind.ModelRoleRegistry
	s.initMemorySubsystem(&chatCfg, &registry)
	s.initSessionAI(&chatCfg, registry)
	s.initToolsAndDeps(&chatCfg, registry, transcriptStore, agentLogWriter)
	s.configureSessionChatCallbacks(&chatCfg)
	return chatCfg
}

func (s *Server) initSessionAI(chatCfg *pipebind.HandlerConfig, registry *aibind.ModelRoleRegistry) {
	s.localAIHub = aibind.NewLocalAI(aibind.Config{}, registry, s.logger)

	s.embeddingClient = aibind.NewEmbedding("", s.logger)
	chatCfg.Memory.Embedding = s.embeddingClient
	if s.polarisStore != nil {
		s.polarisStore.SetSummaryEmbedder(s.embeddingClient)
	}
	if s.wikiStore != nil {
		s.wikiStore.SetEmbedder(s.embeddingClient)
		if reranker := aibind.NewFromEnv(); reranker != nil {
			s.wikiStore.SetReranker(reranker)
		}
		store := s.wikiStore
		s.safeGo("wiki-semantic-warm", func() {
			s.warmSessionSemanticIndexes(store)
		})
	}
}

func (s *Server) warmSessionSemanticIndexes(store interface {
	WarmSemanticIndex(context.Context) error
	WarmDiarySemantic(context.Context) error
},
) {
	ctx, cancel := context.WithTimeout(s.ShutdownCtx(), 10*time.Minute)
	defer cancel()
	if err := store.WarmSemanticIndex(ctx); err != nil {
		s.logger.Warn("wiki semantic warm incomplete", "error", err)
	} else {
		s.logger.Info("wiki semantic index warmed")
	}
	if err := store.WarmDiarySemantic(ctx); err != nil {
		s.logger.Warn("diary semantic warm incomplete", "error", err)
	} else {
		s.logger.Info("diary semantic index warmed")
	}
}

func (s *Server) configureSessionChatCallbacks(chatCfg *pipebind.HandlerConfig) {
	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = svcbind.LoadProviderConfigs(s.logger)
	chatCfg.ProviderRuntime = s.providerRuntime
	chatCfg.BroadcastRaw = pipebind.BroadcastRawFunc(func(event string, data []byte) int {
		return s.broadcaster.BroadcastRaw(event, data)
	})
	if s.gatewaySubs != nil {
		chatCfg.EmitAgentFn = func(kind, sessionKey, runID string, payload map[string]any) {
			ep, _ := svcbind.PayloadOf(payload)
			s.gatewaySubs.EmitAgent(svcbind.AgentEvent{
				Kind:       kind,
				SessionKey: sessionKey,
				RunID:      runID,
				Payload:    ep,
			})
		}
		chatCfg.EmitTranscriptFn = func(sessionKey string, message json.RawMessage, messageID string) {
			s.gatewaySubs.EmitTranscript(svcbind.TranscriptUpdate{
				SessionKey: sessionKey,
				Message:    svcbind.PayloadFromRaw(message),
				MessageID:  messageID,
			})
		}
	}
	chatCfg.DreamTurnFn = func(ctx context.Context) {
		if s.autonomousSvc != nil {
			s.autonomousSvc.IncrementDreamTurn(ctx)
		}
	}
	chatCfg.PreferenceSignalFn = func() {
		if s.wikiDreamer != nil {
			s.wikiDreamer.NotePreferenceSignal()
		}
	}
	chatCfg.DeliverablePublisher = func(text string) (bool, error) {
		return s.proactiveRelay.PublishDeliverable(text)
	}
	chatCfg.RecordActivity = s.recordChatActivity
}

func (s *Server) configureSessionChatHandler() {
	s.chatHandler.SetStatusDepsFunc(func(sessionKey string) pipebind.StatusDeps {
		status := pipebind.StatusDeps{Version: s.version, StartedAt: s.startedAt}
		if s.sessions != nil {
			status.SessionCount = s.sessions.Count()
		}
		if session := s.sessions.Get(sessionKey); session != nil && session.FailureReason != "" {
			status.LastFailureReason = session.FailureReason
		}
		return status
	})

	if s.denebDir == "" {
		return
	}
	checkpointRoot := filepath.Join(s.denebDir, "checkpoints")
	s.chatHandler.SetCheckpointRoot(checkpointRoot)
	s.initCheckpointLifecycle(checkpointRoot)
}

func (s *Server) sessionSendFunc() func(sessionKey, message string) error {
	return func(sessionKey, message string) error {
		request := &protocol.RequestFrame{
			ID:     infrabind.NewShortID("tool_send"),
			Method: "sessions.send",
		}
		params := map[string]string{"key": sessionKey, "message": message}
		request.Params, _ = json.Marshal(params) // marshal of known-good types cannot fail
		response := s.chatHandler.SessionsSend(context.Background(), request)
		if response != nil && response.Error != nil {
			return errors.New(response.Error.Message)
		}
		return nil
	}
}

func (s *Server) initSessionProactiveRelay(
	transcriptStore pipebind.TranscriptStore,
	agentLogWriter *infrabind.Writer,
) {
	s.proactiveRelay = svcbind.NewRelay(svcbind.ProactiveDeps{
		TranscriptStore: transcriptStore,
		Logger:          s.logger,
		PushHub:         s.pushHub,
		PushFCM:         s.pushNotifier,
		WorkFeed:        s.nativeWorkFeedStore(),
		NativeSync:      s.nativeSyncStore,
		BehaviorLog:     agentLogWriter,
		Sessions:        s.sessions,
		CardTitler: func(content string) (string, string) {
			return svcbind.CardTitleSummary(s.ShutdownCtx(), content)
		},
		WorkModel: s.resolveFeedWorkModel,
	})
}

func (s *Server) wireSessionCronRelay(transcriptStore pipebind.TranscriptStore) {
	if s.cronService == nil || transcriptStore == nil {
		return
	}
	s.cronService.SetTranscriptCloner(transcriptStore, "")
	s.cronService.SetMainSessionHandoff(s.relayCronAnalysis)
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

func (s *Server) relayCronAnalysis(
	ctx context.Context,
	channel, to, jobID, analysis string,
) (bool, error) {
	if to == "" || strings.TrimSpace(analysis) == "" {
		return false, nil
	}
	sessionKey := channel + ":" + to
	relayFn := s.proactiveRelay.Relay
	if cronJobUsesCollapsedRelay(jobID) {
		relayFn = s.proactiveRelay.RelayCollapsed
	}
	delivered, err := relayFn(ctx, sessionKey, analysis)
	if err != nil {
		s.logger.Error("cron proactive relay failed",
			"jobId", jobID, "sessionKey", sessionKey, "error", err)
		return false, err
	}
	if delivered {
		s.logger.Info("cron proactive relay delivered",
			"jobId", jobID,
			"sessionKey", sessionKey,
			"bytes", len(analysis),
			"preview", proactiveAnalysisPreview(analysis))
	}
	return delivered, nil
}

func (s *Server) wireSessionACP(transcriptStore pipebind.TranscriptStore) {
	s.wireACPTranscriptLoader(transcriptStore)
	s.wireACPResultInjection(transcriptStore)
}

func (s *Server) wireACPTranscriptLoader(transcriptStore pipebind.TranscriptStore) {
	if s.acpDeps == nil || transcriptStore == nil {
		return
	}
	s.acpDeps.TranscriptLoader = func(sessionKey string, limit int) ([]string, []string, error) {
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

func (s *Server) wireACPResultInjection(transcriptStore pipebind.TranscriptStore) {
	if s.acpDeps == nil || transcriptStore == nil {
		return
	}
	projector := pipebind.NewACPProjector(s.acpDeps.Registry)
	s.acpResultInjectionUnsub = pipebind.StartSubagentResultInjection(pipebind.ResultInjectionDeps{
		Registry:  s.acpDeps.Registry,
		Projector: projector,
		Sessions:  s.sessions,
		Transcript: pipebind.TranscriptAppendFunc(func(sessionKey, text string) error {
			message := pipebind.NewTextChatMessage("system", text, 0)
			return transcriptStore.Append(sessionKey, message)
		}),
		Logger: s.logger,
	})
}
