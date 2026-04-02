// Chat pipeline initialization: memory subsystem, tool registration, and
// handler construction. Extracted from registerSessionRPCMethods() to reduce
// that 467-line function to a clear sequential flow.
package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/unified"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// initMemorySubsystem initializes unified store, Aurora compaction store,
// structured memory store, Gemini embedder, Jina reranker, dreaming adapter,
// and MEMORY.md auto-migration. All results are set on chatCfg and s.
func (s *Server) initMemorySubsystem(chatCfg *chat.HandlerConfig, reg *modelrole.Registry) {
	// Unified memory store (single DB for all tiers).
	unifiedStore, err := unified.New(unified.DefaultConfig(), s.logger)
	if err != nil {
		s.logger.Warn("unified store unavailable", "error", err)
	} else {
		chatCfg.UnifiedStore = unifiedStore
		s.logger.Info("unified memory store initialized")

		if auroraStore, aErr := unifiedStore.NewAuroraStoreWithLogger(s.logger); aErr != nil {
			s.logger.Warn("aurora store unavailable from unified db, compaction will use legacy fallback", "error", aErr)
		} else {
			chatCfg.AuroraStore = auroraStore
			s.logger.Info("aurora compaction store initialized (unified)")
		}
	}

	// Legacy fallback for Aurora compaction.
	if chatCfg.AuroraStore == nil {
		auroraStore, aErr := aurora.NewStore(aurora.DefaultStoreConfig(), s.logger)
		if aErr != nil {
			s.logger.Warn("aurora store unavailable, compaction will use legacy fallback", "error", aErr)
		} else {
			chatCfg.AuroraStore = auroraStore
			s.logger.Info("aurora compaction store initialized")
		}
	}

	// Model role registry.
	chatCfg.DefaultModel = resolveDefaultModel(s.logger)
	chatCfg.SubagentDefaultModel = resolveSubagentDefaultModel(s.logger)
	if chatCfg.SubagentDefaultModel != "" {
		s.logger.Info("subagent default model configured", "model", chatCfg.SubagentDefaultModel)
	}
	reg2 := modelrole.NewRegistry(s.logger, chatCfg.DefaultModel)
	*reg = *reg2
	chatCfg.Registry = reg
	s.modelRegistry = reg

	// Structured memory store (Honcho-style).
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dbPath := filepath.Join(home, ".deneb", "memory.db")
	memStore := chatCfg.MemoryStore
	if memStore == nil && unifiedStore != nil {
		unifiedMemStore, uErr := unifiedStore.NewMemoryStore()
		if uErr != nil {
			s.logger.Warn("memory store unavailable from unified db", "error", uErr)
		} else {
			memStore = unifiedMemStore
			chatCfg.MemoryStore = memStore
			s.logger.Info("aurora-memory: structured store initialized (unified)")
		}
	}
	if memStore == nil {
		legacyStore, mErr := memory.NewStore(dbPath)
		if mErr != nil {
			s.logger.Warn("memory store unavailable", "error", mErr)
		} else {
			memStore = legacyStore
			chatCfg.MemoryStore = memStore
			s.logger.Info("aurora-memory: structured store initialized", "db", dbPath)
		}
	}
	if memStore == nil {
		return
	}

	// Gemini embedder + dreaming adapter.
	if s.geminiEmbedder != nil {
		embedder := memory.NewEmbedder(s.geminiEmbedder, memStore, s.logger)
		chatCfg.MemoryEmbedder = embedder

		lwClient := reg.Client(modelrole.RoleLightweight)
		lwModel := reg.Model(modelrole.RoleLightweight)
		if lwClient == nil || lwModel == "" {
			s.logger.Warn("aurora-memory: dreaming disabled (lightweight model not configured)")
		} else {
			s.dreamingAdapter = memory.NewDreamingAdapter(memStore, embedder, lwClient, lwModel, s.logger)
		}
		chatCfg.DreamTurnFn = func(ctx context.Context) {
			if svc := s.autonomousSvc; svc != nil {
				svc.IncrementDreamTurn(ctx)
			}
		}
	} else {
		s.logger.Info("aurora-memory: embedding disabled (GEMINI_API_KEY not set)")
	}

	// Jina cross-encoder reranker.
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

	// Tier-1 cache invalidation.
	memStore.SetFactMutateCallback(unified.InvalidateTier1Cache)
	s.memoryStore = memStore

	// Auto-migrate existing MEMORY.md on first run.
	count, _ := memStore.ActiveFactCount(context.Background())
	if count == 0 {
		memoryMdPath := filepath.Join(home, ".deneb", "MEMORY.md")
		if imported, err := memStore.ImportFromMarkdown(context.Background(), memoryMdPath); err == nil && imported > 0 {
			s.logger.Info("aurora-memory: imported legacy MEMORY.md", "facts", imported)
		}
	}

	// Session memory store (structured working state per session).
	sessMemDir := filepath.Join(home, ".deneb", "sessions")
	sessMemStore := chat.NewSessionMemoryStore(sessMemDir)
	loaded := sessMemStore.LoadFromDisk()
	chatCfg.SessionMemory = sessMemStore
	if loaded > 0 {
		s.logger.Info("session memory restored", "count", loaded)
	}
}

