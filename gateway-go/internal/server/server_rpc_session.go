package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/chat"
	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/platform"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/session"
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
		s.logger.Info("agent detail log initialized", "dir", home+"/.deneb/agent-logs")
	}

	chatCfg := chat.DefaultHandlerConfig()
	chatCfg.Transcript = transcriptStore
	chatCfg.Tools = chat.NewToolRegistry()
	chatCfg.JobTracker = s.jobTracker
	chatCfg.AgentLog = agentLogWriter

	// Phase 1: Memory subsystem (unified store, Aurora, memory, embedder, reranker).
	var reg modelrole.Registry
	s.initMemorySubsystem(&chatCfg, &reg)

	// Phase 2: Tool deps + registration (core, plugin, autoresearch).
	s.initToolsAndDeps(&chatCfg, &reg, transcriptStore, agentLogWriter)

	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = loadProviderConfigs(s.logger)

	// Wire deps that were previously Set*() after construction.
	// All are available now, so pass via HandlerConfig for cleaner init.
	chatCfg.ProviderRuntime = s.providerRuntime
	chatCfg.PluginHookRunner = s.pluginTypedHookRunner
	chatCfg.HookRegistry = s.hooks
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
	s.dispatcher.RegisterDomain(handlerchat.Methods(handlerchat.Deps{Chat: s.chatHandler}))

	// Wire agent runner to cron service so scheduled jobs can execute agent turns.
	if s.cronService != nil {
		s.cronService.SetAgentRunner(&cronChatAdapter{chat: s.chatHandler})
	}

	// Side-question (/btw) method — routes through chat handler natively.
	s.dispatcher.RegisterDomain(handlerchat.BtwMethods(handlerchat.BtwDeps{
		Chat:        s.chatHandler,
		Broadcaster: broadcastFn,
	}))

	// Native session execution / agent methods (Phase 4).
	s.dispatcher.RegisterDomain(handlersession.ExecMethods(handlersession.ExecDeps{
		Chat:       s.chatHandler,
		Agents:     s.agents,
		JobTracker: s.jobTracker,
	}))
}

// registerApprovalAgentMethods registers exec approval, agent lifecycle, talk, wizard,
// and autonomous dreaming methods.
func (s *Server) registerApprovalAgentMethods(broadcastFn func(string, any) (int, []error)) {
	s.dispatcher.RegisterDomain(handlerprocess.ApprovalMethods(handlerprocess.ApprovalDeps{
		Store:       s.approvals,
		Broadcaster: broadcastFn,
	}))

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
			broadcastFn("exec.approval.requested", map[string]any{
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

	s.dispatcher.RegisterDomain(handleragent.CRUDMethods(handleragent.AgentsDeps{
		Agents:      s.agents,
		Broadcaster: broadcastFn,
	}))

	s.dispatcher.RegisterDomain(handlerplatform.WizardMethods(handlerplatform.WizardDeps{
		Engine: s.wizardEng,
	}))

	s.dispatcher.RegisterDomain(handlerplatform.TalkMethods(handlerplatform.TalkDeps{
		Talk: s.talkState,
	}))

	// AuroraDream: memory consolidation service (dreaming-only, no goal cycles).
	s.autonomousSvc = autonomous.NewService(s.logger)

	// Wire AuroraDream adapter if available (created during chat handler init).
	if s.dreamingAdapter != nil {
		s.autonomousSvc.SetDreamer(s.dreamingAdapter)
	}

	// Broadcast dreaming events to WebSocket clients.
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		broadcastFn("dreaming.cycle", event)
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

	// Register periodic memory flush task: appends high-importance facts
	// to date-stamped markdown files (~/.deneb/memory/YYYY-MM-DD.md).
	if s.memoryStore != nil {
		denebDir := ""
		if home, err := os.UserHomeDir(); err == nil {
			denebDir = filepath.Join(home, ".deneb")
		}
		if denebDir != "" {
			s.autonomousSvc.RegisterTask(&memoryFlushTask{
				store:    s.memoryStore,
				dir:      denebDir,
				timezone: "Asia/Seoul",
				logger:   s.logger,
			})
			s.logger.Info("memory flush task registered with autonomous service")
		}
	}

	// Register diary heartbeat task: every 2 hours, the main LLM writes
	// a detailed narrative diary entry (memory/diary/diary-YYYY-MM-DD.md).
	if s.chatHandler != nil {
		s.autonomousSvc.RegisterTask(&diaryHeartbeatTask{
			chatHandler: s.chatHandler,
			logger:      s.logger,
		})
		s.logger.Info("diary heartbeat task registered with autonomous service (2h interval)")
	}
}
