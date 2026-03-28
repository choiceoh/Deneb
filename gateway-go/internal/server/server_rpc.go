package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func (s *Server) registerExtendedMethods() {
	// ACP RPC methods.
	rpc.RegisterACPMethods(s.dispatcher, s.acpDeps)

	rpc.RegisterExtendedMethods(s.dispatcher, rpc.ExtendedDeps{
		Sessions:    s.sessions,
		Channels:    s.channels,
		GatewaySubs: s.gatewaySubs,
		Processes:   s.processes,
		Cron:        s.cron,
		Hooks:       s.hooks,
		Broadcaster: s.broadcaster,
	})

	// Provider methods.
	rpc.RegisterProviderMethods(s.dispatcher, rpc.ProviderDeps{
		Providers:   s.providers,
		AuthManager: s.authManager,
	})

	// Tool methods.
	rpc.RegisterToolMethods(s.dispatcher, rpc.ToolDeps{
		Processes: s.processes,
	})

	// Session state methods (patch/reset/preview/resolve/compact).
	var sessionCompressor *transcript.Compressor
	if s.transcript != nil {
		sessionCompressor = transcript.NewCompressor(transcript.DefaultCompactionConfig(), s.logger)
	}
	sessionDeps := rpc.SessionDeps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
		Transcripts: s.transcript,
		Compressor:  sessionCompressor,
	}
	rpc.RegisterSessionMethods(s.dispatcher, sessionDeps)

	// Session repair and overflow check methods.
	rpc.RegisterSessionRepairMethods(s.dispatcher, sessionDeps)

	// Daemon status method.
	s.dispatcher.Register("daemon.status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.daemon == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]string{"state": "not_configured"})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, s.daemon.Status())
		return resp
	})

	// Aurora channel methods (desktop app communication).
	rpc.RegisterAuroraChannelMethods(s.dispatcher, rpc.AuroraChannelDeps{
		Chat: s.chatHandler,
	})

	// Event broadcasting method.
	s.dispatcher.Register("events.broadcast", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		sent, _ := s.broadcaster.Broadcast(p.Event, p.Payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})
}

// SetDaemon sets the daemon manager for lifecycle control.

