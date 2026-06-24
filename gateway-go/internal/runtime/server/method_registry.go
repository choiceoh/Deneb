// Centralized RPC method registration via GatewayHub.
//
// Replaces 18 register* wrapper methods with two functions:
//   - registerEarlyMethods: ~30 domains that don't need chatHandler
//   - registerLateMethods:  ~4 domains that depend on chatHandler
//
// Deps structs are assembled inline from hub accessors — no adapter layer.
// Handlers still accept their own Deps structs (testability preserved);
// only this file knows about the hub→Deps mapping.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/mailpriority"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlercheckpoint "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/checkpoint"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	handlerinsights "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/insights"
	handlerobserve "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observe"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
	handlerwiki "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// errWikiDisabled surfaces from the miniapp memory factory when the wiki
// knowledge base is not configured. Returning a real error (rather than
// nil store) keeps the rpc handler's UNAVAILABLE branch typed and lets
// the operator see a meaningful message in the response.
var errWikiDisabled = errors.New("wiki knowledge base not configured")

// errTranscriptUnavailable surfaces when the miniapp sessions.transcript
// factory is called before chat init has populated s.toolDeps. Treated as
// UNAVAILABLE by the handler.
var errTranscriptUnavailable = errors.New("session transcript store not initialized")

// errCronUnavailable surfaces from the miniapp crons factory when the
// cron service hasn't been wired (e.g., a gateway started without the
// cron subsystem). Treated as UNAVAILABLE by the handler so the Mini
// App shows a "automation not configured" banner instead of crashing.
var errCronUnavailable = errors.New("cron service not configured")

// errNotebookDisabled surfaces from the miniapp notebook factory when the
// notebook store failed to initialize. Treated as UNAVAILABLE by the handler.
var errNotebookDisabled = errors.New("notebook store not configured")

// wikiSenderFacts resolves "who is this person to us" in-process from the wiki
// graph — used by the analyze pipeline and the sender_context card. Returns ""
// when the wiki is unconfigured or nothing matches, so callers fall back
// cleanly (to graphify, or to an empty card).
func (s *Server) wikiSenderFacts(ctx context.Context, displayName string) string {
	if s.wikiStore == nil {
		return ""
	}
	facts, err := s.wikiStore.GraphContext(ctx, displayName, 0)
	if err != nil {
		return ""
	}
	return facts
}

