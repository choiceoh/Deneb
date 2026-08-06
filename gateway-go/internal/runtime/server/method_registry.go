// Centralized early-phase RPC method registration via GatewayHub.
//
// Replaces the early register* wrappers with registerEarlyMethods for domains
// that do not need chatHandler. Late-phase wiring lives in
// method_registry_late.go and shared factories in method_registry_helpers.go.
//
// Deps structs are assembled inline from hub accessors — no adapter layer.
// Handlers still accept their own Deps structs (testability preserved);
// only the method_registry files know about the hub→Deps mapping.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calwrite"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/contactsdedup"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/notebooksource"
	runtimenotify "github.com/choiceoh/deneb/gateway-go/internal/runtime/notify"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlercheckpoint "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/checkpoint"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	minifiles "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/files"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	minimodule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/module"
	minischedule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/schedule"
	handlerinsights "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/insights"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlerobservatory "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observatory"
	handlerobserve "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observe"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
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
// graph — used by the analyze pipeline and the sender_context card. The argument
// is the From header (name and, when present, "<address>"). It prefers the EMAIL
// join: resolving the sender's address to their 인물 page and seeding the graph
// from that exact page reaches the RIGHT person across 동명이인 (김성훈@bohae vs
// 김성훈@marsh), which the display name alone conflates. Falls back to the name
// graph when no address resolves. Returns "" when the wiki is unconfigured or
// nothing matches, so callers fall back cleanly (to graphify, or an empty card).
func (s *Server) wikiSenderFacts(ctx context.Context, from string) string {
	if s.wikiStore == nil {
		return ""
	}
	if email := senderEmailFromHeader(from); email != "" {
		if path := s.wikiStore.ResolvePersonByEmail(email); path != "" {
			if facts, err := s.wikiStore.PageConnections(ctx, path, 0); err == nil && facts != "" {
				return facts
			}
		}
	}
	// GraphContext strips any "<address>" itself, so the raw From is a safe query.
	facts, err := s.wikiStore.GraphContext(ctx, from, 0)
	if err != nil {
		return ""
	}
	return facts
}

// senderEmailFromHeader pulls the address out of a "Name <addr@host>" From header
// (or returns a bare address as-is). "" when the header carries no address.
func senderEmailFromHeader(from string) string {
	from = strings.TrimSpace(from)
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			from = from[i+1 : i+j]
		}
	}
	from = strings.TrimSpace(from)
	if strings.Contains(from, "@") {
		return from
	}
	return ""
}

// withMailAliases returns a miniapp.gmail.* method map extended with a
// miniapp.mail.* alias for every method (same handler, both names dispatch).
// The mail domain is archive-first — LMTP-ingested mail on the local archive
// is the primary store and the Gmail API only a legacy fallback — so mail.*
// is the accurate namespace going forward; gmail.* stays registered for
// client compatibility until both native clients migrate.
func withMailAliases(m map[string]rpcutil.HandlerFunc) map[string]rpcutil.HandlerFunc {
	out := make(map[string]rpcutil.HandlerFunc, len(m)*2)
	for name, h := range m {
		out[name] = h
		if rest, ok := strings.CutPrefix(name, "miniapp.gmail."); ok {
			out["miniapp.mail."+rest] = h
		}
	}
	return out
}

// earlyMethodCapabilities contains the shared, phase-local dependencies created
// before early RPC domains are registered. It is deliberately private to this
// composition root: handler Deps remain assembled inline below.
type earlyMethodCapabilities struct {
	nativeWorkFeed *nativeWorkFeedStore
	observe        handlerobserve.Deps
	observatory    handlerobservatory.Deps
	miniapp        map[string]rpcutil.HandlerFunc
}

// registerEarlyMethods registers all RPC domains that don't depend on chatHandler.
// Called after buildHub() but before registerSessionRPCMethods().
func (s *Server) registerEarlyMethods(hub *rpcutil.GatewayHub, denebDir string) error {
	hub.AdvancePhase(rpcutil.PhaseEarly)

	// Fail fast if core hub fields are missing.
	if err := hub.Validate(); err != nil {
		return fmt.Errorf("server init: hub validation: %w", err)
	}

	capabilities, err := s.initializeEarlyMethodCapabilities(hub, denebDir)
	if err != nil {
		return err
	}
	s.registerEarlyCapabilityDomains(hub, denebDir, capabilities)

	// Special-case registrations with embedded business logic.
	s.registerConfigLifecycleMethods()
	return nil
}