func (s *Server) registerPhase2Methods() {
	// Chat methods — native agent execution.
	broadcastFn := func(event string, payload any) (int, []error) {
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

	// Initialize Aurora compaction store.
	auroraStore, err := aurora.NewStore(aurora.DefaultStoreConfig(), s.logger)
	if err != nil {
		s.logger.Warn("aurora store unavailable, compaction will use legacy fallback", "error", err)
	} else {
		chatCfg.AuroraStore = auroraStore
		s.logger.Info("aurora compaction store initialized")
	}

	// Initialize structured memory store (Honcho-style).
	if home, err := os.UserHomeDir(); err == nil {
		dbPath := filepath.Join(home, ".deneb", "memory.db")
		memStore, err := memory.NewStore(dbPath)
		if err != nil {
			s.logger.Warn("memory store unavailable", "error", err)
		} else {
			chatCfg.MemoryStore = memStore

			const sglangURL = "http://127.0.0.1:30000/v1"
			const sglangModel = "Qwen/Qwen3.5-35B-A3B"

			// Use the Gemini embedder set during gateway init.
			if s.geminiEmbedder != nil {
				embedder := memory.NewEmbedder(s.geminiEmbedder, memStore, s.logger)
				chatCfg.MemoryEmbedder = embedder

				sglangClient := llm.NewClient(sglangURL, "", llm.WithLogger(s.logger))
				s.dreamingAdapter = memory.NewDreamingAdapter(memStore, embedder, sglangClient, sglangModel, s.logger)
				// DreamTurnFn is wired after autonomous service is created (phase 3).
				// Use a closure that captures s so the autonomous svc reference resolves at call time.
				chatCfg.DreamTurnFn = func(ctx context.Context) {
					if svc := s.autonomousSvc; svc != nil {
						svc.IncrementDreamTurn(ctx)
					}
				}
			} else {
				s.logger.Info("aurora-memory: embedding disabled (GEMINI_API_KEY not set)")
			}

			// Wire cross-encoder reranker if Jina API key is configured.
			if s.jinaAPIKey != "" {
				reranker := vega.NewReranker(vega.RerankConfig{
					APIKey: s.jinaAPIKey,
					Logger: s.logger,
				})
				if reranker != nil {
					memStore.SetReranker(func(ctx context.Context, query string, docs []string, topN int) ([]memory.RerankResult, error) {
						vegaResults, err := reranker.Rerank(ctx, query, docs, topN)
						if err != nil {
							return nil, err
						}
						results := make([]memory.RerankResult, len(vegaResults))
						for i, r := range vegaResults {
							results[i] = memory.RerankResult{Index: r.Index, RelevanceScore: r.RelevanceScore}
						}
						return results, nil
					})
					s.logger.Info("aurora-memory: cross-encoder reranking enabled (Jina)")
				}
			}

			// Auto-migrate existing MEMORY.md on first run.
			count, _ := memStore.ActiveFactCount(context.Background())
			if count == 0 {
				memoryMdPath := filepath.Join(home, ".deneb", "MEMORY.md")
				if imported, err := memStore.ImportFromMarkdown(context.Background(), memoryMdPath); err == nil && imported > 0 {
					s.logger.Info("aurora-memory: imported legacy MEMORY.md", "facts", imported)
				}
			}

			s.logger.Info("aurora-memory: structured store initialized", "db", dbPath)
		}
	}

	// Resolve default model from config; fall back to hardcoded default.
	chatCfg.DefaultModel = resolveDefaultModel(s.logger)

	// Resolve workspace directory for file tool operations.
	workspaceDir := resolveWorkspaceDir()
	s.logger.Info("resolved agent workspace directory", "workspaceDir", workspaceDir)

	// Build core tool dependencies. Stored on the server so later init phases
	// can late-bind fields.
	s.toolDeps = &chat.CoreToolDeps{
		ProcessMgr:   s.processes,
		WorkspaceDir: workspaceDir,
		CronSched:    s.cron,
		Sessions:     s.sessions,
		LLMClient:    chatCfg.LLMClient,
		Transcript:   transcriptStore,
		AgentLog:     agentLogWriter,
		MemoryStore:  chatCfg.MemoryStore,
	}

	// Register core tools (file I/O, exec, process, sessions, gateway, cron, image).
	chat.RegisterCoreTools(chatCfg.Tools, s.toolDeps)
	if s.authManager != nil {
		chatCfg.AuthManager = s.authManager
	}
	chatCfg.ProviderConfigs = loadProviderConfigs(s.logger)

	s.chatHandler = chat.NewHandler(
		s.sessions,
		broadcastFn,
		s.logger,
		chatCfg,
	)

	// Wire SessionSendFn after handler creation to avoid circular deps.
	s.toolDeps.SessionSendFn = func(sessionKey, message string) error {
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
	rpc.RegisterChatMethods(s.dispatcher, rpc.ChatDeps{Chat: s.chatHandler})

	// Wire raw broadcast directly to chat handler for streaming event relay.
	s.chatHandler.SetBroadcastRaw(func(event string, data []byte) int {
		return s.broadcaster.BroadcastRaw(event, data)
	})

	// Side-question (/btw) method — routes through chat handler natively.
	rpc.RegisterChatBtwMethods(s.dispatcher, rpc.ChatBtwDeps{
		Chat:        s.chatHandler,
		Broadcaster: broadcastFn,
	})

	// Native session execution / agent methods (Phase 4).
	rpc.RegisterSessionExecMethods(s.dispatcher, rpc.SessionExecDeps{
		Chat:       s.chatHandler,
		Agents:     s.agents,
		JobTracker: s.jobTracker,
	})

	// Config reload method with Go subsystem propagation.
	rpc.RegisterConfigReloadMethod(s.dispatcher, rpc.ConfigReloadDeps{
		OnReloaded: func(_ *config.ConfigSnapshot) {
			// Notify hooks of config change.
			if s.hooks != nil {
				s.safeGo("hooks:config.reloaded", func() {
					s.hooks.Fire(context.Background(), hooks.Event("config.reloaded"), nil)
				})
			}
			// Broadcast config change to subscribers.
			s.broadcaster.Broadcast("config.changed", map[string]any{
				"ts": time.Now().UnixMilli(),
			})
			// Restart channels to pick up config changes.
			if s.channelLifecycle != nil {
				s.safeGo("config:restart-channels", func() {
					reloadCtx := context.Background()
					if errs := s.channelLifecycle.StopAll(reloadCtx); len(errs) > 0 {
						for id, err := range errs {
							s.logger.Warn("config reload: channel stop failed", "channel", id, "error", err)
						}
					}
					if errs := s.channelLifecycle.StartAll(reloadCtx); len(errs) > 0 {
						for id, err := range errs {
							s.logger.Warn("config reload: channel start failed", "channel", id, "error", err)
						}
					}
					s.logger.Info("config reload: channels restarted")
				})
			}
			// Restart cron scheduler.
			if s.cron != nil {
				s.safeGo("config:restart-cron", func() {
					s.cron.Close()
					s.cron = cron.NewScheduler(s.logger)
					s.logger.Info("config reload: cron scheduler restarted")
				})
			}
		},
	})

	// Monitoring methods.
	rpc.RegisterMonitoringMethods(s.dispatcher, rpc.MonitoringDeps{
		ChannelHealth: s.channelHealth,
		Activity:      s.activity,
	})

	// Channel lifecycle RPC methods.
	rpc.RegisterChannelLifecycleMethods(s.dispatcher, rpc.ChannelLifecycleDeps{
		ChannelLifecycle: s.channelLifecycle,
		Hooks:            s.hooks,
		Broadcaster:      s.broadcaster,
	})

	// Event subscription methods.
	rpc.RegisterEventsMethods(s.dispatcher, rpc.EventsDeps{Broadcaster: s.broadcaster, Logger: s.logger})

	// Gateway identity method.
	rpc.RegisterIdentityMethods(s.dispatcher, s.version)

	// Heartbeat methods (last-heartbeat, set-heartbeats).
	if s.heartbeatState == nil {
		s.heartbeatState = rpc.NewHeartbeatState()
	}
	rpc.RegisterHeartbeatMethods(s.dispatcher, rpc.HeartbeatDeps{
		State:       s.heartbeatState,
		Broadcaster: broadcastFn,
	})

	// System presence methods (system-presence, system-event).
	if s.presenceStore == nil {
		s.presenceStore = rpc.NewPresenceStore()
	}
	rpc.RegisterPresenceMethods(s.dispatcher, rpc.PresenceDeps{
		Store:       s.presenceStore,
		Broadcaster: broadcastFn,
	})

	// Models list method.
	rpc.RegisterModelsMethods(s.dispatcher, rpc.ModelsDeps{
		Providers: s.providers,
	})

	// Stub handlers for methods that required the removed Node.js bridge.
	// Registered explicitly so callers receive ErrUnavailable instead of
	// "unknown method", and RPC parity tests pass.
	stubUnavailable := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, req.Method+" not available (requires browser/web-login integration)"))
	}
	s.dispatcher.Register("browser.request", stubUnavailable)
	s.dispatcher.Register("web.login.start", stubUnavailable)
	s.dispatcher.Register("web.login.wait", stubUnavailable)
	s.dispatcher.Register("channels.logout", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Channel string `json:"channel"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Channel == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "channel is required"))
		}
		// Validate channel exists.
		ch := s.channels.Get(p.Channel)
		if ch == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "channel not found: "+p.Channel))
		}
		// Stop the channel (logout = stop + clear).
		loggedOut := true
		if s.channelLifecycle != nil {
			if err := s.channelLifecycle.StopChannel(ctx, p.Channel); err != nil {
				s.logger.Warn("channels.logout: stop failed", "channel", p.Channel, "error", err)
				loggedOut = false
			}
		}
		// Broadcast channel change event.
		if loggedOut {
			s.broadcaster.Broadcast("channels.changed", map[string]any{
				"channelId": p.Channel,
				"action":    "logged_out",
				"ts":        time.Now().UnixMilli(),
			})
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":        true,
			"channel":   p.Channel,
			"loggedOut": loggedOut,
			"cleared":   loggedOut,
		})
		return resp
	})
}

// registerAdvancedWorkflowMethods registers Phase 3 RPC methods for exec approvals,
// nodes, devices, agents, cron advanced, config advanced, skills, wizard, secrets, and talk.
func (s *Server) registerAdvancedWorkflowMethods() {
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}

	rpc.RegisterApprovalMethods(s.dispatcher, rpc.ApprovalDeps{
		Store:       s.approvals,
		Broadcaster: broadcastFn,
	})

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

	canvasHost := ""
	if s.runtimeCfg != nil {
		canvasHost = fmt.Sprintf("http://%s:%d", s.runtimeCfg.BindHost, s.runtimeCfg.Port)
	}
	rpc.RegisterNodeMethods(s.dispatcher, rpc.NodeDeps{
		Nodes:       s.nodes,
		Broadcaster: broadcastFn,
		CanvasHost:  canvasHost,
	})

	rpc.RegisterDeviceMethods(s.dispatcher, rpc.DeviceDeps{
		Devices:     s.devices,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterCronAdvancedMethods(s.dispatcher, rpc.CronAdvancedDeps{
		Cron:        s.cron,
		RunLog:      s.cronRunLog,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterAgentsMethods(s.dispatcher, rpc.AgentsDeps{
		Agents:      s.agents,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterConfigAdvancedMethods(s.dispatcher, rpc.ConfigAdvancedDeps{
		Broadcaster: broadcastFn,
	})

	rpc.RegisterSkillMethods(s.dispatcher, rpc.SkillDeps{
		Skills:      s.skills,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterWizardMethods(s.dispatcher, rpc.WizardDeps{
		Engine: s.wizardEng,
	})

	rpc.RegisterSecretMethods(s.dispatcher, rpc.SecretDeps{
		Resolver: s.secrets,
	})

	rpc.RegisterTalkMethods(s.dispatcher, rpc.TalkDeps{
		Talk: s.talkState,
	})

	// AuroraDream: memory consolidation service (dreaming-only, no goal cycles).
	s.autonomousSvc = autonomous.NewService(s.logger)

	// Wire AuroraDream adapter if available (created in phase 2).
	if s.dreamingAdapter != nil {
		s.autonomousSvc.SetDreamer(s.dreamingAdapter)
	}

	// Broadcast dreaming events to WebSocket clients.
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		broadcastFn("dreaming.cycle", event)
	})

	// Wire Telegram notifier for dreaming events.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			s.autonomousSvc.SetNotifier(&telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			})
		}
	}

	// Gmail polling service: periodic new-email analysis via LLM.
	s.initGmailPoll()
}

// initGmailPoll initializes the Gmail polling service if enabled in config.

func (s *Server) registerNativeSystemMethods(denebDir string) {
	rpc.RegisterUsageMethods(s.dispatcher, rpc.UsageDeps{
		Tracker: s.usageTracker,
	})

	rpc.RegisterLogsMethods(s.dispatcher, rpc.LogsDeps{
		LogDir: filepath.Join(denebDir, "logs"),
	})

	rpc.RegisterDoctorMethods(s.dispatcher, rpc.DoctorDeps{})

	rpc.RegisterMaintenanceMethods(s.dispatcher, rpc.MaintenanceDeps{
		Runner: s.maintRunner,
	})

	rpc.RegisterUpdateMethods(s.dispatcher, rpc.UpdateDeps{
		DenebDir: denebDir,
	})

	// Telegram native channel plugin + messaging methods.
	// Loads Telegram config from deneb.json if available.
	if s.runtimeCfg != nil {
		tgCfg := loadTelegramConfig(s.runtimeCfg)
		if tgCfg != nil && tgCfg.BotToken != "" {
			s.telegramPlug = telegram.NewPlugin(tgCfg, s.logger)
			s.channels.Register(s.telegramPlug)
		}
	}
	rpc.RegisterMessagingMethods(s.dispatcher, rpc.MessagingDeps{
		TelegramPlugin: s.telegramPlug,
	})

	// Wire Telegram update handler → autoreply preprocessing → chat.send pipeline.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
	}

	// Discord native channel plugin (coding-focused).
	if s.runtimeCfg != nil {
		dcCfg := loadDiscordConfig(s.runtimeCfg)
		if dcCfg != nil && dcCfg.BotToken != "" {
			s.discordPlug = discord.NewPlugin(dcCfg, s.logger)
			s.channels.Register(s.discordPlug)
		}
	}

	// Wire Discord message handler → chat.send pipeline.
	if s.discordPlug != nil && s.chatHandler != nil {
		s.wireDiscordChatHandler()
	}

}

// wireTelegramChatHandler connects the Telegram polling handler to the chat
// handler via the autoreply inbound processor so incoming messages go through
// command detection, directive parsing, and normalization before reaching the
// LLM agent.

func (s *Server) registerBuiltinMethods() {
	s.dispatcher.Register("health", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"status": "ok",
			"uptime": time.Since(s.startedAt).Milliseconds(),
		})
		return resp
	})

	s.dispatcher.Register("status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"version":     s.version,
			"channels":    s.channels.StatusAll(),
			"sessions":    s.sessions.Count(),
			"connections": s.clientCnt.Load(),
		})
		return resp
	})

	// gateway.identity.get: returns the gateway's identity and runtime information.
	s.dispatcher.Register("gateway.identity.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"version": s.version,
			"runtime": "go",
			"uptime":  time.Since(s.startedAt).Milliseconds(),
			"rustFFI": s.rustFFI,
		})
		return resp
	})

	// last-heartbeat: returns the last heartbeat timestamp.
	s.dispatcher.Register("last-heartbeat", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var ts int64
		if s.activity != nil {
			ts = s.activity.LastActivityAt()
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"lastHeartbeatMs": ts,
		})
		return resp
	})

	// set-heartbeats: configure heartbeat settings (accepted but no-op in Go gateway;
	// the tick broadcaster runs at a fixed 1000ms interval).
	s.dispatcher.Register("set-heartbeats", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	})

	// system-presence: broadcast a presence event to all connected clients.
	s.dispatcher.Register("system-presence", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var payload any
		if len(req.Params) > 0 {
			var p struct {
				Payload any `json:"payload"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrInvalidRequest, "invalid params"))
			}
			payload = p.Payload
		}
		sent, _ := s.broadcaster.Broadcast("presence", payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})

	// system-event: broadcast an arbitrary system event.
	s.dispatcher.Register("system-event", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		sent, _ := s.broadcaster.Broadcast(p.Event, p.Payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})

	// models.list: return provider model list if available.
	s.dispatcher.Register("models.list", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.providers == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{"models": []any{}})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"models": s.providers.List(),
		})
		return resp
	})

	// config.get: returns the resolved runtime config for diagnostics.
	s.dispatcher.Register("config.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.runtimeCfg == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]string{"status": "not_loaded"})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"bindHost":      s.runtimeCfg.BindHost,
			"port":          s.runtimeCfg.Port,
			"authMode":      s.runtimeCfg.AuthMode,
			"tailscaleMode": s.runtimeCfg.TailscaleMode,
		})
		return resp
	})
}

// pluginRegistryAdapter bridges plugin.FullRegistry to the rpc.PluginRegistry interface.
type pluginRegistryAdapter struct {
	registry *plugin.FullRegistry
}

func (a *pluginRegistryAdapter) ListPlugins() []protocol.PluginMeta {
	raw := a.registry.ListPlugins()
	result := make([]protocol.PluginMeta, len(raw))
	for i, p := range raw {
		result[i] = protocol.PluginMeta{
			ID:      p.ID,
			Name:    p.Label,
			Kind:    protocol.PluginKind(p.Kind),
			Version: p.Version,
			Enabled: p.Enabled,
		}
	}
	return result
}

func (a *pluginRegistryAdapter) GetPluginHealth(id string) *protocol.PluginHealthStatus {
	p := a.registry.GetPlugin(id)
	if p == nil {
		return nil
	}
	return &protocol.PluginHealthStatus{
		PluginID: p.ID,
		Healthy:  p.Enabled,
	}
}

// truncateForDedup returns at most maxLen bytes of s for use as a dedup key.
func truncateForDedup(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
