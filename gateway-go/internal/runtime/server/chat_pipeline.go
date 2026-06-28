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
		TranslationModel: resolveTranslationModel(s.logger),
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
				// Open loops are no longer auto-recorded as to-dos (operator approval
				// first) — no open-loop sink is wired (the dreamer skips it when nil).
				// Per-project latest-progress digests are written directly into each
				// project 대표페이지's "## 현재 상태" section by the dream cycle itself
				// (no sink — the dreamer owns the wiki store; see project_digest.go),
				// and kept fresh between cycles by the mail-analysis sink.
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
	// notebook tool (nil store), it does not block chat init. Promoted to the
	// server so the mail pipeline (fileDealFromMail) can auto-pin deal evidence.
	notebookDir := filepath.Join(config.ResolveStateDir(), "notebooks")
	if ns, err := notebook.NewStore(notebookDir); err != nil {
		s.logger.Warn("notebook store unavailable", "error", err)
	} else {
		s.notebookStore = ns
		s.logger.Info("notebook store enabled", "dir", notebookDir)
	}
	notebookStore := s.notebookStore
	// Thread the notebook store into the chat handler (not just CoreToolDeps) so
	// the run pipeline can build the session-grounding tail block for a bound
	// session. NewHandler (server_rpc_session.go) is built after this from the
	// same chatCfg, so setting it here is captured.
	chatCfg.NotebookStore = notebookStore

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
			// External source ingesters (url/mail/diary) — snapshot to text at
			// add time (notebook_sources.go). file (PDF/image OCR, text) is
			// handled in-package by the tool and needs no reader here.
			FetchURL: notebookFetchURL,
			ReadMail: notebookReadMail,
			ReadDiary: func(ctx context.Context, ref string) (string, error) {
				return notebookReadDiary(chatCfg.WikiStore, ref)
			},
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
		// Deep-research panel fan-out: one prompt → every healthy wormhole-served
		// model in parallel (research_panel tool). nil-safe — the tool checks it.
		ConsultPanel: s.consultModelPanel,
		AgentLog:     agentLogWriter,
		LogCapture:   s.logCapture,
		WorkFeed:     s.workFeedStore,
		// Engine-level prefix-cache scrape targets for the observe tool.
		VllmBaseURLs: reg.VllmBaseURLs,
		// Deliver phone_write Intent actions (open_url/share/…) to the native app
		// over SSE for in-app execution — the SSH/Termux-free path.
		PhoneActionSender: s.dispatchPhoneAction,
		// Fleet management: the agent's twin of the /api/v1/fleet passthrough —
		// reaches the same SparkFleet control plane via s.fleet's accessors, so
		// "is the fleet ok / restart qwen36" works from chat. "" base = off.
		Fleet: chat.FleetDeps{BaseURL: s.fleet.BaseURL, Token: s.fleet.Token},
	}

	// Ambient calendar awareness: a frozen-per-day upcoming-events glance in the
	// dynamic system-prompt block, built over the same hybrid calendar source as
	// the calendar tool. nil when no calendar source is wired (feature off).
	chatCfg.CalendarGlanceFn = chat.NewCalendarGlanceFunc(&s.toolDeps.Calendar)
	chatCfg.GoalGlanceFn = chat.NewGoalGlanceFunc()

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

	// Semantic (vector) file search: opens the shared file store + BGE-M3 index
	// and wires s.toolDeps.FilesSemanticSearch. Must run before RegisterCoreTools
	// (the files tool captures the search closure at registration time). The
	// background reindex task is registered later in registerWorkflowSideEffects.
	s.initFileSemanticIndex()

	// Core tools (file I/O, exec, process, sessions, gateway, cron, image).
	chat.RegisterCoreTools(chatCfg.Tools, s.toolDeps)

	// Knowledge: unified recall/read/record surface over the wiki knowledge
	// base and the on-box file store. Polaris (session-bound) and graphify
	// (graph-traversal) stay separate because they have different paradigms.
	// Each adapter constructor returns nil when its backend is unavailable
	// (wiki store missing, or file index/embedding server down) → the router
	// simply drops that layer (knowledge.New ignores nil adapters), so recall
	// degrades gracefully to whatever backends are live.
	filesAdapter := s.newFilesKnowledgeAdapter()
	knowledgeRouter := knowledge.New(
		knowledge.NewWikiAdapter(s.wikiStore),
		filesAdapter,
	)
	toolreg.RegisterKnowledgeTool(chatCfg.Tools, knowledgeRouter)

	// Recall preflight files source: surface relevant uploaded files as recall
	// evidence (injected into the last user message tail like wiki/diary/session).
	// nil when the file index/embedding server is unavailable → the preflight's
	// files source contributes nothing (graceful, recall unaffected). Set on the
	// config here (after initFileSemanticIndex) so NewHandler captures it.
	if filesAdapter != nil {
		chatCfg.FileRecallFn = s.fileRecallForPreflight
	}

	// Coding mode: after a coding-session turn, checkpoint the worktree edits and
	// verify build/tests (method_registry.go codingTurnEnd). nil when coding mode
	// is disabled (no denebDir / store) → the chat hook is simply never armed.
	if mgr, store := s.codingBackends(); mgr != nil && store != nil {
		chatCfg.CodingTurnEndFn = s.codingTurnEnd
	}

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
