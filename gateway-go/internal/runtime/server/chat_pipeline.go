// Chat pipeline initialization: tool registration and handler construction.
// Extracted from registerSessionRPCMethods() to reduce that function
// to a clear sequential flow.
package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	domskills "github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolreg"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// initMemorySubsystem initializes model registry, session memory, and wiki.
// All results are set on chatCfg and s.
func (s *Server) initMemorySubsystem(chatCfg *chat.HandlerConfig, regPtr **modelrole.Registry) {
	// Model role registry.
	chatCfg.DefaultModel = resolveDefaultModel(s.logger)
	chatCfg.SubagentDefaultModel = resolveSubagentDefaultModel(s.logger)
	localVllmModel := resolveLocalVllmModel(s.logger)
	reg := modelrole.NewRegistryWithOptions(s.logger, modelrole.RegistryOptions{
		MainModel:        chatCfg.DefaultModel,
		LocalVllmModel:   localVllmModel,
		LightweightModel: resolveLightweightModel(s.logger),
		TinyModel:        resolveTinyModel(s.logger),
		AnalysisModel:    resolveAnalysisModel(s.logger),
		CodingModel:      resolveCodingModel(s.logger),
		FallbackModel:    resolveFallbackModel(s.logger),
		ChatbotModel:     resolveChatbotModel(s.logger),
		VisionModel:      resolveVisionModel(s.logger),
		Providers:        providerCatalog(s.logger),
	})
	*regPtr = reg
	chatCfg.Registry = reg
	s.modelRegistry = reg

	// Seed new sessions with operator-configured thinking defaults so the
	// model can use extended thinking from the first turn without /think.
	if defaults := resolveSessionThinkingDefaults(s.logger); defaults.ThinkingLevel != "" || defaults.InterleavedThinking != nil {
		s.sessions.SetSessionDefaults(defaults)
		interleaved := false
		if defaults.InterleavedThinking != nil {
			interleaved = *defaults.InterleavedThinking
		}
		s.logger.Info("session thinking defaults",
			"level", defaults.ThinkingLevel,
			"interleaved", interleaved)
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

			// Wiki dreamer.
			lwClient := (*regPtr).Client(modelrole.RoleLightweight)
			lwModel := (*regPtr).Model(modelrole.RoleLightweight)
			if lwClient != nil && lwModel != "" {
				s.wikiDreamer = wiki.NewWikiDreamer(wikiStore, lwClient, lwModel, wikiCfg, s.logger)
				// Let dream cycles consume + curate the auto-recorded
				// workspace MEMORY.md (distill to wiki, keep a bounded buffer).
				s.wikiDreamer.SetWorkspaceDir(resolveWorkspaceDir())
				// Prospective memory: extracted commitments land in the local
				// to-do store (native list + heartbeat deadline signals).
				s.wikiDreamer.SetOpenLoopSink(openLoopTodoSink(s.logger))
				// Mention-driven 인물 seeding from the contacts mirror.
				if cs := s.contactsStore; cs != nil {
					s.wikiDreamer.SetPersonDirectory(func() []wiki.PersonSeed {
						all := cs.All()
						seeds := make([]wiki.PersonSeed, 0, len(all))
						for _, c := range all {
							seeds = append(seeds, wiki.PersonSeed{
								Name: c.Name, Org: c.Org, Phones: c.Phones, Emails: c.Emails,
							})
						}
						return seeds
					})
				}
				s.logger.Info("wiki-dream: enabled")
			}
		}
	}
}

