package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modeltuner"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/regressionwatch"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compactuner"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
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
			chat.NewFileTranscriptStore(transcriptDir), 0)

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
	chatCfg.TopicResolver = newTopicResolver(s.logger)

	// Phase 1: Memory subsystem (unified store, Aurora, memory, wiki).
	var reg *modelrole.Registry
	s.initMemorySubsystem(&chatCfg, &reg)

	// Create centralized local AI hub now that the model registry is available.
	s.localAIHub = localai.New(localai.Config{}, reg, s.logger)
	chatCfg.LocalAIHub = s.localAIHub

	// Create BGE-M3 embedding client for MMR compaction fallback.
	// Starts background health probing; gracefully degrades if server is unavailable.
	s.embeddingClient = embedding.New("", s.logger)
	chatCfg.EmbeddingClient = s.embeddingClient

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
		})
	}

	// Phase 2: Tool deps + registration (core, plugin).
	s.initToolsAndDeps(&chatCfg, reg, transcriptStore, agentLogWriter)

	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = loadProviderConfigs(s.logger)

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
	s.proactiveRelay = proactiveRelayDeps{
		transcriptStore: transcriptStore,
		logger:          s.logger,
		pushHub:         s.pushHub,
		pushFCM:         s.pushNotifier,
		workFeed:        s.nativeWorkFeedStore(),
		nativeSync:      s.nativeSyncStore,
		behaviorLog:     agentLogWriter,
		sessions:        s.sessions,
		cardTitler:      s.cardTitleSummary,
		workModel:       s.resolveFeedWorkModel,
	}

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
			relayFn := s.proactiveRelay.relay
			if strings.HasPrefix(jobID, "email-") {
				relayFn = s.proactiveRelay.relayCollapsed
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

	// Chat, BTW, Exec, Aurora, and cron wiring are registered in
	// registerLateMethods() after this function returns.
}

// resolveFeedWorkModel returns the display name of the model behind proactive
// 업무 feed reports — the main agent-turn model. Cron morning letter, mail
// analysis synthesis, heartbeat, goal, and event ingest all run as main-role
// turns, so the main model is the "작업 모델" for the feed. Returns "" when the
// model registry is unwired (older tests), which leaves the feed footer off.
func (s *Server) resolveFeedWorkModel() string {
	if s.modelRegistry == nil {
		return ""
	}
	return s.modelRegistry.Model(modelrole.RoleMain)
}

