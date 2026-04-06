// Chat pipeline initialization: tool registration and handler construction.
// Extracted from registerSessionRPCMethods() to reduce that function
// to a clear sequential flow.
package server

import (
	"context"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/rlm"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// initMemorySubsystem initializes model registry, session memory, wiki, and RLM.
// All results are set on chatCfg and s.
func (s *Server) initMemorySubsystem(chatCfg *chat.HandlerConfig, regPtr **modelrole.Registry) {
	// Model role registry.
	chatCfg.DefaultModel = resolveDefaultModel(s.logger)
	chatCfg.SubagentDefaultModel = resolveSubagentDefaultModel(s.logger)
	reg := modelrole.NewRegistry(s.logger, chatCfg.DefaultModel)
	*regPtr = reg
	chatCfg.Registry = reg
	s.modelRegistry = reg

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	// Session memory store (structured working state per session).
	sessMemDir := filepath.Join(home, ".deneb", "sessions")
	sessMemStore := chat.NewSessionMemoryStore(sessMemDir)
	loaded := sessMemStore.LoadFromDisk()
	chatCfg.SessionMemory = sessMemStore
	if loaded > 0 {
		s.logger.Info("session memory restored", "count", loaded)
	}

	// Wiki knowledge base.
	if wikiCfg := wiki.ConfigFromEnv(); wikiCfg.Enabled {
		wikiStore, err := wiki.NewStore(wikiCfg.Dir, wikiCfg.DiaryDir)
		if err != nil {
			s.logger.Warn("wiki store unavailable", "error", err)
		} else {
			s.wikiStore = wikiStore
			chatCfg.WikiStore = wikiStore
			s.logger.Info("wiki knowledge base enabled", "dir", wikiCfg.Dir)

			// RLM service backed by wiki (always active).
			rlmCfg := rlm.ConfigFromEnv()
			s.rlmService = rlm.NewService(rlmCfg, wikiStore, s.logger)
			s.logger.Info("rlm: service enabled (wiki-backed)")

			// Wiki dreamer.
			lwClient := (*regPtr).Client(modelrole.RoleLightweight)
			lwModel := (*regPtr).Model(modelrole.RoleLightweight)
			if lwClient != nil && lwModel != "" {
				s.wikiDreamer = wiki.NewWikiDreamer(wikiStore, lwClient, lwModel, wikiCfg, s.logger)
				s.logger.Info("wiki-dream: enabled")
			}
		}
	}
}

// initToolsAndDeps builds CoreToolDeps, registers core/plugin/autoresearch
// tools, and stores toolDeps on the server.
func (s *Server) initToolsAndDeps(chatCfg *chat.HandlerConfig, reg *modelrole.Registry, transcriptStore chat.TranscriptStore, agentLogWriter *agentlog.Writer) {
	workspaceDir := resolveWorkspaceDir()

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
			Service: s.cronService,
			RunLog:  s.cronRunLog,
		},
		Wiki: chat.WikiDeps{
			Store: chatCfg.WikiStore,
		},
		LLMClient:    reg.Client(modelrole.RoleLightweight),
		DefaultModel: reg.Model(modelrole.RoleLightweight),
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

	// Autoresearch runner + tool.
	s.autoresearchRunner = autoresearch.NewRunner(s.logger)
	if lwClient := reg.Client(modelrole.RoleLightweight); lwClient != nil {
		s.autoresearchRunner.SetLLMClient(lwClient)
		s.autoresearchRunner.SetDefaultModel(reg.Model(modelrole.RoleLightweight))
	} else if mainClient := reg.Client(modelrole.RoleMain); mainClient != nil {
		s.autoresearchRunner.SetLLMClient(mainClient)
	}
	if transcriptStore != nil {
		s.autoresearchRunner.SetTranscriptAppendFn(func(sessionKey, text string) error {
			msg := chat.NewTextChatMessage("system", text, 0)
			return transcriptStore.Append(sessionKey, msg)
		})
	}
	toolreg.RegisterAutoresearchTool(chatCfg.Tools, s.autoresearchRunner)

	// Bridge: inter-agent communication tool.
	toolreg.RegisterBridgeTool(chatCfg.Tools, s.broadcaster.Broadcast)
}