// initializeEarlyMethodCapabilities creates stores and long-lived services that
// early handlers consume. Keeping lifecycle setup separate from registration
// makes the boot sequence explicit and prevents registration changes from
// accidentally reordering service initialization.
func (s *Server) initializeEarlyMethodCapabilities(hub *rpcutil.GatewayHub, denebDir string) (earlyMethodCapabilities, error) {
	// Create the insights engine. Read-only — aggregates session manager
	// snapshots and usage tracker state. Stored on both the hub (for RPC
	// handlers) and the server (so the chat dispatcher can wire /insights).
	insightsEngine := insights.New(hub.Sessions(), s.usageTracker)
	hub.Opt.Insights = insightsEngine
	s.insights = insightsEngine

	// Device address book mirror (native-client contacts sync) — no chat dependency,
	// so it's created here in the early phase and is ready by the time chat init wires
	// the contacts tool. nil-tolerant: a load failure just disables the store write
	// (the contacts tool / save path degrade to "unavailable" cleanly).
	if cs, err := contacts.NewStore(filepath.Join(denebDir, "contacts.json")); err != nil {
		s.logger.Warn("contacts store init failed; contacts sync disabled", "error", err)
	} else {
		s.contactsStore = cs
		hub.Opt.ContactsStore = cs
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
				Broadcast: func(event string, payload json.RawMessage) {
					s.broadcaster.Broadcast(event, events.PayloadFromRaw(payload))
				},
				ShutdownCtx: s.ShutdownCtx(),
			})
			s.logger.Info("FCM push fallback enabled", "projectID", sender.ProjectID())
		}
	}

	// Monitoring notify service (error mirrors + status snapshots → native push).
	s.notify = runtimenotify.NewService(hub.Sessions(), hub.Logger(), func(title, body string) {
		nativepush.PublishWithFallback(s.pushHub, s.pushNotifier, nativepush.Event{Title: title, Body: body})
	}, s.BoundAddr)
	if s.notify != nil {
		s.broadcaster.RegisterTap(s.notify.Tap)
		s.notify.Start(s.ShutdownCtx())
	}

	// Observation-plane deps, shared verbatim by the in-process observe.* and
	// the remote miniapp.observe.* registrations below. AgentLog and VllmBases
	// are getters because the agentlog writer and the model registry are both
	// constructed later (session phase); resolving lazily avoids capturing nil.
	observeDeps := handlerobserve.Deps{
		Capture:  s.logCapture,
		AgentLog: func() *agentlog.Writer { return s.agentLogWriter },
		StateDir: func() string { return denebDir },
		VllmBases: func() []string {
			if s.modelRegistry == nil {
				return nil
			}
			return s.modelRegistry.VllmBaseURLs()
		},
		Logger: hub.Logger(),
	}

	// Improvement-loop liveness digest, registered twice like observe above
	// (in-process observatory.* + client-token-gated miniapp.observatory.*).
	// denebDir is the resolved state dir the watchdog reads too.
	observatoryDeps := handlerobservatory.Deps{
		StateDir: func() string { return denebDir },
	}

	// 시장(market) card data for the 오늘 dashboard — a keyless Yahoo Finance fetch
	// cached for 10m. Promoted to a server field so the agent's market tool
	// (wired later during chat init) shares the same cache/asOf. Created once at boot.
	s.marketCache = market.NewCache()
	marketCache := s.marketCache

	// Independently owned native-client capabilities. Build their combined
	// method set before the registration table so ownership collisions fail
	// through this function's normal startup error boundary.
	miniappMethods, err := minimodule.Methods(minimodule.Dependencies{
		Sync:      minimodule.SyncDeps{Store: s.nativeSyncStore},
		Dashboard: s.dashboardDeps(),
		Sessions: minimodule.SessionsDeps{
			Manager: hub.Sessions(),
			Transcripts: func() (minimodule.TranscriptLoader, error) {
				if s.toolDeps == nil || s.toolDeps.Sessions.Transcript == nil {
					return nil, errTranscriptUnavailable
				}
				return s.toolDeps.Sessions.Transcript, nil
			},
		},
		Contacts: minimodule.ContactsDeps{
			Store: func() (*contacts.Store, error) {
				cs := hub.Opt.ContactsStore
				if cs == nil {
					return nil, errors.New("contacts store not configured")
				}
				return cs, nil
			},
			Adjudicator: s.contactsAdjudicator(),
		},
		Market: minimodule.MarketDeps{Fetch: marketCache.Summary},
	})
	if err != nil {
		return earlyMethodCapabilities{}, fmt.Errorf("server init: miniapp module: %w", err)
	}

	return earlyMethodCapabilities{
		nativeWorkFeed: nativeWorkFeed,
		observe:        observeDeps,
		observatory:    observatoryDeps,
		miniapp:        miniappMethods,
	}, nil
}

