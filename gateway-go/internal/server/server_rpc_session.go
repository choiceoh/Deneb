package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
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
	sessionDeps := rpc.SessionDeps{
		Sessions:    s.sessions,
		GatewaySubs: s.gatewaySubs,
		Transcripts: s.transcript,
		Compressor:  sessionCompressor,
	}
	rpc.RegisterSessionMethods(s.dispatcher, sessionDeps)

	// Session repair and overflow check methods.
	rpc.RegisterSessionRepairMethods(s.dispatcher, sessionDeps)

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
	// can late-bind fields (Vega.Backend, Sessions.SendFn, Chrono.SendFn).
	s.toolDeps = &chat.CoreToolDeps{
		WorkspaceDir: workspaceDir,
		Process: chat.ProcessDeps{
			Mgr:          s.processes,
			WorkspaceDir: workspaceDir,
		},
		Sessions: chat.SessionDeps{
			Manager:    s.sessions,
			Transcript: transcriptStore,
		},
		Chrono: chat.ChronoDeps{
			Scheduler: s.cron,
		},
		Vega: chat.VegaDeps{
			MemoryStore: chatCfg.MemoryStore,
		},
		LLMClient:    chatCfg.LLMClient,
		DefaultModel: chatCfg.DefaultModel,
		AgentLog:     agentLogWriter,
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
}

// registerApprovalAgentMethods registers exec approval, agent lifecycle, talk, wizard,
// and autonomous dreaming methods.
func (s *Server) registerApprovalAgentMethods(broadcastFn func(string, any) (int, []error)) {
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

	rpc.RegisterAgentsMethods(s.dispatcher, rpc.AgentsDeps{
		Agents:      s.agents,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterWizardMethods(s.dispatcher, rpc.WizardDeps{
		Engine: s.wizardEng,
	})

	rpc.RegisterTalkMethods(s.dispatcher, rpc.TalkDeps{
		Talk: s.talkState,
	})

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
}