// registerWorkflowSideEffects wires non-RPC business logic: process approval
// callbacks, autonomous/dreaming service, native notifiers, and memory flush.
// All RPC domain registrations (approval, agent CRUD) are now
// handled by registerEarlyMethods via hub adapters.
func (s *Server) registerWorkflowSideEffects(hub *rpcutil.GatewayHub) {
	// Wire process approval callback using the Go approval store directly.
	// When a tool execution requires approval, create an approval request,
	// broadcast it to WS clients, and wait for a decision.
	if s.processes != nil {
		s.processes.SetApprover(func(req process.ExecRequest) bool {
			ar := s.approvals.CreateRequest(approval.CreateRequestParams{
				Command:     req.Command,
				CommandArgv: req.Args,
				Cwd:         req.WorkingDir,
			})
			hub.Broadcast("exec.approval.requested", map[string]any{
				"id":      ar.ID,
				"command": req.Command,
				"args":    req.Args,
			})
			// Wait for decision with timeout.
			waitCh := s.approvals.WaitForDecision(ar.ID)
			timer := time.NewTimer(30 * time.Second)
			defer timer.Stop()
			select {
			case <-waitCh:
				resolved := s.approvals.Get(ar.ID)
				if resolved != nil && resolved.Decision != nil {
					return *resolved.Decision == approval.DecisionAllowOnce || *resolved.Decision == approval.DecisionAllowAlways
				}
				return false
			case <-timer.C:
				return false
			}
		})
	}

	// AuroraDream: memory consolidation service (dreaming-only, no goal cycles).
	s.autonomousSvc = autonomous.NewService(s.logger)
	s.autonomousSvc.SetBehaviorLog(s.agentLogWriter)

	// Persist task last-run times under ~/.deneb so periodic intervals survive
	// the frequent auto-deploy SIGUSR1 restarts. Without this, every restart
	// re-runs all tasks 30s in, defeating 24h (boot) and weekly (evolution)
	// schedules.
	if home, err := os.UserHomeDir(); err == nil {
		s.autonomousSvc.SetStateDir(filepath.Join(home, ".deneb"))
	}

	// Persist per-session frozen system-prompt snapshots (tier-1 wiki, context
	// files, topic knowledge) so the same SIGUSR1 restarts don't force a
	// per-session vLLM APC re-prefill: the restored snapshot reproduces the
	// system prompt byte-for-byte, keeping the engine's KV cache for the tool
	// schemas + history valid. DENEB_STATE_DIR-aware so a dev gateway never
	// writes into the production file. Loaded in restoreAndWakeSessions's
	// goroutine (server_lifecycle.go). See chat/prompt_snapshot_persist.go.
	chat.ConfigurePromptSnapshots(config.ResolveStateDir(), s.logger)

	// Wire wiki dreamer for autonomous diary → wiki consolidation.
	if s.wikiDreamer != nil {
		s.autonomousSvc.SetDreamer(s.wikiDreamer)
	}

	// Broadcast dreaming events to WebSocket clients.
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		hub.Broadcast("dreaming.cycle", event)
	})

	// Wire the proactive relay as the dreaming notifier, bound to a dedicated
	// client:main:dream sub-session. Aurora Dream lifecycle messages (often "변경
	// 없음" or diagnostics) land in their own native conversation instead of the
	// primary 업무 feed (client:main); a follow-up like "방금 뭔 얘기야?" opened there is
	// still answered with the dream delivery in view.
	if n := s.proactiveRelay.notifierForSession(dreamWorkSessionKey); n != nil {
		s.autonomousSvc.SetNotifier(n)
	}

	// Register boot task: on startup (and daily thereafter), runs a full
	// agent turn using ~/.deneb/BOOT.md content for proactive initialization.
	if s.chatHandler != nil {
		homeDir := ""
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
		s.autonomousSvc.RegisterTask(&bootTask{
			chatHandler: s.chatHandler,
			activity:    s.activity,
			logger:      s.logger,
			homeDir:     homeDir,
		})

		// Register heartbeat task: every 30 minutes during active hours
		// (08:00–23:00 Asia/Seoul), checks ~/.deneb/HEARTBEAT.md for
		// user-defined tasks and executes them autonomously.
		s.autonomousSvc.RegisterTask(&heartbeatTask{
			chatHandler: s.chatHandler,
			activity:    s.activity,
			logger:      s.logger,
			homeDir:     homeDir,
			// Proactive signal augmentation: calendar conflicts / imminent
			// meetings plus due to-dos (dream-captured open loops included)
			// surface into the heartbeat turn. Best-effort; no-op when the
			// sources are absent. See heartbeat_signals.go / open_loop_sink.go.
			collectSignals: combineSignalCollectors(
				newCalendarSignalCollector(resolveBriefingCalendarClient),
				newTodoDeadlineCollector(),
				newDealDeadlineSignalCollector(func() *wiki.Store { return s.wikiStore }),
			),
			signalConfig: autonomous.DefaultSignalConfig(),
		})

		// Register the goal loop (Ralph loop): advances active standing goals
		// (set via /goal) one run at a time while the user is idle, judging
		// completion with the lightweight model and enforcing a per-goal
		// idempotency ledger so a re-driven run never repeats a destructive
		// action. State persists in ~/.deneb/goals.json (beside
		// autonomous_state.json) so a standing goal survives the auto-deploy
		// restarts. The store singleton is shared with the /goal slash command.
		goalStateDir := ""
		if homeDir != "" {
			goalStateDir = filepath.Join(homeDir, ".deneb")
		}
		goalStore := goals.NewStore(goalStateDir, s.logger)
		goals.SetDefault(goalStore)
		s.autonomousSvc.RegisterTask(&goalTask{
			chatHandler: s.chatHandler,
			store:       goalStore,
			activity:    s.activity,
			logger:      s.logger,
			notify: func(ctx context.Context, sessionKey, msg string) error {
				n := s.proactiveRelay.notifierForSession(sessionKey)
				if n == nil {
					return nil
				}
				return n.Notify(ctx, msg)
			},
		})

		// Daily offsite memory backup: tar.gz of the memory stores streamed
		// over ssh to the storage node (the NFS mount is read-only from this
		// host). Only registered for the production state dir — dev live-test
		// instances (DENEB_STATE_DIR=/tmp/...) must not ship archives.
		s.registerMemoryBackupTask(homeDir)

		// Project-wiki deep research: every 6h, pick one 프로젝트 page and run an
		// agent turn that re-investigates it from Deneb's own internal sources
		// (mail archive, polaris recall, knowledge graph, contacts, linked wiki
		// pages) and updates it in place. Internal-only (no web), silent, and
		// round-robin across project pages. Production state dir only — see
		// registerWikiResearchTask.
		s.registerWikiResearchTask(homeDir)

		// Model tuner: every 6h, aggregate the last 24h of agent logs by
		// model, auto-apply the bounded output-token floor for models that
		// keep hitting the ceiling, and calibrate newly served vLLM models.
		// Recommendations (stalls / cache breaks / slow tails) are not pushed
		// as a notification — they surface under the native model picker
		// (miniapp_models AdvisoryLines/NoteFor, read from the scorecard), so
		// the tuner runs silently. Scorecard: ~/.deneb/model-stats.json.
		if s.modelRegistry != nil && s.agentLogWriter != nil {
			s.autonomousSvc.RegisterTask(modeltuner.NewTask(modeltuner.Deps{
				Logs:     s.agentLogWriter,
				Registry: s.modelRegistry,
				// DENEB_STATE_DIR-aware so a dev gateway's tuner never writes
				// into the production ~/.deneb scorecard.
				StatePath: modeltuner.DefaultStatePath(),
				Logger:    s.logger,
			}))

			// Regression watch (Stage 1 of the autoresearch cold-start trigger,
			// OBSERVE-ONLY): every 6h, sample operational telemetry (agentlog rates,
			// model-health circuit, error-log spikes — see regressionSources),
			// compare to a rolling baseline, and log regressions. It does NOT create
			// optimization goals yet — that path is gated until these thresholds are
			// validated against real traffic. Baseline: ~/.deneb/regression-baseline.json.
			s.autonomousSvc.RegisterTask(regressionwatch.NewTask(regressionwatch.Deps{
				Sources:   s.regressionSources(),
				StatePath: regressionwatch.DefaultStatePath(),
				Logger:    s.logger,
			}))

			// Compaction tuner (opt-in): refine the summarizer's preservation
			// guidelines from recent summaries' vagueness. Off by default — it
			// auto-edits a prompt, so it ships behind a flag.
			if os.Getenv("DENEB_COMPACTION_TUNER") == "1" && s.polarisStore != nil && s.modelRegistry != nil {
				if lw := s.modelRegistry.Client(modelrole.RoleLightweight); lw != nil {
					var compactNotify func(ctx context.Context, msg string) error
					if n := s.proactiveRelay.notifierForSession(nativeWorkSessionKey); n != nil {
						compactNotify = n.Notify
					}
					s.compactTuner = compactuner.NewTask(compactuner.Deps{
						Summaries:  s.polarisStore,
						Guidelines: compaction.NewGuidelineStore(filepath.Join(config.ResolveStateDir(), compaction.GuidelineFileName)),
						Client:     lw,
						Model:      s.modelRegistry.Model(modelrole.RoleLightweight),
						Notify:     compactNotify,
						Logger:     s.logger,
					})
					s.autonomousSvc.RegisterTask(s.compactTuner)
					s.logger.Info("compaction-tuner: enabled (DENEB_COMPACTION_TUNER)")
				}
			}
		}
	}

	// File semantic index: background reindex task (15 min, plus a first cycle
	// shortly after boot via the service's initial-grace). Keeps the BGE-M3 vector
	// index over the file store fresh so semantic file search finds new/changed
	// files. No-op when the index/store/embedding client isn't wired.
	s.registerFileSemindexTask()

	// Propus: register autonomous tasks (services created in initGenesisServices).
	s.registerGenesisAutonomousTasks(hub)

	// Gmail polling service: periodic new-email analysis via LLM.
	// Load deneb.json once and share the snapshot across the poll initializers.
	cfgSnap, _ := config.LoadConfigFromDefaultPath()
	s.initGmailPoll(cfgSnap)
	s.initLMTPServer(cfgSnap)

	// Calendar briefing service: D-15min push for upcoming meetings.
	// Delivers to the native client (업무 transcript + live push) via the
	// shared proactive relay. Returns nil and is a no-op when calendar OAuth
	// isn't configured, so safe to wire unconditionally.
	s.calendarBriefing = newCalendarBriefingService(
		func(text string) (bool, error) { return s.proactiveRelay.relayNative(text) },
		resolveBriefingCalendarClient,
		s.logger,
	)
	if s.calendarBriefing != nil {
		// Best-effort context enrichment: per-attendee mail freshness + wiki
		// notes, topic-related recent mail, and a past wiki record. Backed by
		// gmail.DefaultClient and the late-bound wiki store; both degrade to
		// the base D-15 reminder when unavailable. wikiStore is read at call
		// time (set during the session phase) so construction order can't
		// capture a nil store.
		s.calendarBriefing.enricher = newBriefingEnricher(
			func() *wiki.Store { return s.wikiStore },
			s.logger,
		)
	}
	s.calendarBriefing.start(s.ShutdownCtx())

	// Periodic auth/liveness probe for role-assigned cloud providers — the
	// fallback role gets no organic traffic, so a dead credential there is
	// invisible until the safety net is actually needed (role_health_watch.go).
	s.startRoleHealthWatch()
}