// contactsAdjudicator builds the LLM-backed adjudicator for the address-book
// dedup, or nil when no model is configured (then miniapp.contacts.adjudicate is
// simply not registered). Returning an untyped nil is deliberate: a typed-nil
// *contactsdedup.Adjudicator wrapped in the interface would read as non-nil and
// the handler would call a nil adjudicator.
func (s *Server) contactsAdjudicator() contacts.Adjudicator {
	if s.modelRegistry == nil {
		return nil
	}
	adj := contactsdedup.New(
		s.modelRegistry.Client(modelrole.RoleSubmain),
		s.modelRegistry.Model(modelrole.RoleSubmain),
	)
	if adj == nil {
		return nil
	}
	return adj
}

// registerEarlyCapabilityDomains preserves the historical domain order while
// keeping phase orchestration independent from handler dependency assembly.
func (s *Server) registerEarlyCapabilityDomains(hub *rpcutil.GatewayHub, denebDir string, capabilities earlyMethodCapabilities) {
	// Table-driven domain registration: one slice, one loop.
	// Deps assembled inline from hub accessors — no adapter layer.
	domains := s.earlyCoreMethods(hub, denebDir, capabilities)
	domains = append(domains, s.earlyNativeClientMethods(hub, capabilities)...)
	domains = append(domains, s.earlyMailAndCalendarMethods(denebDir)...)
	domains = append(domains, s.earlyPlanningMethods(hub, denebDir)...)
	domains = append(domains, s.earlyKnowledgeMethods(hub)...)
	domains = append(domains, s.earlyImprovementMethods(hub)...)

	// Provider methods are the only capability group omitted entirely when its
	// backing registry is unavailable.
	domains = append(domains, s.earlyProviderMethods()...)

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}
}

// earlyCoreMethods owns the transport-agnostic control plane. Its order is
// intentionally stable: sessions and health precede orchestration, followed by
// tools, events, scheduling, observability, and lifecycle operations.
func (s *Server) earlyCoreMethods(hub *rpcutil.GatewayHub, denebDir string, capabilities earlyMethodCapabilities) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		handlersession.CRUDMethods(handlersession.Deps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
			Transcripts: func() (handlersession.TranscriptDeleter, error) {
				if s.toolDeps == nil || s.toolDeps.Sessions.Transcript == nil {
					return nil, errTranscriptUnavailable
				}
				return s.toolDeps.Sessions.Transcript, nil
			},
		}),
		handlersystem.HealthMethods(handlersystem.HealthDeps{
			SessionCount: hub.Sessions().Count,
			Version:      hub.Version(),
		}),
		handleragent.ExtendedMethods(handleragent.ExtendedDeps{
			Sessions:    hub.Sessions(),
			GatewaySubs: hub.GatewaySubs(),
			Processes:   hub.Processes(),
			CronService: hub.CronService(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.ACPMethods(s.acpDeps),
		handlerskill.ToolMethods(handlerskill.ToolDeps{Processes: hub.Processes()}),
		handlerskill.Methods(handlerskill.Deps{
			Skills:      hub.Skills(),
			Broadcaster: hub.Broadcast,
		}),
		handlerevents.BroadcastMethods(handlerevents.EventsDeps{
			Broadcaster: hub.Broadcaster(),
			Logger:      hub.Logger(),
		}),
		handlerevents.EventsMethods(handlerevents.EventsDeps{
			Broadcaster: hub.Broadcaster(),
			Logger:      hub.Logger(),
		}),
		handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
			Service:     hub.CronService(),
			RunLog:      hub.CronPersistLog(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronService()}),
		handlersystem.IdentityMethods(hub.Version()),
		handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{Dispatcher: s.dispatcher}),
		handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{Broadcaster: hub.Broadcast}),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),
		handlerobserve.Methods(capabilities.observe),
		handlerobservatory.Methods(capabilities.observatory),
		handlerinsights.Methods(handlerinsights.Deps{
			Engine: hub.Opt.Insights,
			Logger: hub.Logger(),
		}),
		handlercheckpoint.Methods(handlercheckpoint.Deps{
			Root:   filepath.Join(denebDir, "checkpoints"),
			Logger: hub.Logger(),
		}),
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),
	}
}

