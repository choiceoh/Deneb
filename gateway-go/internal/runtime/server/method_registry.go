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
	"errors"
	"fmt"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlercheckpoint "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/checkpoint"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	handlertask "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlertask"
	handlertelegram "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlertelegram"
	handlerinsights "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/insights"
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

// registerEarlyMethods registers all RPC domains that don't depend on chatHandler.
// Called after buildHub() but before registerSessionRPCMethods().
func (s *Server) registerEarlyMethods(hub *rpcutil.GatewayHub, denebDir string) error {
	hub.AdvancePhase(rpcutil.PhaseEarly)

	// Fail fast if core hub fields are missing.
	if err := hub.Validate(); err != nil {
		return fmt.Errorf("server init: hub validation: %w", err)
	}

	// Create Telegram plugin from config if available.
	if s.runtimeCfg != nil {
		tgCfg := loadTelegramConfig(s.runtimeCfg)
		switch {
		case tgCfg == nil:
			s.logger.Warn("telegram channel not configured (config missing or invalid)")
		case tgCfg.BotToken == "":
			s.logger.Warn("telegram channel config found but botToken is empty")
		default:
			s.telegramPlug = telegram.NewPlugin(tgCfg, s.logger)
			hub.SetTelegram(s.telegramPlug)
		}
	}

	// Create the insights engine. Read-only — aggregates session manager
	// snapshots and usage tracker state. Stored on both the hub (for RPC
	// handlers) and the server (so the chat dispatcher can wire /insights).
	insightsEngine := insights.New(hub.Sessions(), s.usageTracker)
	hub.SetInsights(insightsEngine)
	s.insights = insightsEngine

	// Create the secondary-chat notify service when the Telegram plugin
	// has a notificationChatID configured. The service registers a tap
	// on the broadcaster so user-impacting events (delivery failures,
	// compaction stuck) are mirrored to the monitoring chat, and exposes
	// a status-snapshot sender that the telegram.notify_status RPC drives.
	// When nil (no Telegram plugin or no monitoring chat configured), the
	// telegram.notify_status method is not registered.
	s.notify = newNotifyService(hub.Telegram(), hub.Sessions(), hub.Logger(), s.BoundAddr)
	if s.notify != nil {
		s.broadcaster.RegisterTap(s.notify.tap)
		s.notify.start(s.ShutdownCtx())
		// Install the slog forwarder by swapping the swappable handler's
		// inner. Captured s.logger references in subsystems transparently
		// route through the new wrapping handler. Skipped silently when
		// the swappable wasn't created (logger==nil at startup).
		if s.logSwap != nil {
			s.logSwap.Swap(newNotifySlogHandler(s.logSwap.currentInner(), s.notify))
		}
	}

	// Table-driven domain registration: one slice, one loop.
	// Deps assembled inline from hub accessors — no adapter layer.
	domains := []map[string]rpcutil.HandlerFunc{
		// --- Session CRUD (list/get/delete) ---
		handlersession.CRUDMethods(handlersession.Deps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
		}),

		// --- Health and system info ---
		handlersystem.HealthMethods(handlersystem.HealthDeps{
			SessionCount: hub.Sessions().Count,
			HasTelegram:  func() bool { return hub.Telegram() != nil },
			Version:      hub.Version(),
		}),

		// --- Telegram status (list/get/status/health) ---
		handlertelegram.StatusMethods(handlertelegram.StatusDeps{
			TelegramPlugin: hub.Telegram(),
			SnapshotStore:  s.snapshotStore,
		}),

		// --- Agent orchestration ---
		handleragent.ExtendedMethods(handleragent.ExtendedDeps{
			Sessions:       hub.Sessions(),
			TelegramPlugin: hub.Telegram(),
			GatewaySubs:    hub.GatewaySubs(),
			Processes:      hub.Processes(),
			CronService:    hub.CronService(),
			Broadcaster:    hub.Broadcast,
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

		// --- Telegram lifecycle and messaging ---
		handlertelegram.LifecycleMethods(handlertelegram.LifecycleDeps{
			TelegramPlugin: hub.Telegram(),
			Broadcaster:    hub.Broadcaster(),
		}),
		handlertelegram.NotifyMethods(handlertelegram.NotifyDeps{
			SendStatusSnapshot: notifyStatusSnapshotFunc(s.notify),
		}),
		// --- Scheduling ---
		handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
			Service:     hub.CronService(),
			RunLog:      hub.CronPersistLog(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronService()}),

		// --- Background task control plane ---
		handlertask.Methods(handlertask.Deps{Registry: hub.Tasks()}),

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

		// --- Telegram Mini App (HTTP-exposed via /api/v1/miniapp/rpc) ---
		// Requires initData verification, enforced by the HTTP bridge in
		// server_http_miniapp.go before the dispatcher is reached. The
		// methods read the authenticated user from context via
		// telegram.InitDataFromContext.
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
		}),

		// Mini App Gmail domain (miniapp.gmail.list_recent / get /
		// mark_read / archive). Lazy factory around gmail.DefaultClient
		// — if OAuth tokens are missing the gateway still starts; the
		// RPC just returns UNAVAILABLE until the operator runs the
		// Gmail auth flow.
		handlerminiapp.GmailMethods(handlerminiapp.GmailDeps{
			Client: func() (handlerminiapp.GmailClient, error) {
				return gmail.DefaultClient()
			},
		}),

		// Mini App Calendar domain (miniapp.calendar.list_upcoming / get).
		// Same lazy-factory pattern as Gmail — gateway boots without
		// OAuth tokens configured; per-call UNAVAILABLE until the
		// operator drops calendar_client.json + calendar_token.json
		// into ~/.deneb/credentials/.
		handlerminiapp.CalendarMethods(handlerminiapp.CalendarDeps{
			Client: func() (handlerminiapp.CalendarClient, error) {
				return calendar.DefaultClient()
			},
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
		}),

		// Mini App cron job list (miniapp.crons.list). Same lazy-factory
		// pattern as memory: cron.Service is wired during buildHub so by
		// the time the first RPC fires the service is ready, but a
		// gateway started with the cron subsystem disabled still gets a
		// clean UNAVAILABLE per call instead of a crash at boot.
		handlerminiapp.CronsMethods(handlerminiapp.CronsDeps{
			Service: func() (handlerminiapp.CronLister, error) {
				svc := hub.CronService()
				if svc == nil {
					return nil, errCronUnavailable
				}
				return svc, nil
			},
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
			SenderFacts: gmailpoll.ExtractSenderFacts,
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
	s.registerAuthRPCMethods()
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
			Client: func() (handlerminiapp.GmailClient, error) {
				return gmail.DefaultClient()
			},
			Pipeline: func() (handlerminiapp.AnalyzePipeline, error) {
				if s.modelRegistry == nil {
					return nil, handlerminiapp.ErrAnalyzeNoLLM
				}
				llmClient := s.modelRegistry.Client(modelrole.RoleMain)
				model := s.modelRegistry.Model(modelrole.RoleMain)
				gmailClient, err := gmail.DefaultClient()
				if err != nil {
					return nil, err
				}
				return handlerminiapp.PipelineFromGmailpoll(gmailClient, llmClient, model)
			},
			Cache:      handlerminiapp.NewAnalysisStore(filepath.Join(s.denebDir, "cache", "mail_analysis")),
			SaveToWiki: makeMailAnalysisWikiSink(hub),
		}),
	}

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}

	// Wire Telegram → chat pipeline now that both are ready.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
		// Fail-fast: if wiring forgot replyFunc, every Telegram reply would
		// drop silently. Better to refuse to start than to silently ignore users.
		if err := s.chatHandler.Validate(); err != nil {
			s.logger.Error("chat handler validation failed — refusing to serve", "error", err)
			panic(fmt.Errorf("chat handler misconfigured: %w", err))
		}
	}

	// Wire agent runner, Telegram plugin, and subagent poller to cron service.
	if s.cronService != nil {
		s.cronService.SetAgentRunner(&cronChatAdapter{chat: s.chatHandler, logger: s.logger})
		if s.telegramPlug != nil {
			s.cronService.SetTelegramPlugin(s.telegramPlug)
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
		return store.WritePage(mailAnalysisWikiPath(in.MsgID), buildMailAnalysisPage(in))
	}
}
