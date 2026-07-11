package server

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

// registerSessionRPCMethods registers session state, repair, daemon status, and
// the full chat handler pipeline (init + all chat/session-exec RPC registrations).
func (s *Server) registerSessionRPCMethods() {
	// Session state methods (patch/reset/preview/resolve).
	sessionDeps := handlersession.Deps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
	}
	s.dispatcher.RegisterDomain(handlersession.Methods(sessionDeps))

	// Session repair methods are now included in handlersession.Methods().

	// Chat methods — native agent execution.
	// For "session.tool" events, check if a specific tool event recipient is
	// registered for the run and target the broadcast to that connection only.
	broadcastFn := func(event string, payload any) (int, []error) {
		if event == "session.tool" {
			if m, ok := payload.(map[string]any); ok {
				if runID, _ := m["runId"].(string); runID != "" {
					if connID := s.broadcaster.ToolEventRecipient(runID); connID != "" {
						return s.broadcaster.BroadcastToConnIDs(event, payload, map[string]struct{}{connID: {}})
					}
				}
			}
		}
		return s.broadcaster.Broadcast(event, payload)
	}

	// Determine transcript base directory.
	transcriptDir := ""
	if home, err := os.UserHomeDir(); err == nil {
		transcriptDir = home + "/.deneb/transcripts"
	}
	var transcriptStore chat.TranscriptStore
	var polarisStoreForSweep *polaris.Store
	if transcriptDir != "" {
		cached := chat.NewCachedTranscriptStore(
			chat.NewFileTranscriptStore(transcriptDir), 0,
		)

		// Wrap with Polaris dual-write bridge (required for summary-based assembly).
		home, err := os.UserHomeDir()
		if err != nil {
			s.logger.Error("polaris: cannot determine home directory", "error", err)
		} else {
			polarisStore, polarisErr := polaris.NewStore(home + "/.deneb/polaris.db")
			if polarisErr != nil {
				s.logger.Error("polaris: failed to open store", "error", polarisErr)
			} else {
				transcriptStore = polaris.NewBridge(cached, polarisStore, s.logger)
				polarisStoreForSweep = polarisStore
				s.polarisStore = polarisStore // read by the opt-in compaction tuner
			}
		}
		// Fallback: if Polaris initialization failed, use cached store directly.
		// Assembly will return an error but the gateway can still serve other functions.
		if transcriptStore == nil {
			transcriptStore = cached
		}
	}

	// Startup retention GC: bound the Polaris raw-message store and
	// automated-session transcripts, which otherwise grow forever (237MB /
	// 1,400+ files observed). Runs once, off the startup path.
	if maxAge := memorySweepRetention(); maxAge > 0 {
		sweepPolaris := polarisStoreForSweep
		sweepDir := transcriptDir
		safego.GoWithSlog(s.logger, "memory-sweep", func() {
			if sweepPolaris != nil {
				sweepPolaris.SweepExpired(maxAge, s.logger)
			}
			sweepAutomatedTranscripts(sweepDir, maxAge, s.logger)
		})
	}

	// Initialize agent detail log writer.
	var agentLogWriter *agentlog.Writer
	if home, err := os.UserHomeDir(); err == nil {
		agentLogWriter = agentlog.NewWriter(home + "/.deneb/agent-logs")
	}
	// Share with background workers (autonomous task loop) which run in a
	// different init function (registerWorkflowSideEffects) and so cannot see
	// this local. They emit background.job events to the same JSONL store.
	s.agentLogWriter = agentLogWriter

	// Feed the insights engine's tool aggregator from the agent log so
	// `/insights` and the insights.generate RPC surface the cross-session
	// tool-usage histogram (calls / error rate / avg duration). The engine was
	// created in registerEarlyMethods with no aggregator wired, so its tool
	// section was always empty ("도구 사용량 수집 미연결"); this lights it up from
	// the run.end/turn.tool events the agent log already records.
	if s.insights != nil && agentLogWriter != nil {
		s.insights.SetToolAggregator(func(_ context.Context, since time.Time) []insights.ToolStat {
			agg := agentLogWriter.Aggregate(since.UnixMilli())
			out := make([]insights.ToolStat, 0, len(agg.Tools))
			for _, t := range agg.Tools {
				rate := 0.0
				if t.Calls > 0 {
					rate = float64(t.Errors) / float64(t.Calls)
				}
				out = append(out, insights.ToolStat{
					Name:      t.Name,
					Calls:     t.Calls,
					ErrorRate: rate,
					AvgMs:     t.AvgMs,
				})
			}
			return out
		})
	}

	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	s.genesisTranscripts = transcriptStore // share with genesis for session context loading
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter

	// Wire the per-topic knowledge resolver (deneb.json topics.map). Returns
	// nil when topics are unconfigured, so the chat handler simply skips
	// per-topic injection.
	chatCfg.Ambient.TopicResolver = newTopicResolver(s.logger)

	// Phase 1: Memory subsystem (unified store, Aurora, memory, wiki).
	var reg *modelrole.Registry
	s.initMemorySubsystem(&chatCfg, &reg)

	// Create centralized local AI hub now that the model registry is available.
	s.localAIHub = localai.New(localai.Config{}, reg, s.logger)
	chatCfg.LocalAIHub = s.localAIHub

	// Create BGE-M3 embedding client for MMR compaction fallback.
	// Starts background health probing; gracefully degrades if server is unavailable.
	s.embeddingClient = embedding.New("", s.logger)
	chatCfg.Memory.Embedding = s.embeddingClient

	// Attach the embedder to the polaris store so cross-session recall can blend a
	// semantic match against past sessions' summaries with the keyword search.
	// Degrades to keyword-only when the embedding server is down.
	if s.polarisStore != nil {
		s.polarisStore.SetSummaryEmbedder(s.embeddingClient)
	}

	// Attach the same embedding client to the wiki so Search blends BM25 with
	// semantic neighbors. Degrades to pure BM25 whenever the server is down.
	if s.wikiStore != nil {
		s.wikiStore.SetEmbedder(s.embeddingClient)
		// Warm the vector index off the request path so the first recall queries
		// blend dense vectors instead of silently falling back to BM25. The lazy
		// per-query refresh runs under the ~1.5s recall deadline, where a large
		// uncached page can time out every turn and never persist to the cache;
		// a generous background warm builds it once. Degrades to BM25 if the
		// embedding server is down.
		store := s.wikiStore
		s.safeGo("wiki-semantic-warm", func() {
			ctx, cancel := context.WithTimeout(s.ShutdownCtx(), 10*time.Minute)
			defer cancel()
			if err := store.WarmSemanticIndex(ctx); err != nil {
				s.logger.Warn("wiki semantic warm incomplete", "error", err)
			} else {
				s.logger.Info("wiki semantic index warmed")
			}
			// Same eager warm for diary entries so semantic diary recall is ready
			// before the first turn (paraphrase recall over the day log).
			if err := store.WarmDiarySemantic(ctx); err != nil {
				s.logger.Warn("diary semantic warm incomplete", "error", err)
			} else {
				s.logger.Info("diary semantic index warmed")
			}
		})
	}

	// Phase 2: Tool deps + registration (core, plugin).
	s.initToolsAndDeps(&chatCfg, reg, transcriptStore, agentLogWriter)

	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = configresolve.LoadProviderConfigs(s.logger)

	// Wire deps that were previously Set*() after construction.
	// Most are available now; PluginHookRunner is late-bound in server.go
	// after plugin init (see SetPluginHookRunner call).
	chatCfg.ProviderRuntime = s.providerRuntime
	chatCfg.BroadcastRaw = streaming.BroadcastRawFunc(func(event string, data []byte) int {
		return s.broadcaster.BroadcastRaw(event, data)
	})
	if s.gatewaySubs != nil {
		chatCfg.EmitAgentFn = func(kind, sessionKey, runID string, payload map[string]any) {
			s.gatewaySubs.EmitAgent(events.AgentEvent{
				Kind:       kind,
				SessionKey: sessionKey,
				RunID:      runID,
				Payload:    payload,
			})
		}
		chatCfg.EmitTranscriptFn = func(sessionKey string, message any, messageID string) {
			s.gatewaySubs.EmitTranscript(events.TranscriptUpdate{
				SessionKey: sessionKey,
				Message:    message,
				MessageID:  messageID,
			})
		}
	}
	chatCfg.DreamTurnFn = func(ctx context.Context) {
		if s.autonomousSvc != nil {
			s.autonomousSvc.IncrementDreamTurn(ctx)
		}
	}
	// Preference capsules (신호:선호) put the wiki dreamer on its accelerated
	// cadence so a voiced standing preference reaches the 사용자 pages within
	// the hour instead of the regular 50-turn/8h batch window. Wired straight
	// to the dreamer — scheduling still runs through IncrementDreamTurn above.
	chatCfg.PreferenceSignalFn = func() {
		if s.wikiDreamer != nil {
			s.wikiDreamer.NotePreferenceSignal()
		}
	}
	// Deliverable → 작업 피드 auto safety net: when a turn analyzed a user document
	// but the model did not publish the result itself, file a doc_analysis card so
	// the deliverable reaches the feed. Wrapped so s.proactiveRelay (built below,
	// after the chat handler) is resolved at call time, not now.
	chatCfg.DeliverablePublisher = func(text string) (bool, error) {
		return s.proactiveRelay.PublishDeliverable(text)
	}
	chatCfg.RecordActivity = s.recordChatActivity

	s.chatHandler = chat.NewHandler(
		s.sessions,
		broadcastFn,
		s.logger,
		chatCfg,
	)

	// Wire server-level status data for /status command.
	s.chatHandler.SetStatusDepsFunc(func(sessionKey string) chat.StatusDeps {
		sd := chat.StatusDeps{
			Version:   s.version,
			StartedAt: s.startedAt,
		}
		if s.sessions != nil {
			sd.SessionCount = s.sessions.Count()
		}
		if sess := s.sessions.Get(sessionKey); sess != nil && sess.FailureReason != "" {
			sd.LastFailureReason = sess.FailureReason
		}
		return sd
	})

	// Wire file-edit checkpoint snapshots. Rooted under `<stateDir>/checkpoints`
	// so retention/disk-usage stays scoped to the Deneb state dir. Each run
	// gets a per-session Manager attached to its runCtx (see run_start.go).
	// Passing empty string would disable snapshots; always enabled when the
	// server has a resolved state dir.
	if denebDir := s.denebDir; denebDir != "" {
		cpRoot := filepath.Join(denebDir, "checkpoints")
		s.chatHandler.SetCheckpointRoot(cpRoot)
		// Release per-session checkpoint dirs immediately on terminal
		// lifecycle events instead of waiting for the 30-day startup GC.
		s.initCheckpointLifecycle(cpRoot)
	}

	// Wire SendFn after handler creation to avoid circular deps.
	sendFn := func(sessionKey, message string) error {
		fakeReq := &protocol.RequestFrame{
			ID:     shortid.New("tool_send"),
			Method: "sessions.send",
		}
		params := map[string]string{"key": sessionKey, "message": message}
		fakeReq.Params, _ = json.Marshal(params) // best-effort: marshal of known-good types cannot fail
		resp := s.chatHandler.SessionsSend(context.Background(), fakeReq)
		if resp != nil && resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		return nil
	}
	s.toolDeps.Sessions.SendFn = sendFn
	s.toolDeps.Chrono.SendFn = sendFn

	// Build the proactive-relay deps now that both dependencies
	// (native send function, transcript store) are available. Shared by the
	// cron handoff below, wiki dreaming in registerWorkflowSideEffects,
	// and gmail polling in initGmailPoll.
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

	// Wire transcript cloner for subagent cron session support.
	// The cached store satisfies cron.TranscriptCloner (CloneRecent), avoiding
	// a second uncached FileTranscriptStore that would bypass the TTL cache.
	if s.cronService != nil && transcriptStore != nil {
		s.cronService.SetTranscriptCloner(
			transcriptStore,
			"", // main session key resolved dynamically per-job
		)

		// Deliver cron analysis to the user without routing through the
		// LLM. The body is sent verbatim via the channel plugin and then
		// appended to the session transcript as an assistant message, so
		// a follow-up user turn ("더 자세히 알려줘") answers in a session
		// that knows what was just relayed.
		//
		// The previous implementation handed the body to the main agent
		// as a "relay this verbatim" directive and relied on the LLM to
		// comply. It didn't: the agent sometimes called wiki/memory tools
		// and replied with a terse action report ("위키 업데이트 완료")
		// instead of the body, leaving the user without the content.
		// Moving delivery out of the LLM's control fixes this class of
		// deviation structurally — no prompt-level instruction to obey.
		//
		// The native relay mirrors every proactive report into client:main.
		// If the transcript relay is not wired, decline the handoff and let
		// cron fall back to its own delivery accounting path.
		s.cronService.SetMainSessionHandoff(func(ctx context.Context, channel, to, jobID, analysis string) (bool, error) {
			if to == "" || strings.TrimSpace(analysis) == "" {
				return false, nil
			}
			sessionKey := channel + ":" + to
			// Mail analyses (email-single-analysis per kakao-watch trigger,
			// email-analysis-full daily batch) arrive as collapsed title-only
			// cards so each mail is one tap-to-expand row in the 업무 chat
			// instead of a wall of prose. Other jobs (morning letter, weekly
			// report) keep plain delivery.
			relayFn := s.proactiveRelay.Relay
			if strings.HasPrefix(jobID, "email-") {
				relayFn = s.proactiveRelay.RelayCollapsed
			}
			delivered, err := relayFn(ctx, sessionKey, analysis)
			if err != nil {
				s.logger.Error("cron proactive relay failed",
					"jobId", jobID, "sessionKey", sessionKey, "error", err)
				return false, err
			}
			if delivered {
				// Include a preview head so a postmortem can tell at a glance
				// whether the delivered body looks like the analysis (starts
				// with 📬 / 🔴 markers) or a stray wrap-up ("위키 업데이트
				// 완료"). 120 chars is enough to spot the difference without
				// bloating the log.
				preview := analysis
				if len(preview) > 120 {
					preview = preview[:120] + "…"
				}
				s.logger.Info("cron proactive relay delivered",
					"jobId", jobID,
					"sessionKey", sessionKey,
					"bytes", len(analysis),
					"preview", preview)
			}
			return delivered, nil
		})
	}

	// Wire transcript loader for subagent /log command.
	if s.acpDeps != nil && transcriptStore != nil {
		s.acpDeps.TranscriptLoader = func(sessionKey string, limit int) ([]string, []string, error) {
			msgs, _, err := transcriptStore.Load(sessionKey, limit)
			if err != nil {
				return nil, nil, err
			}
			roles := make([]string, len(msgs))
			contents := make([]string, len(msgs))
			for i, m := range msgs {
				roles[i] = m.Role
				contents[i] = m.TextContent()
			}
			return roles, contents, nil
		}
	}

	// Inject subagent completion results into parent session transcripts.
	// When a subagent finishes, its output is appended as a system note to
	// the parent session so the LLM sees what the subagent produced.
	if s.acpDeps != nil && transcriptStore != nil {
		projector := acp.NewACPProjector(s.acpDeps.Registry)
		s.acpResultInjectionUnsub = acp.StartSubagentResultInjection(acp.ResultInjectionDeps{
			Registry:  s.acpDeps.Registry,
			Projector: projector,
			Sessions:  s.sessions,
			Transcript: acp.TranscriptAppendFunc(func(sessionKey, text string) error {
				msg := chat.NewTextChatMessage("system", text, 0)
				return transcriptStore.Append(sessionKey, msg)
			}),
			Logger: s.logger,
		})
	}

	// Chat, BTW, miniapp-chat bridge, Exec, Wiki, Genesis, and GmailAnalyze are
	// registered in registerLateMethods() after this function returns; Aurora
	// (dreaming) is wired later in registerWorkflowSideEffects().
}