// earlyNativeClientMethods owns the authenticated miniapp transport surface
// and shared native stores. Product domains are registered by later groups.
func (s *Server) earlyNativeClientMethods(hub *rpcutil.GatewayHub, capabilities earlyMethodCapabilities) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		handlerobserve.MiniappMethods(capabilities.observe),
		handlerobservatory.MiniappMethods(capabilities.observatory),
		s.earlyMiniappGatewayMethods(hub),
		capabilities.miniapp,
		// DeliveryEnabled keeps the client on background SSE when FCM is missing
		// or its OAuth dependency is currently unreachable.
		handlerminiapp.PushMethods(handlerminiapp.PushDeps{
			Store: s.pushTokenStore,
			DeliveryEnabled: func(ctx context.Context) bool {
				return s.pushNotifier != nil && s.pushNotifier.DeliveryEnabled(ctx)
			},
		}),
		handlerminiapp.WormholeMethods(handlerminiapp.WormholeDeps{}),
		handlerminiapp.WorkFeedMethods(handlerminiapp.WorkFeedDeps{
			Store:          capabilities.nativeWorkFeed,
			OnAnswer:       s.recordDealQuestionAnswer,
			OnMetaProposal: s.handleMetaProposalAction,
			OnDeadlineDone: s.markDeadlineDone,
		}),
		handlerminiapp.UsageMethods(handlerminiapp.UsageDeps{
			// Lazy: the agent-log writer and model registry are built in the
			// session phase, after this early registration.
			AgentLog: func() *agentlog.Writer { return s.agentLogWriter },
			RoleForRequest: func(requested string) string {
				if requested == "" {
					return "main"
				}
				if s.modelRegistry != nil {
					if _, role, ok := s.modelRegistry.ResolveModel(requested); ok {
						return string(role)
					}
					if role, ok := s.modelRegistry.RoleForModel(requested); ok {
						return string(role)
					}
				}
				return requested
			},
		}),
		// miniapp.models.* is deliberately registered in registerLateMethods:
		// the picker snapshots the model registry and chat handler at creation.
		s.earlyFileMethods(),
	}
}

func (s *Server) earlyMailAndCalendarMethods(denebDir string) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		withMailAliases(handlermail.GmailMethods(handlermail.GmailDeps{
			Client: s.miniappMailClientFactory(denebDir),
			// Native-sync mirror: archive/trash from one client force-warms the
			// mail list on the others. Warn on failure — TTL revalidation backstops.
			NotifyChanged: func(messageID string) {
				if s.nativeSyncStore == nil {
					return
				}
				if _, err := s.nativeSyncStore.Append(nativesync.MailChanged(messageID)); err != nil {
					s.logger.Warn("native sync: mail change append failed", "messageId", messageID, "error", err)
				}
			},
			AnalysisCache: handlermail.NewAnalysisStore(filepath.Join(denebDir, "cache", "mail_analysis")),
			WorkState:     mailwork.New(filepath.Join(denebDir, "mail_work_state.json")),
			MailStore: func() handlermail.MailStoreReader {
				if s.mailStore == nil {
					return nil
				}
				return s.mailStore
			},
			Priority: func() func(from, subject, snippet string) (string, string) {
				counterparties := mailflow.NewCounterpartyLookup(func() *wiki.Store { return s.wikiStore })
				return func(from, subject, snippet string) (string, string) {
					tier, hint := mailPriorityScorer(s.contactsStore, counterparties).Score(from, subject, snippet)
					return string(tier), hint
				}
			}(),
		})),
		minischedule.CalendarMethods(minischedule.CalendarDeps{
			Client: func() (minischedule.CalendarClient, error) {
				return calendar.DefaultClient()
			},
			Local:     resolveLocalCalendar(s.logger),
			Proposals: resolveCalendarProposals(s.logger),
			// One-way write mirror (Deneb → Google), ON BY DEFAULT since #4210
			// (the existing calendar token already carries the read+write scope);
			// DENEB_CALENDAR_GOOGLE_WRITE=0 turns it off. Best-effort: the syncer
			// logs push failures here and handlers ignore them (local write is
			// authoritative). The chat calendar tool wires the same syncer in
			// chat_pipeline.go — both surfaces mirror, or neither should.
			Writer: func() (minischedule.CalendarWriter, error) {
				return calwrite.DefaultSyncer(func(op string, err error) {
					s.logger.Warn("calendar google-sync failed", "op", op, "error", err)
				})
			},
		}),
	}
}

