package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
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
			}
		}
		// Fallback: if Polaris initialization failed, use cached store directly.
		// Assembly will return an error but the gateway can still serve other functions.
		if transcriptStore == nil {
			transcriptStore = cached
		}
	}

	// Initialize agent detail log writer.
	var agentLogWriter *agentlog.Writer
	if home, err := os.UserHomeDir(); err == nil {
		agentLogWriter = agentlog.NewWriter(home + "/.deneb/agent-logs")
	}

	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	s.genesisTranscripts = transcriptStore // share with genesis for session context loading
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter

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
		s.chatHandler.SetCheckpointRoot(filepath.Join(denebDir, "checkpoints"))
	}

	// Wire /insights command — builds a MarkdownV2 usage report using the
	// insights engine (created during registerEarlyMethods via hub.SetInsights).
	// Nil-safe: if the engine isn't available the dispatcher replies with a
	// friendly disabled-notice.
	if engine := s.insightsEngine(); engine != nil {
		s.chatHandler.SetInsightsProviderFunc(func(ctx context.Context, days int) (string, error) {
			rep, err := engine.Generate(ctx, days)
			if err != nil {
				return "", err
			}
			return insights.RenderMarkdownV2(rep), nil
		})
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
	// (telegram plugin, transcript store) are available. Shared by the
	// cron handoff below, wiki dreaming in registerWorkflowSideEffects,
	// and gmail polling in initGmailPoll.
	s.proactiveRelay = proactiveRelayDeps{
		telegramPlug:    s.telegramPlug,
		transcriptStore: transcriptStore,
		logger:          s.logger,
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
		// Channels without a wired plugin (non-telegram, plugin not yet
		// connected) decline the handoff and cron falls back to its own
		// direct delivery path so the user still receives the message.
		s.cronService.SetMainSessionHandoff(func(ctx context.Context, channel, to, jobID, analysis string) (bool, error) {
			if to == "" || strings.TrimSpace(analysis) == "" {
				return false, nil
			}
			sessionKey := channel + ":" + to
			delivered, err := s.proactiveRelay.relay(ctx, sessionKey, analysis)
			if err != nil {
				s.logger.Error("cron proactive relay failed",
					"jobId", jobID, "sessionKey", sessionKey, "error", err)
				return false, err
			}
			if delivered {
				s.logger.Info("cron proactive relay delivered",
					"jobId", jobID, "sessionKey", sessionKey, "bytes", len(analysis))
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

	// Chat, BTW, Exec, Aurora, cron wiring, and Telegram pipeline are
	// registered in registerLateMethods() after this function returns.
}

// registerWorkflowSideEffects wires non-RPC business logic: process approval
// callbacks, autonomous/dreaming service, Telegram notifiers, and memory flush.
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

	// Wire wiki dreamer for autonomous diary → wiki consolidation.
	if s.wikiDreamer != nil {
		s.autonomousSvc.SetDreamer(s.wikiDreamer)
	}

	// Broadcast dreaming events to WebSocket clients.
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		hub.Broadcast("dreaming.cycle", event)
	})

	// Wire proactive relay as the dreaming notifier. Going through the
	// relay (vs. a plain telegram send) mirrors the body into the user's
	// session transcript, so a follow-up message after a dream
	// completion ("방금 뭔 얘기야?") is answered in a session that knows
	// what was just delivered.
	if s.telegramPlug != nil {
		if tgCfg := s.telegramPlug.Config(); tgCfg != nil && tgCfg.ChatID != 0 {
			sessionKey := "telegram:" + strconv.FormatInt(tgCfg.ChatID, 10)
			if n := s.proactiveRelay.notifierForSession(sessionKey); n != nil {
				s.autonomousSvc.SetNotifier(n)
			}
		}
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

		// Register heartbeat task: every 5 minutes during active hours
		// (08:00–23:00 Asia/Seoul), checks ~/.deneb/HEARTBEAT.md for
		// user-defined tasks and executes them autonomously.
		s.autonomousSvc.RegisterTask(&heartbeatTask{
			chatHandler: s.chatHandler,
			activity:    s.activity,
			logger:      s.logger,
			homeDir:     homeDir,
		})
	}

	// Skill Genesis: register autonomous tasks (services created in initGenesisServices).
	s.registerGenesisAutonomousTasks(hub)

	// Gmail polling service: periodic new-email analysis via LLM.
	s.initGmailPoll()

}