// initToolsAndDeps builds CoreToolDeps, registers core/plugin/autoresearch
// tools, and stores toolDeps on the server.
func (s *Server) initToolsAndDeps(chatCfg *chat.HandlerConfig, reg *modelrole.Registry, transcriptStore chat.TranscriptStore, agentLogWriter *agentlog.Writer) {
	workspaceDir := resolveWorkspaceDir()
	s.logger.Info("resolved agent workspace directory", "workspaceDir", workspaceDir)

	s.toolDeps = &chat.CoreToolDeps{
		WorkspaceDir: workspaceDir,
		Process: chat.ProcessDeps{
			Mgr:          s.processes,
			WorkspaceDir: workspaceDir,
		},
		Sessions: chat.SessionDeps{
			Manager:              s.sessions,
			Transcript:           transcriptStore,
			SubagentDefaultModel: chatCfg.SubagentDefaultModel,
		},
		Chrono: chat.ChronoDeps{
			Scheduler: s.cron,
		},
		Vega: chat.VegaDeps{
			MemoryStore:    chatCfg.MemoryStore,
			MemoryEmbedder: chatCfg.MemoryEmbedder,
			RecallClient:   reg.Client(modelrole.RoleFallback),
			RecallModel:    reg.Model(modelrole.RoleFallback),
		},
		LLMClient:    reg.Client(modelrole.RoleFallback),
		DefaultModel: reg.Model(modelrole.RoleFallback),
		AgentLog:     agentLogWriter,
	}

	// Spillover store: saves large tool results to disk, replaces with preview.
	if home, err := os.UserHomeDir(); err == nil {
		spillDir := filepath.Join(home, ".deneb", "spillover")
		spillStore := agent.NewSpilloverStore(spillDir)
		spillStore.StartCleanup(context.Background())
		s.toolDeps.SpilloverStore = spillStore
	}

	// Core tools (file I/O, exec, process, sessions, gateway, cron, image).
	chat.RegisterCoreTools(chatCfg.Tools, s.toolDeps)

	// Plugin-provided tools.
	if s.pluginFullRegistry != nil {
		for _, t := range s.pluginFullRegistry.ListTools() {
			pluginTool := t
			chatCfg.Tools.RegisterTool(chat.ToolDef{
				Name:        pluginTool.Definition.Name,
				Description: pluginTool.Definition.Description,
				InputSchema: pluginTool.Definition.InputSchema,
				Fn: func(ctx context.Context, input json.RawMessage) (string, error) {
					var m map[string]any
					if err := json.Unmarshal(input, &m); err != nil {
						return "", err
					}
					return pluginTool.Handler(ctx, m)
				},
			})
		}
		if count := len(s.pluginFullRegistry.ListTools()); count > 0 {
			s.logger.Info("plugin tools registered", "count", count)
		}
	}

	// Autoresearch runner + tool.
	// Use the lightweight (local sglang) model for autoresearch: it runs many
	// iterations autonomously, so a local model avoids external API hangs and
	// keeps latency low. The Qwen 35B model is more than capable for the
	// hypothesis-and-tweak loop autoresearch performs.
	s.autoresearchRunner = autoresearch.NewRunner(s.logger)
	if lwClient := reg.Client(modelrole.RoleLightweight); lwClient != nil {
		s.autoresearchRunner.SetLLMClient(lwClient)
		s.autoresearchRunner.SetDefaultModel(reg.Model(modelrole.RoleLightweight))
	} else if mainClient := reg.Client(modelrole.RoleMain); mainClient != nil {
		s.autoresearchRunner.SetLLMClient(mainClient)
	}
	// Inject autoresearch completion reports into the triggering session's
	// transcript so the LLM sees results on its next turn.
	if transcriptStore != nil {
		s.autoresearchRunner.SetTranscriptAppendFn(func(sessionKey, text string) error {
			msg := chat.NewTextChatMessage("system", text, 0)
			return transcriptStore.Append(sessionKey, msg)
		})
	}
	toolreg.RegisterAutoresearchTool(chatCfg.Tools, s.autoresearchRunner)
}