func (s *Server) earlyPlanningMethods(hub *rpcutil.GatewayHub, denebDir string) []map[string]rpcutil.HandlerFunc {
	approvalCache := groupware.NewApprovalAnalysisStore(filepath.Join(denebDir, "cache", "approval_analysis"))
	approvalBodyCache := groupware.NewApprovalBodyStore(filepath.Join(denebDir, "cache", "approval_body"))
	return []map[string]rpcutil.HandlerFunc{
		s.earlyProjectMethods(hub),
		handlerminiapp.OrgMethods(s.orgDeps()),
		minischedule.TodoMethods(minischedule.TodoDeps{Store: resolveLocalTodos(s.logger)}),
		// 전자결재 browse/act/get/analyze + ERP list — always registered;
		// FromEnv fails the call with DEPENDENCY_FAILED when credentials are
		// unset (same pattern as live work-feed chips calling ActApproval).
		handlerminiapp.GroupwareApprovalsMethods(handlerminiapp.GroupwareApprovalsDeps{
			List: func(ctx context.Context, folder string, limit int) ([]groupware.ApprovalSummary, error) {
				cfg, ok := groupware.FromEnv()
				if !ok {
					return nil, fmt.Errorf("groupware credentials unset")
				}
				return groupware.ListApprovals(ctx, cfg, folder, limit)
			},
			Act: func(ctx context.Context, docID, decision, comment string) (string, error) {
				cfg, ok := groupware.FromEnv()
				if !ok {
					return "", fmt.Errorf("groupware credentials unset")
				}
				return groupware.ActApproval(ctx, cfg, docID, decision, comment)
			},
			Get: func(ctx context.Context, docID, folder string) (string, error) {
				// Read-through body cache: repeated detail opens skip the
				// Playwright roundtrip; the folder hint skips the 4-box scan
				// on a cold open.
				if body := approvalBodyCache.Load(docID); body != "" {
					return body, nil
				}
				cfg, ok := groupware.FromEnv()
				if !ok {
					return "", fmt.Errorf("groupware credentials unset")
				}
				body, err := groupware.ReadApprovalByDocIDIn(ctx, cfg, docID, folder)
				if err != nil {
					return "", err
				}
				_ = approvalBodyCache.Save(docID, body)
				return body, nil
			},
			Attach: func(ctx context.Context, docID, selector string) (string, error) {
				cfg, ok := groupware.FromEnv()
				if !ok {
					return "", fmt.Errorf("groupware credentials unset")
				}
				return groupware.ReadApprovalAttachment(ctx, cfg, docID, selector)
			},
			Cache: approvalCache,
			Analyze: func(ctx context.Context, docID, title, date, body string) (string, string, error) {
				return s.completeApprovalAnalysis(ctx, docID, title, date, body)
			},
			ListERP: func(ctx context.Context, area, folder, query string, limit int) (string, error) {
				cfg, ok := groupware.FromEnv()
				if !ok {
					return "", fmt.Errorf("groupware credentials unset")
				}
				return groupware.ListERP(ctx, cfg, area, folder, query, limit)
			},
			ReadBoard: func(ctx context.Context, query string) (string, error) {
				cfg, ok := groupware.FromEnv()
				if !ok {
					return "", fmt.Errorf("groupware credentials unset")
				}
				return groupware.ReadBoardPost(ctx, cfg, query)
			},
			LogWiki: s.logApprovalAnalysisToWiki,
		}),
	}
}