// registerEarlyMethods registers all RPC domains that don't depend on chatHandler.
// Called after buildHub() but before registerSessionRPCMethods().
func (s *Server) registerEarlyMethods(hub *rpcutil.GatewayHub, denebDir string) error {
	hub.AdvancePhase(rpcutil.PhaseEarly)

	// Fail fast if core hub fields are missing.
	if err := hub.Validate(); err != nil {
		return fmt.Errorf("server init: hub validation: %w", err)
	}

	// Create the insights engine. Read-only — aggregates session manager
	// snapshots and usage tracker state. Stored on both the hub (for RPC
	// handlers) and the server (so the chat dispatcher can wire /insights).
	insightsEngine := insights.New(hub.Sessions(), s.usageTracker)
	hub.SetInsights(insightsEngine)
	s.insights = insightsEngine

	// Device address book mirror (native-client contacts sync) — no chat dependency,
	// so it's created here in the early phase and is ready by the time chat init wires
	// the contacts tool. nil-tolerant: a load failure just disables the store write
	// (the contacts tool / save path degrade to "unavailable" cleanly).
	if cs, err := contacts.NewStore(filepath.Join(denebDir, "contacts.json")); err != nil {
		s.logger.Warn("contacts store init failed; contacts sync disabled", "error", err)
	} else {
		s.contactsStore = cs
		hub.SetContactsStore(cs)
	}
	s.nativeSyncStore = nativesync.NewStore(filepath.Join(denebDir, "native_sync.jsonl"))
	s.workFeedStore = workfeed.NewStore(filepath.Join(denebDir, "workfeed.jsonl"))
	nativeWorkFeed := s.nativeWorkFeedStore()

	// Mirror local-calendar mutations onto the native-sync stream so the client
	// refetches its calendar promptly instead of waiting out its background warm
	// throttle. localcal.Store is the single choke point every mutation path funnels
	// through (the client's RPC, the agent calendar tool, mail-proposal accept,
	// cron), so one observer on the process-wide store covers them all. Set-once,
	// nil-tolerant: a store-load failure just leaves the client on its warm cadence.
	if calStore, err := localcal.Default(); err == nil && calStore != nil {
		calStore.SetChangeObserver(func(eventID string) {
			if _, err := s.nativeSyncStore.Append(nativesync.CalendarChanged(eventID)); err != nil {
				s.logger.Error("native sync: calendar event append failed", "eventID", eventID, "error", err)
			}
		})
	}

	// FCM push fallback (proactive delivery when the app is fully closed / Doze
	// and no live SSE client is connected). The device-token store is always
	// created so registration RPCs work and tokens accumulate; the sender is
	// dormant until a service-account file is configured (DENEB_FCM_CREDENTIALS_FILE).
	s.pushTokenStore = push.NewStore(filepath.Join(denebDir, "push_tokens.json"))
	if fcmCfg := push.ConfigFromEnv(); fcmCfg.Enabled() {
		if sender, err := push.NewFCMSender(fcmCfg); err != nil {
			s.logger.Error("FCM push: credentials load failed; proactive FCM fallback disabled", "error", err)
		} else {
			s.pushNotifier = push.NewNotifier(push.NotifierDeps{
				Store:  s.pushTokenStore,
				Sender: sender,
				Logger: s.logger,
				Broadcast: func(event string, payload any) {
					s.broadcaster.Broadcast(event, payload)
				},
				ShutdownCtx: s.ShutdownCtx(),
			})
			s.logger.Info("FCM push fallback enabled", "projectID", sender.ProjectID())
		}
	}

	// Monitoring notify service (error mirrors + status snapshots → native push).
	s.notify = newNotifyService(hub.Sessions(), hub.Logger(), s.pushHub, s.BoundAddr)
	if s.notify != nil {
		s.broadcaster.RegisterTap(s.notify.tap)
		s.notify.start(s.ShutdownCtx())
	}

	// Observation-plane deps, shared verbatim by the in-process observe.* and
	// the remote miniapp.observe.* registrations below. AgentLog and VllmBases
	// are getters because the agentlog writer and the model registry are both
	// constructed later (session phase); resolving lazily avoids capturing nil.
	observeDeps := handlerobserve.Deps{
		Capture:  s.logCapture,
		AgentLog: func() *agentlog.Writer { return s.agentLogWriter },
		VllmBases: func() []string {
			if s.modelRegistry == nil {
				return nil
			}
			return s.modelRegistry.VllmBaseURLs()
		},
		Logger: hub.Logger(),
	}

	// Table-driven domain registration: one slice, one loop.
	// Deps assembled inline from hub accessors — no adapter layer.
	domains := []map[string]rpcutil.HandlerFunc{
		// --- Session CRUD (list/get/delete) ---
		handlersession.CRUDMethods(handlersession.Deps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
			// Lazy: the transcript store exists only after chat init (between
			// early and late phase). sessions.delete must remove the .jsonl or
			// the startup restore resurrects the session.
			Transcripts: func() (handlersession.TranscriptDeleter, error) {
				if s.toolDeps == nil || s.toolDeps.Sessions.Transcript == nil {
					return nil, errTranscriptUnavailable
				}
				return s.toolDeps.Sessions.Transcript, nil
			},
		}),

		// --- Health and system info ---
		handlersystem.HealthMethods(handlersystem.HealthDeps{
			SessionCount: hub.Sessions().Count,
			Version:      hub.Version(),
		}),

		// --- Agent orchestration ---
		handleragent.ExtendedMethods(handleragent.ExtendedDeps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
			Processes:   hub.Processes(),
			CronService: hub.CronService(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.ACPMethods(s.acpDeps),

		// --- Tools and skills ---
		handlerskill.ToolMethods(handlerskill.ToolDeps{Processes: hub.Processes()}),
		handlerskill.Methods(handlerskill.Deps{
			Skills:      hub.Skills(),
			Broadcaster: hub.Broadcast,
		}),

		// --- Events (transport-agnostic) ---
		handlerevents.BroadcastMethods(handlerevents.EventsDeps{
			Broadcaster: hub.Broadcaster(),
			Logger:      hub.Logger(),
		}),
		handlerevents.EventsMethods(handlerevents.EventsDeps{
			Broadcaster: hub.Broadcaster(),
			Logger:      hub.Logger(),
		}),

		// --- Scheduling ---
		handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
			Service:     hub.CronService(),
			RunLog:      hub.CronPersistLog(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronService()}),

		// --- Background task control plane ---

		// --- System ---
		handlersystem.IdentityMethods(hub.Version()),
		handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{
			ChannelHealth: s.channelHealth,
			Dispatcher:    s.dispatcher,
		}),
		handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{
			Broadcaster: hub.Broadcast,
		}),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),

		// --- Observation plane (unified: log ring + turn shape + behavior) ---
		handlerobserve.Methods(observeDeps),

		// --- Insights (usage reports) ---
		handlerinsights.Methods(handlerinsights.Deps{
			Engine: hub.Insights(),
			Logger: hub.Logger(),
		}),

		// --- Checkpoint (list/restore/diff backing /rollback) ---
		// Root is derived from the resolved state dir. When denebDir is
		// empty the handler still registers but replies UNAVAILABLE.
		handlercheckpoint.Methods(handlercheckpoint.Deps{
			Root:   filepath.Join(denebDir, "checkpoints"),
			Logger: hub.Logger(),
		}),
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),

		// --- Native client miniapp.* RPC (HTTP-exposed via /api/v1/miniapp/rpc) ---
		// Requires client-token auth, enforced by the HTTP bridge in
		// server_http_miniapp.go before the dispatcher is reached. The
		// methods read the authenticated operator from context via
		// clientauth.FromContext.

		// Observation plane under miniapp.observe.* — the same handlers as the
		// in-process observe.* above, exposed here so remote adapters (native
		// dashboard, token-holding external CLI) can reach logs/turns/behavior.
		// The miniapp.* gate is exactly the client-token boundary we want.
		handlerobserve.MiniappMethods(observeDeps),
		handlerminiapp.Methods(handlerminiapp.Deps{
			Version: hub.Version(),
			CurrentModel: func() string {
				// Lazy: chatHandler / modelRegistry are populated after this
				// registration phase. Resolve at request time.
				if s.chatHandler != nil {
					if m := s.chatHandler.DefaultModel(); m != "" {
						return m
					}
				}
				if s.modelRegistry != nil {
					return s.modelRegistry.FullModelID(modelrole.RoleMain)
				}
				return ""
			},
			Capabilities: func() map[string]bool {
				wikiReady := hub.WikiStore() != nil
				chatReady := s.chatHandler != nil
				return map[string]bool{
					"rpc":             true,
					"chat":            chatReady,
					"chatStream":      chatReady,
					"events":          s.pushHub != nil,
					"models":          s.modelRegistry != nil,
					"gmail":           true,
					"calendar":        true,
					"wiki":            wikiReady,
					"search":          wikiReady,
					"people":          true,
					"crons":           hub.CronService() != nil,
					"captureImage":    chatReady,
					"captureAudio":    chatReady,
					"captureContacts": hub.ContactsStore() != nil,
					"workFeed":        s.workFeedStore != nil,
					"workFeedActions": s.workFeedStore != nil,
					"nativeSync":      s.nativeSyncStore != nil,
					"pushRegister":    s.pushTokenStore != nil,
					"pushFallback":    s.pushNotifier != nil,
					"gmailAttachment": true,
					"updateManifest":  true,
					"prompts":         s.promptStore != nil,
					"promptTuner":     s.compactTuner != nil,
					// topicDocs gates the native single-topic background editor.
					// True only when a current topic key resolves (topics.map
					// has a "0" entry) — i.e. there is actually a doc to edit
					// that injects into the prompt.
					"topicDocs": resolveCurrentTopicKey() != "",
				}
			},
		}),
		handlerminiapp.SyncMethods(handlerminiapp.SyncDeps{
			Store: s.nativeSyncStore,
		}),
		// Native-client FCM device-token registration. Always available (tokens
		// accumulate even before the FCM sender is configured); the proactive
		// fallback that consumes them is wired separately via s.pushNotifier.
		handlerminiapp.PushMethods(handlerminiapp.PushDeps{
			Store: s.pushTokenStore,
		}),
		// Wormhole router status + feature toggles (config path / URL resolved
		// from env, defaulting to the on-host single-machine layout).
		handlerminiapp.WormholeMethods(handlerminiapp.WormholeDeps{}),
		handlerminiapp.WorkFeedMethods(handlerminiapp.WorkFeedDeps{
			Store: nativeWorkFeed,
			// Record a deal-question card's team answer onto the deal wiki page
			// (불확실 → 질문 → 기록). See deal_question.go.
			OnAnswer: s.recordDealQuestionAnswer,
			DeliverAnswer: func(ctx context.Context, sessionKey, answer string) (string, string, error) {
				if s.chatHandler == nil {
					return "", "", errors.New("chat handler not initialized")
				}
				target := handlerchat.DefaultSessionKey(sessionKey)
				res, err := s.chatHandler.SendSync(ctx, target, answer, "", &chat.SyncOptions{
					Delivery:            &chat.DeliveryContext{Channel: handlerchat.NativeClientChannel, To: target},
					AutoDeliveredOutput: true,
					GateUntrustedTools:  true,
				})
				if err != nil {
					return "", "", err
				}
				return res.BestText(), res.Model, nil
			},
		}),
		s.miniappModelMethods(),

		// Native local file browser (miniapp.files.{list,search,share,upload}):
		// list/search/share/upload over the on-box file store (filestore). share
		// mints a signed download link (fileshare); a nil store (open error)
		// skips the domain.
		handlerminiapp.FilesBrowseMethods(handlerminiapp.FilesBrowseDeps{
			Store: localFileStoreOrNil(s.logger),
			// Content search (search content=true) extracts file text via the chat
			// tools' document extractor. Wired here (server layer may import tools);
			// the handler keeps it as an injected callback to avoid a layer inversion.
			ExtractText: func(ctx context.Context, d []byte, n string) string {
				t, _ := tools.ExtractDocumentText(ctx, d, n, "")
				return t
			},
			// Semantic search (search semantic=true) ranks by meaning via the shared
			// BGE-M3 file index. A lazy closure: the index + embedding client are
			// created later in initToolsAndDeps (this wiring runs in the early phase),
			// and requests arrive well after boot, so reading them at call time is
			// safe. Returns empty (→ name/content fallback) when the index/embedding
			// server is unavailable.
			SemanticSearch: func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
				return s.fileSemanticSearch(ctx, query, max)
			},
			// Keep the semantic index fresh after a delete/move so search doesn't
			// hand back a stale path between 15-min reindex passes. Lazy like
			// SemanticSearch (the index is created later in initToolsAndDeps).
			OnDelete: s.fileIndexRemove,
			OnMove:   s.fileIndexRename,
		}),

		// Native mail domain. The RPC namespace stays miniapp.gmail.* for
		// client compatibility, but the server now prefers the on-box archive
		// repository and keeps Gmail as a fallback for legacy queries/tokens.
		handlerminiapp.GmailMethods(handlerminiapp.GmailDeps{
			Client: s.miniappMailClientFactory(denebDir),
			// Same per-msgID cache directory the analyze handler/poller
			// write to (the store is a stateless dir wrapper) — list rows
			// prefer its LLM verdict over the heuristic below.
			AnalysisCache: handlerminiapp.NewAnalysisStore(filepath.Join(denebDir, "cache", "mail_analysis")),
			WorkState:     mailwork.New(filepath.Join(denebDir, "mail_work_state.json")),
			// Row priority: cheap local heuristics + address-book VIP
			// lookup. contactsStore is created above in this same
			// registration pass; a nil store just drops the VIP signal.
			Priority: func(from, subject, snippet string) (string, string) {
				tier, hint := mailPriorityScorer(s.contactsStore).Score(from, subject, snippet)
				return string(tier), hint
			},
		}),

		// Mini App Calendar domain. Hybrid: a read-only Google client (lazy
		// factory, like Gmail — gateway boots without OAuth tokens; reads
		// return UNAVAILABLE only when no local store either) plus a local
		// store ({stateDir}/calendar.json) that holds hand-added events, so
		// create/edit/delete work without a Google write scope.
		handlerminiapp.CalendarMethods(handlerminiapp.CalendarDeps{
			Client: func() (handlerminiapp.CalendarClient, error) {
				return calendar.DefaultClient()
			},
			Local:     resolveLocalCalendar(s.logger),
			Proposals: resolveCalendarProposals(s.logger),
		}),

		// Mini App part-status dashboard (miniapp.dashboard.lanes). Groups work
		// items (calendar + work feed in this 1차 scope) into the operator's
		// managed parts via the rule-based classifier. The classifier roster is
		// loaded from {stateDir}/classification_rules.json (operator data, never
		// in the repo); each source is nil-tolerant so a down Google calendar or
		// absent feed degrades that lane only, not the whole dashboard.
		handlerminiapp.DashboardMethods(s.dashboardDeps()),

		// Mini App project digests (miniapp.project.digests). Each active
		// project's latest-progress digest lives ON its 대표페이지 (프로젝트/<name>.md)
		// "## 현재 상태" section — written by the dream cycle (LLM roll-up) and kept
		// fresh by mail analysis (dated bullets). This reads those sections from the
		// wiki store; no LLM on the read path, so the screen loads instantly. Lazy
		// factory (wiki is late-bound in the session phase) — UNAVAILABLE when wiki
		// is disabled, exactly like the memory factory.
		handlerminiapp.ProjectMethods(handlerminiapp.ProjectDeps{
			Wiki: func() (handlerminiapp.ProjectStatusSource, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
		}),

		// Mini App org chart editor (miniapp.org.{get,save}). The org chart
		// ({stateDir}/org.json, operator data, never in the repo) is the MASTER
		// source for the dashboard's part classification — its lane-tagged nodes
		// define the dashboard columns and seed the classification rules. get
		// returns the tree (missing file → empty); save validates (unique ids,
		// existing parents, no cycles, unique lane keys) then atomically writes.
		handlerminiapp.OrgMethods(s.orgDeps()),

		// Mini App To-do domain (miniapp.todo.*). The task-list companion to
		// the calendar, backed by a local store ({stateDir}/todos.json). No
		// external provider — every method writes locally, so it works without
		// any OAuth scope. Skipped if the store file can't be read.
		handlerminiapp.TodoMethods(handlerminiapp.TodoDeps{
			Store: resolveLocalTodos(s.logger),
		}),

		// Mini App memory search (miniapp.memory.search). Lazy factory
		// around hub.WikiStore() — wiki is created in the late phase
		// (registerLateMethods) so the factory is what defers the lookup
		// to per-request, by which time the store is wired. When wiki
		// is disabled by config the factory surfaces UNAVAILABLE.
		handlerminiapp.MemoryMethods(handlerminiapp.MemoryDeps{
			Store: func() (handlerminiapp.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
			// Background worker for miniapp.memory.merge — synthesizes the
			// combined body (lightweight model), runs the structural merge,
			// then notifies the home chat. Off the request path so the Mini
			// App never blocks on a slow/down model.
			StartMerge: s.makeWikiMergeStarter(hub),
		}),

		// Mini App notebook read surface (miniapp.notebook.*). Lazy factory
		// around s.notebookStore (set in the late chat-init phase); deferring
		// the lookup to per-request means the store is wired by the first RPC.
		// A gateway whose notebook store failed to init gets a clean
		// UNAVAILABLE per call instead of a boot crash.
		handlerminiapp.NotebookMethods(handlerminiapp.NotebookDeps{
			Store: func() (*notebook.Store, error) {
				if s.notebookStore == nil {
					return nil, errNotebookDisabled
				}
				return s.notebookStore, nil
			},
		}),

		// Mini App cron job list (miniapp.crons.list). Same lazy-factory
		// pattern as memory: cron.Service is wired during buildHub so by
		// the time the first RPC fires the service is ready, but a
		// gateway started with the cron subsystem disabled still gets a
		// clean UNAVAILABLE per call instead of a crash at boot.
		handlerminiapp.CronsMethods(handlerminiapp.CronsDeps{
			Service: func() (handlerminiapp.CronService, error) {
				svc := hub.CronService()
				if svc == nil {
					return nil, errCronUnavailable
				}
				return svc, nil
			},
		}),
		handlerminiapp.PromptMethods(handlerminiapp.PromptDeps{
			Store: s.promptStore,
		}),
		handlerminiapp.PromptTunerMethods(handlerminiapp.PromptTunerDeps{
			Tuner: func() handlerminiapp.PromptTuner {
				return s.compactTuner
			},
		}),
		// Mini App single-topic background editor (miniapp.topicdocs.*).
		// Edits <workspace>/topics/<key>.md for the current topic key, the
		// same file prompt.LoadTopicKnowledge injects into the Static block.
		// Resolved per call (dir + "0"-key) so a config change applies without
		// a restart; both factories are nil-tolerant so when topics are
		// unconfigured the handler responds UNAVAILABLE rather than 404.
		// applyNow drops the frozen topic snapshots so an edit lands this
		// session (the RPC analog of slash "--now"); the default is deferred
		// (next-session) to keep the Static prompt cache stable.
		handlerminiapp.TopicDocsMethods(handlerminiapp.TopicDocsDeps{
			TopicsDir:  func() (string, error) { return resolveTopicsDir(), nil },
			CurrentKey: resolveCurrentTopicKey,
			ApplyNow:   prompt.Cache.ClearAllTopicSnapshots,
		}),

		// Mini App sessions recent + transcript (miniapp.sessions.*).
		// Transcripts is a lazy factory that reaches into s.toolDeps
		// once chat init runs (between early and late phase) so it is
		// safe to register here; calls before chat init resolve to
		// UNAVAILABLE which is fine for boot-time noise.
		handlerminiapp.SessionsMethods(handlerminiapp.SessionsDeps{
			Manager: hub.Sessions(),
			Transcripts: func() (handlerminiapp.TranscriptLoader, error) {
				if s.toolDeps == nil || s.toolDeps.Sessions.Transcript == nil {
					return nil, errTranscriptUnavailable
				}
				return s.toolDeps.Sessions.Transcript, nil
			},
		}),

		// Mini App Gmail sender context (miniapp.gmail.sender_context).
		// Combines Gmail recent-activity query, wiki memory lookup, and
		// wiki-graph traversal (graphify CLI) so the Mini App detail
		// view can show a contextual sender card.
		handlerminiapp.GmailContextMethods(handlerminiapp.GmailContextDeps{
			Client: func() (handlerminiapp.GmailClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: func() (handlerminiapp.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
			// In-process wiki graph first (always current); fall back to the
			// external graphify snapshot only when nothing matches in-process.
			SenderFacts: func(ctx context.Context, from string) string {
				if f := s.wikiSenderFacts(ctx, from); f != "" {
					return f
				}
				return gmailpoll.ExtractSenderFacts(ctx, from)
			},
		}),

		// Mini App people directory (miniapp.people.list). Same Gmail
		// lazy-client pattern; aggregates a single Search call into a
		// frequency-sorted counterparty list, then folds in 인물 wiki
		// pages (best-effort — wiki disabled degrades to Gmail-only).
		handlerminiapp.PeopleMethods(handlerminiapp.PeopleDeps{
			Client: func() (handlerminiapp.PeopleClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: func() (handlerminiapp.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
		}),

		// Mini App full address-book list (miniapp.contacts.list): the raw
		// contacts.json mirror for the native 전체 연락처 browser, sectioned
		// alphabetically on the client. Distinct from people.list (ranked Gmail
		// counterparties). UNAVAILABLE when the store isn't configured.
		handlerminiapp.ContactsMethods(handlerminiapp.ContactsDeps{
			Store: func() (*contacts.Store, error) {
				cs := hub.ContactsStore()
				if cs == nil {
					return nil, errors.New("contacts store not configured")
				}
				return cs, nil
			},
		}),

		// Mini App skills list/detail/write surface + Propus feed
		// (miniapp.skills.*). Catalog for the Settings → Skills tab, guarded
		// update/delete for mutable local skills, plus the genesis → review →
		// evolve timeline. Uses the same archived + eligibility filtering as
		// the system prompt
		// (chat.EligibleWorkspaceSkills), so the tab advertises only skills
		// the agent can actually use. The tracker projections read
		// s.genesisTracker lazily (it is wired after early registration) and
		// are nil-tolerant so the tab degrades to an un-enriched list when
		// the tracker is unavailable.
		handlerminiapp.SkillsMethods(handlerminiapp.SkillsDeps{
			List: func() []skills.SkillEntry {
				// chatHandler (and its tool registry) is ready by the time this
				// runs — the RPC fires long after boot wires the chat pipeline.
				// Pass the live toolset so requires_tools eligibility matches the
				// prompt and slash routing.
				var toolNames []string
				if s.chatHandler != nil {
					toolNames = s.chatHandler.ToolNames()
				}
				return chat.EligibleWorkspaceSkills(resolveWorkspaceDir(), toolNames)
			},
			CuratorRecords: func() ([]genesis.SkillCuratorRecord, error) {
				if s.genesisTracker == nil {
					return nil, nil
				}
				return s.genesisTracker.SkillCuratorReport("")
			},
			UsageStats: func() ([]genesis.UsageStats, error) {
				if s.genesisTracker == nil {
					return nil, nil
				}
				return s.genesisTracker.ListAllStats()
			},
			RecentLifecycle: func(limit int) ([]genesis.LifecycleLogEntry, error) {
				if s.genesisTracker == nil {
					return nil, nil
				}
				return s.genesisTracker.RecentLifecycleLog(limit)
			},
			ValidationSummary: func(skillName string) (genesis.SkillValidationCaseSummary, error) {
				if s.genesisTracker == nil {
					return genesis.SkillValidationCaseSummary{SkillName: strings.TrimSpace(skillName)}, nil
				}
				return s.genesisTracker.ValidationCaseSummary(strings.TrimSpace(skillName))
			},
			RecentOpportunities: func(skillName string, limit int) ([]genesis.SkillOpportunityRecord, error) {
				if s.genesisTracker == nil {
					return nil, nil
				}
				return s.genesisTracker.RecentSkillOpportunities(strings.TrimSpace(skillName), limit)
			},
			RecentSelfCorrections: func(skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
				if s.genesisTracker == nil {
					return nil, nil
				}
				return s.genesisTracker.RecentSelfCorrectionCandidates(strings.TrimSpace(skillName), genesis.SelfCorrectionStatusProposed, limit)
			},
			SelfHarnessSignals: func() genesis.SelfHarnessSignalSummary {
				if s.genesisTracker == nil {
					return genesis.SelfHarnessSignalSummary{}
				}
				return s.genesisTracker.SelfHarnessSignals()
			},
			InvalidateSkills: chat.InvalidateSkillsCache,
		}),

		// Mini App self-improvement coding queue. This is not a skill list and
		// not a Propus event log: it is the deferred coding-correction backlog
		// that an AI coding agent should batch-review before applying changes.
		handlerminiapp.SelfImprovementCodingMethods(handlerminiapp.SelfImprovementCodingDeps{
			RecentCandidates: func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
				if s.genesisTracker == nil {
					return nil, nil
				}
				return s.genesisTracker.RecentSelfCorrectionCandidates("", status, limit)
			},
		}),

		// Mini App unified search (miniapp.search.all). Single entry
		// point that fans out to wiki + diary + people in parallel.
		// Replaces the per-domain home menu entries — there's now one
		// search input on home that returns three result sections.
		// Either factory may be unavailable; the handler degrades
		// gracefully (Gmail-disabled gateway still serves wiki+diary).
		handlerminiapp.SearchMethods(handlerminiapp.SearchDeps{
			Store: func() (handlerminiapp.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
			Client: func() (handlerminiapp.PeopleClient, error) {
				return gmail.DefaultClient()
			},
		}),
	}

	// Conditional: provider methods.
	if s.providers != nil {
		domains = append(domains,
			handlerprovider.Methods(handlerprovider.Deps{
				Providers:   s.providers,
				AuthManager: s.authManager,
			}),
			handlerprovider.ModelsMethods(handlerprovider.ModelsDeps{
				Providers: s.providers,
			}),
		)
	}

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}

	// Special-case registrations with embedded business logic.
	s.registerConfigLifecycleMethods()
	return nil
}

// registerLateMethods registers RPC domains that depend on chatHandler.
// Called after registerSessionRPCMethods() which creates the chat handler.
func (s *Server) registerLateMethods(hub *rpcutil.GatewayHub) {
	hub.AdvancePhase(rpcutil.PhaseLate)
	hub.SetChat(s.chatHandler)
	hub.SetWikiStore(s.wikiStore) // late-bound: created during session phase

	domains := []map[string]rpcutil.HandlerFunc{
		handlerchat.Methods(handlerchat.Deps{
			Chat:        hub.Chat(),
			Broadcaster: hub.Broadcast,
		}),
		handlerchat.BtwMethods(handlerchat.BtwDeps{
			Chat:        hub.Chat(),
			Broadcaster: hub.Broadcast,
		}),
		// Native-client chat bridge (miniapp.chat.send/history): lets the
		// standalone app drive a turn over the miniapp.* RPC surface via
		// SendSync, with deneb-ui emission enabled (channel "client").
		handlerchat.MiniappMethods(handlerchat.Deps{
			Chat:       hub.Chat(),
			OcrImage:   tools.OcrImageBytes,
			Transcribe: tools.TranscribeAudio,
			// Document attach (pdf/doc/sheet) → in-house extractor (PDF/Excel/Word/
			// PowerPoint/CSV/text, with a scanned-PDF / image OCR fallback).
			ExtractDocument: tools.ExtractAttachmentTextBytes,
			// In-app browser in-place translation (en/ru → ko) via the translation
			// model role (defaults to local lightweight; agents.translationModel opts in).
			Translate: tools.TranslateSegments,
			// Raw capture persistence: full OCR text / diarized transcript →
			// {memory}/captures/ + diary breadcrumb (recallable, dream-distilled,
			// backed up). The agent turn only summarizes; this keeps the original.
			SaveCapture: func(kind, context, text string) (string, error) {
				ws := hub.WikiStore()
				if ws == nil {
					return "", fmt.Errorf("wiki store unavailable")
				}
				return ws.SaveCapture(kind, context, text)
			},
			// Proper-noun bias for audio transcription, merged from two sources:
			// the wiki (people/companies/deals/domain terms) and the contacts
			// address book (every saved name + org). Either may be empty.
			Hotwords: func() string {
				var parts []string
				if ws := hub.WikiStore(); ws != nil {
					if h := ws.HotwordHints(150); h != "" {
						parts = append(parts, h)
					}
				}
				if cs := hub.ContactsStore(); cs != nil {
					if h := cs.HotwordHints(100); h != "" {
						parts = append(parts, h)
					}
				}
				return strings.Join(parts, ", ")
			},
			// Primary contacts sync: persist the whole address book into the
			// contacts store (phone lookup / name search / ASR hotwords).
			SaveContacts: func(contactsJSON []byte) (int, error) {
				cs := hub.ContactsStore()
				if cs == nil {
					return 0, fmt.Errorf("contacts store unavailable")
				}
				var p struct {
					Contacts []contacts.Contact `json:"contacts"`
				}
				if err := json.Unmarshal(contactsJSON, &p); err != nil {
					return 0, err
				}
				return cs.ReplaceAll(p.Contacts)
			},
			// Bonus: enrich existing wiki people (native-client contacts sync).
			// Enriches only 사람 pages already in the wiki — it creates none — so
			// the phone book strengthens the curated set without flooding it.
			EnrichContacts: func(contactsJSON []byte) (wiki.ContactEnrichResult, error) {
				ws := hub.WikiStore()
				if ws == nil {
					return wiki.ContactEnrichResult{}, fmt.Errorf("wiki store unavailable")
				}
				return ws.EnrichContacts(contactsJSON)
			},
			WorkFeed:    s.nativeWorkFeedStore(),
			IngestEvent: s.ingestPhoneEventAsync,
		}),
		handlersession.ExecMethods(handlersession.ExecDeps{
			Chat:       hub.Chat(),
			JobTracker: hub.JobTracker(),
		}),
		// --- Wiki knowledge base (feature-flagged, late-bound) ---
		handlerwiki.Methods(handlerwiki.Deps{
			Store: hub.WikiStore(),
		}),

		// --- Skill genesis (depends on chatHandler for LLM client) ---
		handlerskill.GenesisMethods(handlerskill.GenesisDeps{
			Genesis:     s.genesisSvc,
			Evolver:     s.genesisEvolver,
			Tracker:     s.genesisTracker,
			Transcripts: s.genesisTranscripts,
		}),

		// --- Mini App email analysis (miniapp.gmail.analyze) ---
		// Late-bound because the analyzer needs a configured LLM client
		// from the model registry, which is wired during memory subsystem
		// init right before this phase. Lazy factory still — operator
		// runs without any provider configured, the call returns
		// UNAVAILABLE rather than crashing the gateway.
		handlerminiapp.GmailAnalyzeMethods(handlerminiapp.GmailAnalyzeDeps{
			// Archive-first client — the same factory the native mail list/detail
			// surface uses. Mail now arrives via LMTP and lives in the on-box
			// archive keyed by RFC822 Message-ID. The old gmail.DefaultClient()
			// fetched by Gmail-API message id, so "🔄 다시 분석" on an archived mail
			// handed an archive id (…@amazonses.com) to the Gmail API → HTTP 400
			// "Invalid id value". The repository resolves the archive first and only
			// falls back to Gmail for legacy/disabled setups.
			Client: s.miniappMailClientFactory(s.denebDir),
			Pipeline: func() (handlerminiapp.AnalyzePipeline, error) {
				// Role selection is shared with the autonomous poller via
				// mailAnalysisModels (stage-2 = analysis role, stage-1 = tiny
				// role) so the two mail-analysis paths cannot drift apart.
				// This replaces a #1816-era pin to the fallback role
				// ("step3.7 streams unstoppable thinking") that the poller
				// has since disproven — the pipeline disables thinking and
				// scrubs reasoning leaks — and that broke the interactive
				// button alone when the fallback provider's key died (401,
				// 2026-06-10).
				llmClient, model, localClient, localModel := s.mailAnalysisModels()
				if llmClient == nil {
					return nil, handlerminiapp.ErrAnalyzeNoLLM
				}
				gmailClient, err := gmail.DefaultClient()
				if err != nil {
					return nil, err
				}
				return handlerminiapp.PipelineFromGmailpoll(gmailClient, llmClient, localClient, model, localModel, s.mailAnalysisPrompt(), s.projectCandidatesFn(), s.wikiSenderFacts, tools.ExtractAttachmentTextBytes)
			},
			Cache:      handlerminiapp.NewAnalysisStore(filepath.Join(s.denebDir, "cache", "mail_analysis")),
			WorkState:  mailwork.New(filepath.Join(s.denebDir, "mail_work_state.json")),
			SaveToWiki: makeMailAnalysisWikiSink(hub),
			WikiStore: func() (handlerminiapp.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
			Ask: s.makeMailQAAsk(),
		}),
	}

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}

	// Wire agent runner and subagent poller to cron service. Cron output is
	// delivered to the native client via the main-session handoff wired in
	// registerSessionRPCMethods (proactive relay), not Telegram.
	if s.cronService != nil {
		// Pre-collect wiki weekly-report data for "/weekly" cron payloads so the
		// LLM writes inside a fixed 양식 (cronChatAdapter.resolveCronCommand), and
		// render the formal form image to post to the 업무 chat alongside the text.
		var weeklyDataFn func(ctx context.Context) (string, error)
		var weeklyFormFn func(ctx context.Context) error
		var weeklyTextFn func(ctx context.Context) (string, error)
		if s.wikiStore != nil {
			wikiDir := s.wikiStore.Dir()
			weeklyDataFn = func(ctx context.Context) (string, error) {
				return tools.CollectWeeklyReportData(ctx, tools.WeeklyReportOpts{WikiDir: wikiDir}, time.Now())
			}
			// Deterministic exact-양식 text — the cron prefers this over the LLM turn
			// so the report format is identical every run.
			weeklyTextFn = func(_ context.Context) (string, error) {
				return tools.RenderWeeklyReportText(tools.WeeklyReportOpts{WikiDir: wikiDir}, time.Now()), nil
			}
			weeklyFormFn = func(ctx context.Context) error {
				img, ok := tools.BuildWeeklyReportImage(ctx, tools.WeeklyReportOpts{WikiDir: wikiDir}, time.Now())
				if !ok {
					return nil // render unavailable (low memory/disk) → text report only
				}
				_, err := s.proactiveRelay.deliverNativeImage("📋 주간업무보고 — 정식 양식", img)
				return err
			}
		}
		s.cronService.SetAgentRunner(&cronChatAdapter{
			chat:              s.chatHandler,
			logger:            s.logger,
			weeklyReportData:  weeklyDataFn,
			weeklyReportText:  weeklyTextFn,
			weeklyFormDeliver: weeklyFormFn,
		})
		// Interactive /weekly (/주간보고) reuses the same deterministic generators
		// so a manually typed command matches the Saturday cron output (this path
		// was cron-only before — typed input fell through to the LLM).
		if s.chatHandler != nil {
			s.chatHandler.SetWeeklyReport(weeklyTextFn, weeklyFormFn)
		}
		if s.acpDeps != nil {
			s.cronService.SetSubagentPoller(&acpSubagentPoller{
				registry: s.acpDeps.Registry,
				sessions: s.sessions,
			})
		}
	}
}

// makeMailAnalysisWikiSink returns the SaveToWiki callback the Mini App's
// gmail.analyze handler invokes after a fresh LLM run. We persist into the
// wiki so the analysis (a) shows up in recall/search, (b) accumulates per
// sender for RAG context on future analyses. Page assembly lives in
// wiki_mail_analysis.go so this file stays focused on wiring. Returns nil
// if no wiki store is available, which is the handler's signal to skip
// persistence entirely.
func makeMailAnalysisWikiSink(hub *rpcutil.GatewayHub) func(handlerminiapp.WikiAnalysisInput) error {
	return func(in handlerminiapp.WikiAnalysisInput) error {
		store := hub.WikiStore()
		if store == nil {
			return nil
		}
		return store.WritePage(mailAnalysisWikiPath(in.MsgID, in.RelatedProjects), buildMailAnalysisPage(in))
	}
}

// resolveLocalCalendar returns the process-wide local calendar store, or a nil
// interface (so handlers degrade) when its file can't be read. Returning a nil
// literal — not the (nil, err) store — avoids a non-nil interface wrapping a nil
// pointer. The store lives at {stateDir}/calendar.json (dev uses its own dir).
// localFileStoreOrNil opens the default on-box file store, returning a nil
// interface (not a typed-nil *LocalStore) on error so FilesBrowseMethods skips
// the domain rather than panicking on a nil deref later.
func localFileStoreOrNil(logger *slog.Logger) filestore.Store {
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		if logger != nil {
			logger.Error("local file store unavailable — miniapp.files.* disabled", "error", err)
		}
		return nil
	}
	return store
}

func resolveLocalCalendar(logger *slog.Logger) handlerminiapp.LocalCalendar {
	store, err := localcal.Default()
	if err != nil {
		if logger != nil {
			logger.Error("local calendar store unavailable — add/edit/delete disabled", "error", err)
		}
		return nil
	}
	return store
}

// resolveCalendarProposals returns the process-wide calendar-proposal store
// (the bell), or a nil interface when its file can't be read. Mirrors
// resolveLocalCalendar. The store lives at {stateDir}/calendar_proposals.json.
func resolveCalendarProposals(logger *slog.Logger) handlerminiapp.CalProposals {
	store, err := calprop.Default()
	if err != nil {
		if logger != nil {
			logger.Error("calendar proposal store unavailable — bell disabled", "error", err)
		}
		return nil
	}
	return store
}

// resolveLocalTodos returns the process-wide to-do store, or a nil interface (so
// handlers degrade to UNAVAILABLE) when its file can't be read. Mirrors
// resolveLocalCalendar. The store lives at {stateDir}/todos.json.
func resolveLocalTodos(logger *slog.Logger) handlerminiapp.LocalTodos {
	store, err := localtodo.Default()
	if err != nil {
		if logger != nil {
			logger.Error("local todo store unavailable — to-do list disabled", "error", err)
		}
		return nil
	}
	return store
}

// mailPriorityScorer builds the inbox-row scorer for the gmail list handler.
// The scorer is stateless and cheap to construct; the VIP signal binds the
// (possibly nil) contacts store — nil simply drops that signal.
func mailPriorityScorer(cs *contacts.Store) *mailpriority.Scorer {
	var vip func(string) bool
	if cs != nil {
		vip = cs.HasEmail
	}
	return mailpriority.New(vip)
}
