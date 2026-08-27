package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	airerank "github.com/choiceoh/deneb/gateway-go/internal/ai/rerank"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	chattranscript "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// registerSessionRPCMethods registers session state, repair, daemon status, and
// the full chat handler pipeline (init + all chat/session-exec RPC registrations).
func (s *Server) registerSessionRPCMethods() error {
	transcriptStore, transcriptDir, polarisStoreForSweep := s.openSessionTranscriptStore()
	agentLogWriter := s.newSessionAgentLogWriter()

	chatCfg, err := s.buildSessionChatConfig(transcriptStore, agentLogWriter)
	if err != nil {
		if polarisStoreForSweep != nil {
			if closeErr := polarisStoreForSweep.Close(); closeErr != nil {
				s.logger.Warn("polaris close after chat config failure", "error", closeErr)
			}
			s.polarisStore = nil
		}
		return fmt.Errorf("build session chat config: %w", err)
	}

	s.registerSessionDomainMethods()
	s.startSessionMemorySweep(transcriptDir, polarisStoreForSweep)
	s.agentLogWriter = agentLogWriter
	s.wireSessionInsights(agentLogWriter)

	s.chatHandler = chat.NewHandler(
		s.sessions,
		func(event string, payload json.RawMessage) (int, []error) {
			return s.broadcastSessionEvent(event, events.PayloadFromRaw(payload))
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
	return nil
}

func (s *Server) registerSessionDomainMethods() {
	s.dispatcher.RegisterDomain(handlersession.Methods(handlersession.Deps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
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
func (s *Server) broadcastSessionEvent(event string, payload events.EventPayload) (int, []error) {
	runID := targetedToolRunID(event, payload)
	if runID != "" {
		if connID := s.broadcaster.ToolEventRecipient(runID); connID != "" {
			return s.broadcaster.BroadcastToConnIDs(event, payload, map[string]struct{}{connID: {}})
		}
	}
	return s.broadcaster.Broadcast(event, payload)
}

func (s *Server) openSessionTranscriptStore() (chat.TranscriptStore, string, *polaris.Store) {
	transcriptDir := transcriptBaseDir()
	if transcriptDir == "" {
		return nil, "", nil
	}

	cached := chattranscript.NewCachedTranscriptStore(chattranscript.NewFileTranscriptStore(transcriptDir), 0)
	transcriptStore, polarisStore := s.openPolarisTranscriptBridge(cached)
	if transcriptStore == nil {
		// Assembly will return an error, but the gateway can still serve other functions.
		transcriptStore = cached
	}
	return transcriptStore, transcriptDir, polarisStore
}

func (s *Server) openPolarisTranscriptBridge(cached chat.TranscriptStore) (chat.TranscriptStore, *polaris.Store) {
	// Polaris indexes the transcripts above, so it must live beside them: a dev
	// gateway writing its probes into the operator's conversation index is the
	// same leak, one layer down.
	polarisStore, err := polaris.NewStore(filepath.Join(config.ResolveStateDir(), "polaris.db"))
	if err != nil {
		s.logger.Error("polaris: failed to open store", "error", err)
		return nil, nil
	}
	s.polarisStore = polarisStore // read by the opt-in compaction tuner
	return polaris.NewBridge(cached, polarisStore, s.logger), polarisStore
}

func (s *Server) startSessionMemorySweep(transcriptDir string, polarisStore *polaris.Store) {
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

func (s *Server) newSessionAgentLogWriter() *agentlog.Writer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	writer := agentlog.NewWriter(home + "/.deneb/agent-logs")
	// Teach the aggregator which sessions a person was waiting on. agentlog is
	// a core package and cannot reach the run-kind vocabulary in domain/session,
	// so the classifier is injected here rather than copied there.
	writer.SetInteractiveSessionFilter(session.IsInteractiveSessionKey)
	// Retention runs once per process start, off the startup path: the sweep
	// stats one directory entry per session ever logged, which had grown to
	// 5K+ files with no other bound.
	safego.GoWithSlog(s.logger, "agentlog-retention", func() {
		writer.PruneStaleFiles(time.Now())
	})
	return writer
}

func (s *Server) wireSessionInsights(agentLogWriter *agentlog.Writer) {
	if s.insights == nil || agentLogWriter == nil {
		return
	}
	s.insights.SetToolAggregator(func(_ context.Context, since time.Time) []insights.ToolStat {
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

func (s *Server) buildSessionChatConfig(
	transcriptStore chat.TranscriptStore,
	agentLogWriter *agentlog.Writer,
) (chat.HandlerConfig, error) {
	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	s.genesisTranscripts = transcriptStore
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter
	chatCfg.Ambient.TopicResolver = newTopicResolver(s.logger)

	var registry *modelrole.Registry
	if err := s.initMemorySubsystem(&chatCfg, &registry); err != nil {
		return chat.HandlerConfig{}, fmt.Errorf("initialize memory subsystem: %w", err)
	}
	s.initSessionAI(&chatCfg, registry)
	s.initToolsAndDeps(&chatCfg, registry, transcriptStore, agentLogWriter)
	s.configureSessionChatCallbacks(&chatCfg)
	return chatCfg, nil
}

func (s *Server) initSessionAI(chatCfg *chat.HandlerConfig, registry *modelrole.Registry) {
	s.localAIHub = localai.New(localai.Config{}, registry, s.logger)

	s.embeddingClient = embedding.New("", s.logger)
	s.rerankerClient = airerank.NewFromEnv()
	chatCfg.Memory.Embedding = s.embeddingClient
	var warmers []semanticWarmTarget
	if s.mailStore != nil {
		s.mailStore.SetEmbedder(s.embeddingClient)
		store := s.mailStore
		warmers = append(warmers, semanticWarmTarget{name: "mail", warm: store.WarmSemanticIndex})
		if s.rerankerClient != nil {
			s.mailStore.SetReranker(s.rerankerClient)
		} else {
			s.mailStore.SetReranker(nil)
		}
	}
	if s.workFeedStore != nil {
		s.workFeedStore.SetEmbedder(s.embeddingClient)
		store := s.workFeedStore
		warmers = append(warmers, semanticWarmTarget{name: "workfeed", warm: store.WarmSemanticIndex})
	}
	if s.polarisStore != nil {
		s.polarisStore.SetSummaryEmbedder(s.embeddingClient)
	}
	if s.wikiStore != nil {
		s.wikiStore.SetEmbedder(s.embeddingClient)
		if s.rerankerClient != nil {
			s.wikiStore.SetReranker(s.rerankerClient)
		}
		store := s.wikiStore
		warmers = append(
			warmers,
			semanticWarmTarget{name: "wiki", warm: store.WarmSemanticIndex},
			semanticWarmTarget{name: "diary", warm: store.WarmDiarySemantic},
		)
	}
	if len(warmers) > 0 {
		s.safeGo("retrieval-semantic-warm", func() {
			s.warmSessionSemanticIndexes(warmers)
		})
	}
}

type semanticWarmTarget struct {
	name string
	warm func(context.Context) error
}

func (s *Server) warmSessionSemanticIndexes(warmers []semanticWarmTarget) {
	ctx, cancel := context.WithTimeout(s.ShutdownCtx(), 10*time.Minute)
	defer cancel()
	if err := s.waitForEmbeddingReady(ctx); err != nil {
		s.logger.Warn("semantic warm skipped; embedding unavailable", "error", err)
		return
	}
	for _, target := range warmers {
		if target.warm == nil {
			continue
		}
		if err := target.warm(ctx); err != nil {
			s.logger.Warn("semantic warm incomplete", "index", target.name, "error", err)
		} else {
			s.logger.Info("semantic index warmed", "index", target.name)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (s *Server) waitForEmbeddingReady(ctx context.Context) error {
	if s.embeddingClient == nil || s.embeddingClient.IsHealthy() {
		return nil
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.embeddingClient.IsHealthy() {
				return nil
			}
		}
	}
}

func (s *Server) configureSessionChatCallbacks(chatCfg *chat.HandlerConfig) {
	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = configresolve.LoadProviderConfigs(s.logger)
	chatCfg.ProviderRuntime = s.providerRuntime
	chatCfg.BroadcastRaw = streaming.BroadcastRawFunc(func(event string, data []byte) int {
		return s.broadcaster.BroadcastRaw(event, data)
	})
	if s.gatewaySubs != nil {
		chatCfg.EmitAgentFn = func(kind, sessionKey, runID string, payload map[string]any) {
			ep, _ := events.PayloadOf(payload)
			s.gatewaySubs.EmitAgent(events.AgentEvent{
				Kind:       kind,
				SessionKey: sessionKey,
				RunID:      runID,
				Payload:    ep,
			})
		}
		chatCfg.EmitTranscriptFn = func(sessionKey string, message json.RawMessage, messageID string) {
			s.gatewaySubs.EmitTranscript(events.TranscriptUpdate{
				SessionKey: sessionKey,
				Message:    events.PayloadFromRaw(message),
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
	chatCfg.ProjectSignalFn = func() {
		if s.wikiDreamer != nil {
			s.wikiDreamer.NoteProjectSignal()
		}
	}
	chatCfg.DeliverablePublisher = func(text string) (bool, error) {
		return s.proactiveRelay.PublishDeliverable(text)
	}
	// Korean-first surface: the system prompt is overwhelmingly English, so the
	// model reasons in English even on Korean turns (measured 2026-07-26: 82% of
	// stored reasoning blocks are English-dominant). Left nil when DeepL is not
	// configured, which disables the feature instead of failing once per turn.
	if toolbind.ThinkingTranslatorEnabled() {
		chatCfg.TranslateThinking = toolbind.TranslateThinking
	}
	chatCfg.RecordActivity = s.recordChatActivity
}

func (s *Server) configureSessionChatHandler() {
	s.chatHandler.SetStatusDepsFunc(func(sessionKey string) chat.StatusDeps {
		status := chat.StatusDeps{Version: s.version, StartedAt: s.startedAt}
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
			ID:     shortid.New("tool_send"),
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
	transcriptStore chat.TranscriptStore,
	agentLogWriter *agentlog.Writer,
) {
	s.proactiveRelay = proactive.NewRelay(proactive.Deps{
		TranscriptStore: transcriptStore,
		Logger:          s.logger,
		PushHub:         s.pushHub,
		PushFCM:         s.pushNotifier,
		WorkFeed:        s.nativeWorkFeedStore(),
		NativeSync:      s.nativeSyncStore,
		BehaviorLog:     agentLogWriter,
		Sessions:        s.sessions,
		CardTitler: func(content string) (string, string) {
			return proactive.CardTitleSummary(s.ShutdownCtx(), content)
		},
		WorkModel: s.resolveFeedWorkModel,
	})
}

func (s *Server) wireSessionCronRelay(transcriptStore chat.TranscriptStore) {
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

func (s *Server) wireSessionACP(transcriptStore chat.TranscriptStore) {
	s.wireACPResultInjection(transcriptStore)
}

func (s *Server) wireACPResultInjection(transcriptStore chat.TranscriptStore) {
	if s.acpDeps == nil || transcriptStore == nil {
		return
	}
	projector := acp.NewACPProjector(s.acpDeps.Registry)
	s.acpResultInjectionUnsub = acp.StartSubagentResultInjection(acp.ResultInjectionDeps{
		Registry:  s.acpDeps.Registry,
		Projector: projector,
		Sessions:  s.sessions,
		Transcript: acp.TranscriptAppendFunc(func(sessionKey, text string) error {
			message := chat.NewTextChatMessage("system", text, 0)
			return transcriptStore.Append(sessionKey, message)
		}),
		Logger: s.logger,
	})
}