// earlyKnowledgeMethods keeps every late-bound knowledge source behind its
// request-time factory so an unavailable wiki, notebook, or cron service does
// not prevent gateway startup.
func (s *Server) earlyKnowledgeMethods(hub *rpcutil.GatewayHub) []map[string]rpcutil.HandlerFunc {
	wikiStore := func() (miniknowledge.MemorySearcher, error) {
		store := hub.Opt.WikiStore
		if store == nil {
			return nil, errWikiDisabled
		}
		return store, nil
	}

	return []map[string]rpcutil.HandlerFunc{
		miniknowledge.MemoryMethods(miniknowledge.MemoryDeps{
			Store:      wikiStore,
			StartMerge: s.makeWikiMergeStarter(hub),
		}),
		miniknowledge.NotebookMethods(miniknowledge.NotebookDeps{
			Store: func() (*notebook.Store, error) {
				if s.notebookStore == nil {
					return nil, errNotebookDisabled
				}
				return s.notebookStore, nil
			},
			// Same in-house extractor the file browser and chat attachments use, so a
			// picked notebook file is read exactly like a chat-attached document.
			ExtractText: func(ctx context.Context, data []byte, filename, mimeType string) string {
				text, _ := toolbind.ExtractDocumentText(ctx, data, filename, mimeType)
				return text
			},
			// Image OCR / audio-video ASR — the same sidecars chat capture uses, so a
			// photo of a contract or a meeting recording becomes notebook grounding.
			OcrImage:   toolbind.OCRImage,
			Transcribe: toolbind.TranscribeAudio,
			// Archive the original bytes under 노트북/<id>/ in the file store so the
			// user can re-open the source (add_file pins the returned path as the ref).
			SaveOriginal: func(ctx context.Context, id, filename string, data []byte) (string, error) {
				fs := localFileStoreOrNil(s.logger)
				if fs == nil {
					return "", fmt.Errorf("file store unavailable")
				}
				name := filepath.Base(strings.TrimSpace(filename))
				if name == "" || name == "." || name == "/" {
					name = "file"
				}
				entry, err := fs.Put(ctx, "노트북/"+id+"/"+name, data, false)
				if err != nil {
					return "", err
				}
				return entry.PathDisplay, nil
			},
			// Same SSRF-safe readers the notebook chat tool uses for url/mail/diary
			// sources. FetchURL/ReadMail need no runtime state; ReadDiary reads
			// s.wikiStore lazily (set during chat init, long before any add_ref call —
			// reading the field at call time, not registration, avoids a nil snapshot).
			FetchURL: notebooksource.FetchURL,
			ReadMail: notebooksource.ReadMail,
			ReadDiary: func(ctx context.Context, ref string) (string, error) {
				return notebooksource.ReadDiary(s.wikiStore, ref)
			},
		}),
		minischedule.CronsMethods(minischedule.CronsDeps{
			Service: func() (minischedule.CronService, error) {
				service := hub.CronService()
				if service == nil {
					return nil, errCronUnavailable
				}
				return service, nil
			},
		}),
		handlerminiapp.PromptMethods(handlerminiapp.PromptDeps{Store: s.promptStore}),
		handlerminiapp.PromptTunerMethods(handlerminiapp.PromptTunerDeps{
			Tuner: func() handlerminiapp.PromptTuner {
				if s.modelMaintenance == nil {
					return nil
				}
				return s.modelMaintenance.PromptTuner()
			},
		}),
		miniknowledge.TopicDocsMethods(miniknowledge.TopicDocsDeps{
			TopicsDir:  func() (string, error) { return configresolve.TopicsDir(), nil },
			CurrentKey: configresolve.CurrentTopicKey,
			ApplyNow:   prompt.Cache.ClearAllTopicSnapshots,
		}),
		withMailAliases(handlermail.GmailContextMethods(handlermail.GmailContextDeps{
			Client: func() (handlermail.GmailClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: wikiStore,
			SenderFacts: func(ctx context.Context, from string) string {
				if facts := s.wikiSenderFacts(ctx, from); facts != "" {
					return facts
				}
				return mailanalysis.ExtractSenderFacts(ctx, from)
			},
		})),
		miniknowledge.PeopleMethods(miniknowledge.PeopleDeps{
			Client: func() (miniknowledge.PeopleClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: wikiStore,
		}),
	}
}

func (s *Server) earlyImprovementMethods(hub *rpcutil.GatewayHub) []map[string]rpcutil.HandlerFunc {
	return []map[string]rpcutil.HandlerFunc{
		s.earlySkillMethods(),
		s.earlySelfImprovementMethods(),
		s.earlyRSIStatusMethods(),
		miniknowledge.SearchMethods(miniknowledge.SearchDeps{
			Store: func() (miniknowledge.MemorySearcher, error) {
				store := hub.Opt.WikiStore
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
			},
			Client: func() (miniknowledge.PeopleClient, error) {
				return gmail.DefaultClient()
			},
		}),
	}
}

// earlyMiniappGatewayMethods wires the native client's boot identity and
// capability snapshot. All readiness probes are lazy because chat, wiki, and
// model services become available after the early phase.
func (s *Server) earlyMiniappGatewayMethods(hub *rpcutil.GatewayHub) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.Methods(handlerminiapp.Deps{
		Version: hub.Version(),
		CurrentModel: func() string {
			if s.chatHandler != nil {
				if model := s.chatHandler.DefaultModel(); model != "" {
					return model
				}
			}
			if s.modelRegistry != nil {
				return s.modelRegistry.FullModelID(modelrole.RoleMain)
			}
			return ""
		},
		Capabilities: func() map[string]bool {
			wikiReady := hub.Opt.WikiStore != nil
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
				"captureContacts": hub.Opt.ContactsStore != nil,
				"workFeed":        s.workFeedStore != nil,
				"workFeedActions": s.workFeedStore != nil,
				"nativeSync":      s.nativeSyncStore != nil,
				"pushRegister":    s.pushTokenStore != nil,
				"pushFallback":    s.pushNotifier != nil,
				"gmailAttachment": true,
				"updateManifest":  true,
				"prompts":         s.promptStore != nil,
				"promptTuner":     s.modelMaintenance != nil && s.modelMaintenance.PromptTuner() != nil,
				"topicDocs":       configresolve.CurrentTopicKey() != "",
			}
		},
	})
}

