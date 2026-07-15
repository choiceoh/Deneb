// Chat pipeline initialization: tool registration and handler construction.
// Extracted from registerSessionRPCMethods() to reduce that function
// to a clear sequential flow.
package server

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/chatwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcops"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
)

// initMemorySubsystem initializes model registry, session memory, and wiki.
// All results are set on chatCfg and s.
func (s *Server) initMemorySubsystem(chatCfg *pipebind.HandlerConfig, regPtr **aibind.ModelRoleRegistry) {
	// Model role registry.
	chatCfg.DefaultModel = svcbind.DefaultModel(s.logger)
	chatCfg.SubagentDefaultModel = svcbind.SubagentDefaultModel(s.logger)
	localVllmModel := svcbind.LocalVLLMModel(s.logger)
	reg := aibind.NewRegistryWithOptions(s.logger, aibind.RegistryOptions{
		MainModel:        chatCfg.DefaultModel,
		LocalVllmModel:   localVllmModel,
		LightweightModel: svcbind.LightweightModel(s.logger),
		TinyModel:        svcbind.TinyModel(s.logger),
		CodingModel:      svcbind.CodingModel(s.logger),
		FallbackModel:    svcbind.FallbackModel(s.logger),
		VisionModel:      svcbind.VisionModel(s.logger),
		Providers:        svcbind.ProviderCatalog(s.logger),
	})
	*regPtr = reg
	chatCfg.Registry = reg
	s.modelRegistry = reg

	// Seed new sessions with operator-configured thinking defaults so the
	// model can use extended thinking from the first turn without /think.
	if defaults := svcbind.SessionThinkingDefaults(s.logger); defaults.ThinkingLevel != "" || defaults.InterleavedThinking != nil {
		s.sessions.SetSessionDefaults(defaults)
		interleaved := false
		if defaults.InterleavedThinking != nil {
			interleaved = *defaults.InterleavedThinking
		}
		s.logger.Info("session thinking defaults",
			"level", defaults.ThinkingLevel,
			"interleaved", interleaved)
	}

	// Local mail store: file-backed mirror of the on-box IMAP archive so the
	// mail_archive tool answers reads from memory (no per-call IMAP round-trip +
	// re-parse + re-index). LMTP intake writes new mail here; cmd/mail-backfill
	// seeds existing mail. Reads IMAP-fall back while the store is empty/missing.
	mailStoreDir := filepath.Join(infrabind.ResolveStateDir(), "mailstore")
	if ms, err := platbind.NewMailStore(mailStoreDir); err != nil {
		s.logger.Warn("mailstore unavailable", "error", err)
	} else {
		s.mailStore = ms
		s.logger.Info("mailstore enabled", "dir", mailStoreDir, "messages", ms.Len())
		// Seed historical mail into the store once, in the background, so older-mail
		// reads hit the fast path instead of the ~12.9s per-call IMAP fallback (the
		// store only auto-fills with NEW mail otherwise). One-shot + best-effort.
		s.maybeAutoBackfillMailStore(mailStoreDir)
	}

	// Wiki knowledge base.
	if wikiCfg := domainbind.WikiConfigFromEnv(); wikiCfg.Enabled {
		wikiStore, err := domainbind.NewWikiStore(wikiCfg.Dir, wikiCfg.DiaryDir)
		if err != nil {
			s.logger.Warn("wiki store unavailable", "error", err)
		} else {
			s.wikiStore = wikiStore
			chatCfg.Memory.Wiki = wikiStore
			s.logger.Info("wiki knowledge base enabled", "dir", wikiCfg.Dir)

			// Wiki dreamer.
			lwClient := (*regPtr).Client(aibind.RoleLightweight)
			lwModel := (*regPtr).Model(aibind.RoleLightweight)
			if lwClient != nil && lwModel != "" {
				s.wikiDreamer = domainbind.NewWikiDreamer(wikiStore, lwClient, lwModel, wikiCfg, s.logger)
				// Shape the dreamer's raw LLM calls for the lightweight model:
				// thinking off on dual-mode reasoning models (deepseek-v4's
				// chain-of-thought consumed the whole 4096-token synthesis
				// budget — 2026-07-02/03 "empty content (finish_reason=length)"
				// dream failures), reasoning headroom when no off-switch exists.
				extra, synthMax := dreamerLLMShape(*regPtr)
				s.wikiDreamer.SetLLMRequestShape(extra, synthMax)
				// Let dream cycles consume + curate the auto-recorded
				// workspace MEMORY.md (distill to wiki, keep a bounded buffer).
				s.wikiDreamer.SetWorkspaceDir(svcbind.WorkspaceDir())
				// Open loops are no longer auto-recorded as to-dos (operator approval
				// first) — no open-loop sink is wired (the dreamer skips it when nil).
				// Per-project latest-progress digests are written directly into each
				// project 대표페이지's "## 현재 상태" section by the dream cycle itself
				// (no sink — the dreamer owns the wiki store; see project_digest.go),
				// and kept fresh between cycles by the mail-analysis sink.
				// Mention-driven 인물 seeding from the contacts mirror.
				if cs := s.contactsStore; cs != nil {
					s.wikiDreamer.SetPersonDirectory(func() []domainbind.PersonSeed {
						all := cs.All()
						seeds := make([]domainbind.PersonSeed, 0, len(all))
						for _, c := range all {
							seeds = append(seeds, domainbind.PersonSeed{
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
func (s *Server) initToolsAndDeps(chatCfg *pipebind.HandlerConfig, reg *aibind.ModelRoleRegistry, transcriptStore pipebind.TranscriptStore, agentLogWriter *infrabind.Writer) {
	workspaceDir := svcbind.WorkspaceDir()

	// Out-of-workspace skill catalog roots: lets the read tool reach the SKILL.md
	// locations the skills index advertises (same roots the discovery walks;
	// workspace-local roots need no allowance). The repo's bundled skills/ root is
	// included so bundled SKILL.md bodies are readable too (not just listed).
	bundledSkillsDir := pipebind.BundledSkillsDir()
	skillCatalogDirs := []string{
		domainbind.DefaultManagedSkillsDir(),
		domainbind.DefaultPersonalSkillsDir(),
	}
	if bundledSkillsDir != "" {
		skillCatalogDirs = append(skillCatalogDirs, bundledSkillsDir)
	}

	// Notebook store: NotebookLM-style scoped source collections (딜/프로젝트
	// 브리핑). Lives under the gateway state dir; a failure just disables the
	// notebook tool (nil store), it does not block chat init. Promoted to the
	// server so the mail pipeline (fileDealFromMail) can auto-pin deal evidence.
	notebookDir := filepath.Join(infrabind.ResolveStateDir(), "notebooks")
	if ns, err := domainbind.NewNotebookStore(notebookDir); err != nil {
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
	chatCfg.Memory.Notebook = notebookStore

	s.toolDeps = &pipebind.CoreToolDeps{
		WorkspaceDir:      workspaceDir,
		SkillsCatalogDirs: skillCatalogDirs,
		BundledSkillsDir:  bundledSkillsDir,
		Process: pipebind.ProcessDeps{
			Mgr:          s.processes,
			WorkspaceDir: workspaceDir,
		},
		Sessions: pipebind.SessionDeps{
			Manager:              s.sessions,
			Transcript:           transcriptStore,
			SubagentDefaultModel: chatCfg.SubagentDefaultModel,
			CodingDefaultModel:   reg.FullModelID(aibind.RoleCoding),
			CodingDefaultModelFn: func() string {
				if reg == nil {
					return ""
				}
				return reg.FullModelID(aibind.RoleCoding)
			},
			// Powers sessions action=stats (per-session run/token roll-ups).
			AgentLog: adaptAgentLogStats(agentLogWriter),
		},
		Chrono: pipebind.ChronoDeps{
			Service: s.cronService,
			RunLog:  s.cronRunLog,
		},
		Wiki: pipebind.WikiDeps{
			Store: chatCfg.Memory.Wiki,
			// Same address book the contacts tool uses; lets a wiki write
			// auto-record a referenced person's contact details.
			Contacts: adaptContactsBook(s.contactsStore),
		},
		MailStore: s.mailStore,
		Notebook: pipebind.NotebookDeps{
			Store: notebookStore,
			// Pinned wiki-page sources are read live from the same store at
			// brief time, so a notebook briefing reflects the current page.
			Wiki: chatCfg.Memory.Wiki,
			// External source ingesters (url/mail/diary) — snapshot to text at
			// add time (notebook_sources.go). file (PDF/image OCR, text) is
			// handled in-package by the tool and needs no reader here.
			FetchURL:  chatwire.NotebookFetchURL,
			ReadMail:  chatwire.NotebookReadMail,
			ReadDiary: chatwire.NotebookReadDiary(chatCfg.Memory.Wiki),
		},
		Contacts: pipebind.ContactsDeps{
			// Created during registerEarlyMethods (no chat dep), so it's already
			// wired by the time chat init runs.
			Store: adaptContactsBook(s.contactsStore),
		},
		Calendar: pipebind.CalendarDeps{
			Client: adaptCalendarReaderFactory(platbind.DefaultCalendarClient),
			Local:  resolveToolLocalCalendar(s.logger),
		},
		// Deep-research panel fan-out: one prompt → every healthy wormhole-served
		// model in parallel (research_panel tool). nil-safe — the tool checks it.
		ConsultPanel: chatwire.NewConsultPanel(s.modelRegistry, s.logger),
		ObserveTool:  pipebind.ObserveToolFunc(toolbind.ToolObserve(s.logCapture, agentLogWriter, s.workFeedStore, reg.VllmBaseURLs)),
		// Deliver phone_write Intent actions (open_url/share/…) to the native app
		// over SSE for in-app execution — the SSH/Termux-free path.
		PhoneActionSender: s.dispatchPhoneAction,
		// Fleet management: the agent's twin of the /api/v1/fleet passthrough —
		// reaches the same SparkFleet control plane via s.fleet's accessors, so
		// "is the fleet ok / restart qwen36" works from pipebind. "" base = off.
		Fleet: pipebind.FleetDeps{BaseURL: s.fleet.BaseURL, Token: s.fleet.Token},
		// ASR proper-noun bias for the transcribe tool — the same wiki+contacts
		// hints the miniapp.capture.audio path merges (people/companies/deals).
		AsrHotwords: func() string {
			var parts []string
			if w := chatCfg.Memory.Wiki; w != nil {
				if h := w.HotwordHints(150); h != "" {
					parts = append(parts, h)
				}
			}
			if c := s.contactsStore; c != nil {
				if h := c.HotwordHints(100); h != "" {
					parts = append(parts, h)
				}
			}
			return strings.Join(parts, ", ")
		},
	}
	// Market tool shares the dashboard's quote cache (set in
	// registerEarlyMethods, which runs before chat init).
	if s.marketCache != nil {
		s.toolDeps.MarketSummary = adaptMarketSummary(s.marketCache.Summary)
	}
	// Workfeed tool mutates through the native-sync-teeing wrapper so agent
	// reads/acks mirror to the phone. Typed-nil guard: assigning a nil
	// *nativeWorkFeedStore directly would make the interface non-nil.
	if nw := s.nativeWorkFeedStore(); nw != nil {
		s.toolDeps.WorkFeedRW = workFeedRWAdapter{inner: nw}
	}

	// Ambient calendar awareness: a frozen-per-day upcoming-events glance in the
	// dynamic system-prompt block, built over the same hybrid calendar source as
	// the calendar tool. nil when no calendar source is wired (feature off).
	chatCfg.Ambient.CalendarGlance = pipebind.CalendarGlanceFunc(chatwire.NewCalendarGlance(&s.toolDeps.Calendar))
	chatCfg.Ambient.GoalGlance = pipebind.NewGoalGlanceFunc()
	chatCfg.ReportCardHealth = chatwire.ReportCardHealth
	chatCfg.LinkEnrichStart = pipebind.LinkEnrichStart(chatwire.NewLinkEnrichStart(s.logger))

	// Operator-edited 업무 persona (Settings prompt corner → prompt store). Returns
	// "" when unedited so the chat pipeline renders the default persona. Reading
	// the store keeps the chat package free of the prompt-store import.
	chatCfg.Ambient.PersonaOverride = s.personaOverride

	// Spillover store: saves large tool results to disk, replaces with preview.
	// Session-end events release per-session spill files immediately instead of
	// waiting for the 30-minute TTL sweep (see server_spillover_lifecycle.go).
	// Keyed by the STATE dir (not the home dir): prod resolves to ~/.deneb as
	// before, while dev/puppet instances (DENEB_STATE_DIR=/tmp/...) keep their
	// spill files out of the production store.
	{
		spillDir := filepath.Join(infrabind.ResolveStateDir(), "spillover")
		spillStore := aibind.NewSpilloverStore(spillDir)
		spillStore.StartCleanup(context.Background())
		s.toolDeps.SpilloverStore = spillStore
		s.initSpilloverLifecycle(spillStore)
	}

	// Semantic (vector) file search: opens the shared file store + BGE-M3 index
	// and wires s.toolDeps.FilesSemanticSearch. Must run before RegisterCoreTools
	// (the files tool captures the search closure at registration time). The
	// background reindex task is registered later in registerWorkflowSideEffects.
	s.fileSemindex = svcops.NewFileSemIndex(localFileStoreOrNil(s.logger), s.embeddingClient, s.logger)
	if s.fileSemindex != nil {
		s.toolDeps.FilesSemanticSearch = adaptFilesSemanticSearch(s.fileSemindex.Search)
	}

	// Core tools (file I/O, exec, process, sessions, gateway, cron, image).
	pipebind.RegisterCoreTools(chatCfg.Tools, s.toolDeps)

	// External MCP servers (Plaud recorder) as deferred tools — discovered in
	// the background so a slow npx cold start never blocks boot.
	s.externalMCP = chatwire.StartExternalMCP(s.ShutdownCtx(), chatCfg.Tools, s.logger)

	// Background services execute registered tools outside chat turns through
	// this handle (plaud_recordings.go polls the Plaud MCP tools).
	s.chatToolRegistry = chatCfg.Tools

	// Knowledge: unified recall/read/record surface over the wiki knowledge
	// base and the on-box file store. Polaris (session-bound) and graphify
	// (graph-traversal) stay separate because they have different paradigms.
	// Each adapter constructor returns nil when its backend is unavailable
	// (wiki store missing, or file index/embedding server down) → the router
	// simply drops that layer (knowledge.New ignores nil adapters), so recall
	// degrades gracefully to whatever backends are live.
	if chatwire.WireKnowledgeTool(chatCfg.Tools, s.wikiStore, s.fileSemindex) {
		// Recall preflight files source: surface relevant uploaded files as recall
		// evidence (injected into the last user message tail like wiki/diary/session).
		// nil when the file index/embedding server is unavailable → the preflight's
		// files source contributes nothing (graceful, recall unaffected). Set on the
		// config here (after initFileSemanticIndex) so NewHandler captures it.
		chatCfg.Memory.FileRecall = s.fileSemindex.Recall
	}

	// Org chart recall source: domainbind.Load reads {stateDir}/org.json independently of
	// the wiki store (a missing file yields an empty tree), so wire it
	// unconditionally — the recall source degrades to zero org evidence when the
	// chart is empty, and to org-context-without-person-links when the wiki store
	// is absent.
	chatCfg.Memory.Org = domainbind.Load

	// Polaris: retrieval tools for compressed conversation history.
	if bridge, ok := transcriptStore.(*pipebind.Bridge); ok {
		var localAI chatwire.LocalAIFunc
		if pipebind.LocalAIHub() != nil {
			localAI = func(ctx context.Context, system, user string, maxTokens int) (string, error) {
				return pipebind.CallLocalLLM(ctx, system, user, maxTokens)
			}
		}
		chatwire.RegisterPolarisTools(chatCfg.Tools, bridge.Store(), localAI)

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
func formatRecentPolarisSummaries(nodes []pipebind.SummaryNode) string {
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

// dreamerLLMShape computes the request shaping for the wiki dreamer's raw
// lightweight-role LLM calls. The dreamer talks to the registry client
// directly — it goes through neither the pilot/localai hub (which injects
// enable_thinking=false for non-reasoning models, localai.mergeRequestBody)
// nor the chat effort router (which attaches the dual-mode thinking toggle,
// run_capability.go) — so this helper reproduces both policies at wiring time:
//
//   - reasoning model with a template off-switch (deepseek-v4 →
//     chat_template_kwargs.thinking=false): disable thinking. The dreamer's
//     JSON-extraction calls gain nothing from chain-of-thought, and with it on
//     the synthesis budget was fully consumed by reasoning (the 2026-07-02/03
//     empty-content failures).
//   - reasoning model with no off-switch: budget for the thinking instead —
//     observed dsv4 reasoning runs ~13K chars (≈4K tokens), so 4x headroom.
//   - non-reasoning model: send enable_thinking=false (Qwen-family templates
//     default thinking on).
func dreamerLLMShape(reg *aibind.ModelRoleRegistry) (extraBody map[string]any, synthesisMaxTokens int) {
	if reg == nil {
		return nil, 0
	}
	cfg := reg.Config(aibind.RoleLightweight)
	// Shared three-way, registry-aware so deneb.json routing.toggleKwarg
	// overrides shape the dreamer like they shape foreground turns (see
	// aibind.ThinkingOffDirectiveFor).
	if directive := reg.ThinkingOffDirectiveFor(cfg.ProviderID, cfg.Model); directive != nil {
		return map[string]any{
			"chat_template_kwargs": map[string]any{directive.TemplateKwarg(): false},
		}, 0
	}
	// nil kwargs + a reasoning model = untoggleable chain-of-thought: the
	// only defense is budgeting reasoning + answer. The dreamer scales its
	// non-synthesis calls by the same 4x (llmRequest). Non-reasoning models
	// with nil kwargs (direct cloud providers) need no headroom. Reasoning is
	// checked registry-aware, not builtin-only: a deneb.json provider entry
	// declaring reasoning:true (a model the builtin prefix table doesn't
	// know) must get the same headroom.
	if aibind.IsReasoningModel(cfg.Model) || reg.CapabilityForModel(cfg.ProviderID, cfg.Model).Reasoning {
		return nil, 16384
	}
	return nil, 0
}