// initToolsAndDeps builds CoreToolDeps, registers core/plugin tools,
// and stores toolDeps on the server.
func (s *Server) initToolsAndDeps(chatCfg *chat.HandlerConfig, reg *modelrole.Registry, transcriptStore chat.TranscriptStore, agentLogWriter *agentlog.Writer) {
	workspaceDir := resolveWorkspaceDir()

	// Out-of-workspace skill catalog roots: lets the read tool reach the SKILL.md
	// locations the skills index advertises (same roots the discovery walks;
	// workspace-local roots need no allowance). The repo's bundled skills/ root is
	// included so bundled SKILL.md bodies are readable too (not just listed).
	bundledSkillsDir := chat.BundledSkillsDir()
	skillCatalogDirs := []string{
		domskills.DefaultManagedSkillsDir(),
		domskills.DefaultPersonalSkillsDir(),
	}
	if bundledSkillsDir != "" {
		skillCatalogDirs = append(skillCatalogDirs, bundledSkillsDir)
	}

	// Notebook store: NotebookLM-style scoped source collections (딜/프로젝트
	// 브리핑). Lives under the gateway state dir; a failure just disables the
	// notebook tool (nil store), it does not block chat init.
	var notebookStore *notebook.Store
	notebookDir := filepath.Join(config.ResolveStateDir(), "notebooks")
	if ns, err := notebook.NewStore(notebookDir); err != nil {
		s.logger.Warn("notebook store unavailable", "error", err)
	} else {
		notebookStore = ns
		s.logger.Info("notebook store enabled", "dir", notebookDir)
	}

	s.toolDeps = &chat.CoreToolDeps{
		WorkspaceDir:      workspaceDir,
		SkillsCatalogDirs: skillCatalogDirs,
		BundledSkillsDir:  bundledSkillsDir,
		Process: chat.ProcessDeps{
			Mgr:          s.processes,
			WorkspaceDir: workspaceDir,
		},
		Sessions: chat.SessionDeps{
			Manager:              s.sessions,
			Transcript:           transcriptStore,
			SubagentDefaultModel: chatCfg.SubagentDefaultModel,
			CodingDefaultModel:   reg.FullModelID(modelrole.RoleCoding),
			CodingDefaultModelFn: func() string {
				if reg == nil {
					return ""
				}
				return reg.FullModelID(modelrole.RoleCoding)
			},
		},
		Chrono: chat.ChronoDeps{
			Service: s.cronService,
			RunLog:  s.cronRunLog,
		},
		Wiki: chat.WikiDeps{
			Store: chatCfg.WikiStore,
			// Same address book the contacts tool uses; lets a wiki write
			// auto-record a referenced person's contact details.
			Contacts: s.contactsStore,
		},
		Notebook: chat.NotebookDeps{
			Store: notebookStore,
			// Pinned wiki-page sources are read live from the same store at
			// brief time, so a notebook briefing reflects the current page.
			Wiki: chatCfg.WikiStore,
		},
		Contacts: chat.ContactsDeps{
			// Created during registerEarlyMethods (no chat dep), so it's already
			// wired by the time chat init runs.
			Store: s.contactsStore,
		},
		Calendar: chat.CalendarDeps{
			// Same hybrid sources as the miniapp.calendar.* RPC surface: a
			// lazy read-only Google client (nil-safe before OAuth) merged with
			// the local store for create/edit/delete. Reusing the resolvers
			// keeps the chat tool and the native UI on one calendar.
			Client: func() (chat.CalendarReader, error) {
				return calendar.DefaultClient()
			},
			Local: resolveLocalCalendar(s.logger),
		},
		LLMClient:    reg.Client(modelrole.RoleLightweight),
		DefaultModel: reg.Model(modelrole.RoleLightweight),
		AgentLog:     agentLogWriter,
		LogCapture:   s.logCapture,
		WorkFeed:     s.workFeedStore,
		// Engine-level prefix-cache scrape targets for the observe tool.
		VllmBaseURLs: reg.VllmBaseURLs,
		// Fleet management: the agent's twin of the /api/v1/fleet passthrough —
		// reaches the same SparkFleet control plane via s.fleet's accessors, so
		// "is the fleet ok / restart qwen36" works from chat. "" base = off.
		Fleet: chat.FleetDeps{BaseURL: s.fleet.BaseURL, Token: s.fleet.Token},
	}

	// Ambient calendar awareness: a frozen-per-day upcoming-events glance in the
	// dynamic system-prompt block, built over the same hybrid calendar source as
	// the calendar tool. nil when no calendar source is wired (feature off).
	chatCfg.CalendarGlanceFn = chat.NewCalendarGlanceFunc(&s.toolDeps.Calendar)

	// Operator-edited 업무 persona (Settings prompt corner → prompt store). Returns
	// "" when unedited so the chat pipeline renders the default persona. Reading
	// the store keeps the chat package free of the prompt-store import.
	chatCfg.PersonaOverrideFn = s.personaOverride

	// Spillover store: saves large tool results to disk, replaces with preview.
	// Session-end events release per-session spill files immediately instead of
	// waiting for the 30-minute TTL sweep (see server_spillover_lifecycle.go).
	if home, err := os.UserHomeDir(); err == nil {
		spillDir := filepath.Join(home, ".deneb", "spillover")
		spillStore := agent.NewSpilloverStore(spillDir)
		spillStore.StartCleanup(context.Background())
		s.toolDeps.SpilloverStore = spillStore
		s.initSpilloverLifecycle(spillStore)
	}

	// Core tools (file I/O, exec, process, sessions, gateway, cron, image).
	chat.RegisterCoreTools(chatCfg.Tools, s.toolDeps)

	// Knowledge: unified recall/read/record surface over the wiki knowledge
	// base. Polaris (session-bound) and graphify (graph-traversal) stay
	// separate because they have different paradigms. Skipped when the wiki
	// store is unavailable (NewWikiAdapter returns nil → router has no layers).
	knowledgeRouter := knowledge.New(
		knowledge.NewWikiAdapter(s.wikiStore),
	)
	toolreg.RegisterKnowledgeTool(chatCfg.Tools, knowledgeRouter)

	// Polaris: retrieval tools for compressed conversation history.
	if bridge, ok := transcriptStore.(*polaris.Bridge); ok {
		var localAI tools.LocalAIFunc
		if pilot.LocalAIHub() != nil {
			localAI = func(ctx context.Context, system, user string, maxTokens int) (string, error) {
				return pilot.CallLocalLLM(ctx, system, user, maxTokens)
			}
		}
		toolreg.RegisterPolarisTools(chatCfg.Tools, bridge.Store(), localAI)

		// Wire dreamer to read recent polaris summaries as a higher-density
		// fact source alongside raw diary entries.
		if s.wikiDreamer != nil {
			polarisStore := bridge.Store()
			s.wikiDreamer.SetPolarisContextFn(func() string {
				return formatRecentPolarisSummaries(polarisStore.RecentSummariesAcrossSessions(dreamerPolarisSummaryLimit))
			})
		}
	}
}

const dreamerPolarisSummaryLimit = 8

// formatRecentPolarisSummaries renders polaris summary nodes as bullet text for
// the wiki dreamer's synthesis prompt.
func formatRecentPolarisSummaries(nodes []polaris.SummaryNode) string {
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, n := range nodes {
		sb.WriteString("- ")
		sb.WriteString(n.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}