// earlyFileMethods wires local file operations and the late-bound semantic
// index. Mutations invalidate stale vectors immediately; the periodic indexer
// repopulates them with current content.
func (s *Server) earlyFileMethods() map[string]rpcutil.HandlerFunc {
	return minifiles.FilesBrowseMethods(minifiles.FilesBrowseDeps{
		Store: localFileStoreOrNil(s.logger),
		ExtractText: func(ctx context.Context, data []byte, name string) string {
			text, _ := toolbind.ExtractDocumentText(ctx, data, name, "")
			return text
		},
		SemanticSearch: func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
			if s.fileSemindex == nil {
				return nil, nil
			}
			return s.fileSemindex.Search(ctx, query, max)
		},
		OnDelete: func(path string) {
			if s.fileSemindex != nil {
				s.fileSemindex.Remove(path)
			}
		},
		OnMove: func(oldPath, newPath string) {
			if s.fileSemindex != nil {
				s.fileSemindex.Rename(oldPath, newPath)
			}
		},
		OnUpload: func(path string) {
			if s.fileSemindex != nil {
				s.fileSemindex.Remove(path)
			}
		},
	})
}

// earlyProjectMethods wires the project dashboard to lazy snapshots. Every
// source is resolved at request time so session-phase stores are visible.
func (s *Server) earlyProjectMethods(hub *rpcutil.GatewayHub) map[string]rpcutil.HandlerFunc {
	return handlerminiapp.ProjectMethods(handlerminiapp.ProjectDeps{
		Wiki: func() (handlerminiapp.ProjectStatusSource, error) {
			store := hub.Opt.WikiStore
			if store == nil {
				return nil, errWikiDisabled
			}
			return store, nil
		},
		Notebooks: func() []handlerminiapp.ProjectLinkedNotebook {
			if s.notebookStore == nil {
				return nil
			}
			notebooks := s.notebookStore.List()
			out := make([]handlerminiapp.ProjectLinkedNotebook, 0, len(notebooks))
			for _, notebook := range notebooks {
				out = append(out, handlerminiapp.ProjectLinkedNotebook{
					ID: notebook.ID, DealRef: notebook.DealRef, ProjectRefs: notebook.ProjectRefs,
				})
			}
			return out
		},
		WorkItems: func() []handlerminiapp.ProjectLinkedWorkItem {
			feed := s.nativeWorkFeedStore()
			if feed == nil {
				return nil
			}
			items, _, err := feed.List(1000, true)
			if err != nil {
				return nil
			}
			out := make([]handlerminiapp.ProjectLinkedWorkItem, 0, len(items))
			for _, item := range items {
				out = append(out, handlerminiapp.ProjectLinkedWorkItem{ID: item.ID, RefID: item.RefID})
			}
			return out
		},
		Calendar: func() []handlerminiapp.ProjectLinkedCalEvent {
			store, err := localcal.Default()
			if err != nil || store == nil {
				return nil
			}
			now := time.Now()
			events := store.ListRange(now.AddDate(-1, 0, 0), now.AddDate(1, 0, 0))
			out := make([]handlerminiapp.ProjectLinkedCalEvent, 0, len(events))
			for _, event := range events {
				if event.Source == "" {
					continue
				}
				out = append(out, handlerminiapp.ProjectLinkedCalEvent{ID: event.ID, Source: event.Source})
			}
			return out
		},
	})
}

