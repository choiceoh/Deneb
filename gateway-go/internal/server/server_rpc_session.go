package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/session"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// registerSessionRPCMethods registers session state, repair, daemon status, and
// the full chat handler pipeline (init + all chat/session-exec RPC registrations).
func (s *Server) registerSessionRPCMethods() {
	// Session state methods (patch/reset/preview/resolve/compact).
	var sessionCompressor *transcript.Compressor
	if s.transcript != nil {
		sessionCompressor = transcript.NewCompressor(transcript.DefaultCompactionConfig(), s.logger)
	}
	sessionDeps := handlersession.Deps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
		Transcripts: s.transcript,
		Compressor:  sessionCompressor,
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
					if connID := s.broadcaster.GetToolEventRecipient(runID); connID != "" {
						return s.broadcaster.BroadcastToConnIDs(event, payload, map[string]bool{connID: true})
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
		transcriptStore = chat.NewCachedTranscriptStore(
			chat.NewFileTranscriptStore(transcriptDir), 0)
	}

	// Initialize agent detail log writer.
	var agentLogWriter *agentlog.Writer
	if home, err := os.UserHomeDir(); err == nil {
		agentLogWriter = agentlog.NewWriter(home + "/.deneb/agent-logs")
	}

	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter

	// Phase 1: Memory subsystem (unified store, Aurora, memory, wiki).
	var reg *modelrole.Registry
	s.initMemorySubsystem(&chatCfg, &reg)

	// Create centralized local AI hub now that the model registry is available.
	s.localAIHub = localai.New(localai.Config{
		CJKBlockFile: firstEnv("LOCAL_AI_CJK_BLOCK_FILE", "SGLANG_CJK_BLOCK_FILE"),
	}, reg, s.logger)
	chatCfg.LocalAIHub = s.localAIHub

	// Phase 2: Tool deps + registration (core, plugin, autoresearch).
	s.initToolsAndDeps(&chatCfg, reg, transcriptStore, agentLogWriter)

	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = loadProviderConfigs(s.logger)

	// Wire deps that were previously Set*() after construction.
	// Most are available now; PluginHookRunner is late-bound in server.go
	// after plugin init (see SetPluginHookRunner call).
	chatCfg.ProviderRuntime = s.providerRuntime
	chatCfg.InternalHookRegistry = s.internalHooks
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
			Version:       s.version,
			StartedAt:     s.startedAt,
			RustFFI:       false,
			WSConnections: s.clientCnt.Load(),
		}
		if s.sessions != nil {
			sd.SessionCount = s.sessions.Count()
		}
		if sess := s.sessions.Get(sessionKey); sess != nil && sess.FailureReason != "" {
			sd.LastFailureReason = sess.FailureReason
		}
		return sd
	})

	// Wire SendFn after handler creation to avoid circular deps.
	sendFn := func(sessionKey, message string) error {
		fakeReq := &protocol.RequestFrame{
			ID:     shortid.New("tool_send"),
			Method: "sessions.send",
		}
		params := map[string]string{"key": sessionKey, "message": message}
		fakeReq.Params, _ = json.Marshal(params)
		resp := s.chatHandler.SessionsSend(context.Background(), fakeReq)
		if resp != nil && resp.Error != nil {
			return errors.New(resp.Error.Message)
		}
		return nil
	}
	s.toolDeps.Sessions.SendFn = sendFn
	s.toolDeps.Chrono.SendFn = sendFn

	// Wire autoresearch workdir resolver so /chart finds the experiment dir.
	if s.autoresearchRunner != nil {
		s.chatHandler.SetAutoresearchWorkdirFn(s.autoresearchRunner.Workdir)
	}

	// Wire transcript cloner for subagent cron session support.
	// The cached store satisfies cron.TranscriptCloner (CloneRecent), avoiding
	// a second uncached FileTranscriptStore that would bypass the TTL cache.
	if s.cronService != nil && transcriptStore != nil {
		s.cronService.SetTranscriptCloner(
			transcriptStore,
			"", // main session key resolved dynamically per-job
		)
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
// All RPC domain registrations (approval, agent CRUD, wizard, talk) are now
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

	// Wire Telegram notifier for dreaming and autoresearch events.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			notifier := &telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			}
			s.autonomousSvc.SetNotifier(notifier)
			if s.autoresearchRunner != nil {
				s.autoresearchRunner.SetNotifier(notifier)
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

		// Register heartbeat task: every 3 minutes, checks ~/.deneb/HEARTBEAT.md
		// for user-defined tasks and executes them autonomously.
		s.autonomousSvc.RegisterTask(&heartbeatTask{
			chatHandler: s.chatHandler,
			activity:    s.activity,
			logger:      s.logger,
			homeDir:     homeDir,
		})
	}

	// Register diary heartbeat task: every 2 hours, the main LLM writes
	// a detailed narrative diary entry (memory/diary/diary-YYYY-MM-DD.md).
	if s.chatHandler != nil {
		s.autonomousSvc.RegisterTask(&diaryHeartbeatTask{
			chatHandler: s.chatHandler,
			activity:    s.activity,
			logger:      s.logger,
		})

	}

	// Skill Genesis: register autonomous tasks (services created in initGenesisServices).
	s.registerGenesisAutonomousTasks(hub)

	// RL self-learning: wire session trajectory collection hook.
	s.registerRLSideEffects(hub)

	// Gmail polling service: periodic new-email analysis via LLM.
	s.initGmailPoll()

}

// firstEnv returns the first non-empty environment variable value.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