// earlySkillMethods wires the native skill catalog to late-bound Genesis
// projections. Missing tracker state degrades to an unenriched catalog.
func (s *Server) earlySkillMethods() map[string]rpcutil.HandlerFunc {
	return minimodule.SkillsMethods(minimodule.SkillsDeps{
		List: func() []skills.SkillEntry {
			var toolNames []string
			if s.chatHandler != nil {
				toolNames = s.chatHandler.ToolNames()
			}
			return chat.EligibleWorkspaceSkills(configresolve.WorkspaceDir(), toolNames)
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
	})
}

func (s *Server) earlySelfImprovementMethods() map[string]rpcutil.HandlerFunc {
	return minimodule.SelfImprovementCodingMethods(minimodule.SelfImprovementCodingDeps{
		RecentCandidates: func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
			if s.genesisTracker == nil {
				return nil, nil
			}
			return s.genesisTracker.RecentSelfCorrectionCandidates("", status, limit)
		},
		NextDispatchCandidate: func(excludedIDs []string) (genesis.SelfCorrectionCandidateRecord, bool, error) {
			if s.genesisTracker == nil {
				return genesis.SelfCorrectionCandidateRecord{}, false, nil
			}
			return s.genesisTracker.NextSelfCorrectionDispatchCandidate(excludedIDs)
		},
		RecordCandidate: func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
			if s.genesisTracker == nil {
				return genesis.SelfCorrectionCandidateRecord{}, errors.New("genesis tracker unavailable")
			}
			return s.genesisTracker.RecordSelfCorrectionCandidate(rec)
		},
		RecordDispatch: func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
			if s.genesisTracker == nil {
				return genesis.SelfCorrectionCandidateRecord{}, errors.New("genesis tracker unavailable")
			}
			// Stamp the dispatch procedure (the coding-session contract prompt
			// version) Go-side at the composition root — the gateway materializes
			// the same prompt the out-of-process dispatch script reads, so this
			// captures the active dispatch procedure without touching that script.
			// Folded first-seen per attempt, so the started row's version wins.
			if s.genesisMeta != nil {
				rec.DispatchProcedureRef = s.genesisMeta.DispatchProcedureRef()
			}
			return s.genesisTracker.RecordSelfCorrectionDispatch(rec)
		},
		RecordImpact: func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
			if s.genesisTracker == nil {
				return genesis.SelfCorrectionCandidateRecord{}, errors.New("genesis tracker unavailable")
			}
			return s.genesisTracker.RecordSelfCorrectionDispatch(rec)
		},
		Funnel: func() genesis.SelfCorrectionFunnelSummary {
			if s.genesisTracker == nil {
				return genesis.SelfCorrectionFunnelSummary{}
			}
			return s.genesisTracker.SelfCorrectionFunnel()
		},
		LastNudgeAtMs: runtimeheartbeat.LastSelfCodingNudgeAtMillis,
	})
}

func (s *Server) earlyRSIStatusMethods() map[string]rpcutil.HandlerFunc {
	return minimodule.RSIStatusMethods(minimodule.RSIStatusDeps{
		Status: func() genesis.RSILoopStatus {
			if s.genesisTracker == nil {
				return genesis.RSILoopStatus{}
			}
			return s.genesisTracker.RSIStatus()
		},
	})
}

func (s *Server) earlyProviderMethods() []map[string]rpcutil.HandlerFunc {
	if s.providers == nil {
		return nil
	}
	return []map[string]rpcutil.HandlerFunc{
		handlerprovider.Methods(handlerprovider.Deps{
			Providers:   s.providers,
			AuthManager: s.authManager,
		}),
		handlerprovider.ModelsMethods(handlerprovider.ModelsDeps{
			Providers: s.providers,
		}),
	}
}
