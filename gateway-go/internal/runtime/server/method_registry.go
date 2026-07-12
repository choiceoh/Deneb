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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

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
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/mailflow"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelpicker"
	runtimenotify "github.com/choiceoh/deneb/gateway-go/internal/runtime/notify"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
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
	s.notify = runtimenotify.NewService(hub.Sessions(), hub.Logger(), func(title, body string) {
		proactive.PublishWithFallback(s.pushHub, s.pushNotifier, proactive.Event{Title: title, Body: body})
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
				cs := hub.ContactsStore()
				if cs == nil {
					return nil, errors.New("contacts store not configured")
				}
				return cs, nil
			},
		},
		Market: minimodule.MarketDeps{Fetch: marketCache.Summary},
	})
	if err != nil {
		return fmt.Errorf("server init: miniapp module: %w", err)
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
			Dispatcher: s.dispatcher,
		}),
		handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{
			Broadcaster: hub.Broadcast,
		}),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),

		// --- Observation plane (unified: log ring + turn shape + behavior) ---
		handlerobserve.Methods(observeDeps),
		handlerobservatory.Methods(observatoryDeps),

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
		handlerobservatory.MiniappMethods(observatoryDeps),
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
					"topicDocs": configresolve.CurrentTopicKey() != "",
				}
			},
		}),
		miniappMethods,
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
			// Apply a meta-proposal card's 채택/기각 (RSI P2 feed-card adoption).
			// See workfeed_meta_proposal.go.
			OnMetaProposal: s.handleMetaProposalAction,
		}),
		modelpicker.NewController(modelpicker.ControllerConfig{
			Registry:    s.modelRegistry,
			ChatHandler: s.chatHandler,
			Logger:      s.logger,
			RoleHealthVerdicts: func() map[string]string {
				if s.roleHealth == nil {
					return nil
				}
				return s.roleHealth.Verdicts()
			},
			RefreshCodingModelConsumers: s.refreshCodingModelConsumers,
			ProviderConfigs: func() map[string]chat.ProviderConfig {
				return configresolve.LoadProviderConfigs(s.logger)
			},
		}).Methods(),
		// Native local file browser (miniapp.files.{list,search,share,upload}):
		// list/search/share/upload over the on-box file store (filestore). share
		// mints a signed download link (fileshare); a nil store (open error)
		// skips the domain.
		minifiles.FilesBrowseMethods(minifiles.FilesBrowseDeps{
			Store: localFileStoreOrNil(s.logger),
			// Content search (search content=true) extracts file text via the chat
			// tools' document extractor. Wired here (server layer may import tools);
			// the handler keeps it as an injected callback to avoid a layer inversion.
			ExtractText: func(ctx context.Context, d []byte, n string) string {
				t, _ := document.ExtractDocumentText(ctx, d, n, "")
				return t
			},
			// Semantic search (search semantic=true) ranks by meaning via the shared
			// BGE-M3 file index. A lazy closure: the index + embedding client are
			// created later in initToolsAndDeps (this wiring runs in the early phase),
			// and requests arrive well after boot, so reading them at call time is
			// safe. Returns empty (→ name/content fallback) when the index/embedding
			// server is unavailable.
			SemanticSearch: func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
				if s.fileSemindex == nil {
					return nil, nil
				}
				return s.fileSemindex.Search(ctx, query, max)
			},
			// Keep the semantic index fresh after a delete/move/overwrite so
			// search doesn't hand back a stale path — or rank an overwritten
			// file by its old content — between 15-min reindex passes. Lazy
			// like SemanticSearch (the index is created later in
			// initToolsAndDeps). An overwrite-save drops the stale vectors
			// (Remove); the next reindex re-embeds the new content.
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
		}),

		// Native mail domain. Registered under BOTH miniapp.gmail.* (legacy,
		// what shipped clients call) and miniapp.mail.* (accurate name — the
		// server prefers the on-box archive repository and keeps Gmail only as
		// a fallback for legacy queries/tokens). See withMailAliases.
		withMailAliases(handlermail.GmailMethods(handlermail.GmailDeps{
			Client: s.miniappMailClientFactory(denebDir),
			// Same per-msgID cache directory the analyze handler/poller
			// write to (the store is a stateless dir wrapper) — list rows
			// prefer its LLM verdict over the heuristic below.
			AnalysisCache: handlermail.NewAnalysisStore(filepath.Join(denebDir, "cache", "mail_analysis")),
			WorkState:     mailwork.New(filepath.Join(denebDir, "mail_work_state.json")),
			// Lazy: mailStore is created in the session phase, after this early
			// registration. Serve get bodies from it once present (no API round-trip).
			MailStore: func() handlermail.MailStoreReader {
				if s.mailStore == nil {
					return nil
				}
				return s.mailStore
			},
			// Row priority: cheap local heuristics + address-book VIP lookup
			// + active-counterparty boost (recent project-linked mail
			// analyses in the wiki). contactsStore is created above in this
			// same registration pass; the wiki store is late-bound (session
			// phase), which the lookup's getter tolerates — until it exists
			// the boost is simply off. Nil stores just drop their signal.
			Priority: func() func(from, subject, snippet string) (string, string) {
				cp := mailflow.NewCounterpartyLookup(func() *wiki.Store { return s.wikiStore })
				return func(from, subject, snippet string) (string, string) {
					tier, hint := mailPriorityScorer(s.contactsStore, cp).Score(from, subject, snippet)
					return string(tier), hint
				}
			}(),
		})),

		// Mini App Calendar domain. Hybrid: a read-only Google client (lazy
		// factory, like Gmail — gateway boots without OAuth tokens; reads
		// return UNAVAILABLE only when no local store either) plus a local
		// store ({stateDir}/calendar.json) that holds hand-added events, so
		// create/edit/delete work without a Google write scope.
		minischedule.CalendarMethods(minischedule.CalendarDeps{
			Client: func() (minischedule.CalendarClient, error) {
				return calendar.DefaultClient()
			},
			Local:     resolveLocalCalendar(s.logger),
			Proposals: resolveCalendarProposals(s.logger),
		}),

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
			// Item snapshots for miniapp.project.linked (server-side matching).
			// Read at call time so the late-set notebook store is picked up; a nil
			// store simply contributes no matches. Mail needs no provider — it is
			// resolved from the project's graph refs inside the handler.
			Notebooks: func() []handlerminiapp.ProjectLinkedNotebook {
				if s.notebookStore == nil {
					return nil
				}
				nbs := s.notebookStore.List()
				out := make([]handlerminiapp.ProjectLinkedNotebook, 0, len(nbs))
				for _, nb := range nbs {
					out = append(out, handlerminiapp.ProjectLinkedNotebook{ID: nb.ID, DealRef: nb.DealRef, ProjectRefs: nb.ProjectRefs})
				}
				return out
			},
			WorkItems: func() []handlerminiapp.ProjectLinkedWorkItem {
				nf := s.nativeWorkFeedStore()
				if nf == nil {
					return nil
				}
				items, _, err := nf.List(1000, true) // superset; the client filters to its own list
				if err != nil {
					return nil
				}
				out := make([]handlerminiapp.ProjectLinkedWorkItem, 0, len(items))
				for _, it := range items {
					out = append(out, handlerminiapp.ProjectLinkedWorkItem{ID: it.ID, RefID: it.RefID})
				}
				return out
			},
			Calendar: func() []handlerminiapp.ProjectLinkedCalEvent {
				store, err := localcal.Default()
				if err != nil || store == nil {
					return nil
				}
				now := time.Now()
				events := store.ListRange(now.AddDate(-1, 0, 0), now.AddDate(1, 0, 0)) // wide superset
				out := make([]handlerminiapp.ProjectLinkedCalEvent, 0, len(events))
				for _, ev := range events {
					if ev.Source == "" {
						continue // only Deneb-sourced (mail-proposal) events carry a project link
					}
					out = append(out, handlerminiapp.ProjectLinkedCalEvent{ID: ev.ID, Source: ev.Source})
				}
				return out
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
		minischedule.TodoMethods(minischedule.TodoDeps{
			Store: resolveLocalTodos(s.logger),
		}),

		// Mini App memory search (miniapp.memory.search). Lazy factory
		// around hub.WikiStore() — wiki is created in the late phase
		// (registerLateMethods) so the factory is what defers the lookup
		// to per-request, by which time the store is wired. When wiki
		// is disabled by config the factory surfaces UNAVAILABLE.
		miniknowledge.MemoryMethods(miniknowledge.MemoryDeps{
			Store: func() (miniknowledge.MemorySearcher, error) {
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
		miniknowledge.NotebookMethods(miniknowledge.NotebookDeps{
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
		minischedule.CronsMethods(minischedule.CronsDeps{
			Service: func() (minischedule.CronService, error) {
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
		miniknowledge.TopicDocsMethods(miniknowledge.TopicDocsDeps{
			TopicsDir:  func() (string, error) { return configresolve.TopicsDir(), nil },
			CurrentKey: configresolve.CurrentTopicKey,
			ApplyNow:   prompt.Cache.ClearAllTopicSnapshots,
		}),

		// Mini App Gmail sender context (miniapp.gmail.sender_context).
		// Combines Gmail recent-activity query, wiki memory lookup, and
		// wiki-graph traversal (graphify CLI) so the Mini App detail
		// view can show a contextual sender card.
		withMailAliases(handlermail.GmailContextMethods(handlermail.GmailContextDeps{
			Client: func() (handlermail.GmailClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: func() (miniknowledge.MemorySearcher, error) {
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
				return mailanalysis.ExtractSenderFacts(ctx, from)
			},
		})),

		// Mini App people directory (miniapp.people.list). Same Gmail
		// lazy-client pattern; aggregates a single Search call into a
		// frequency-sorted counterparty list, then folds in 인물 wiki
		// pages (best-effort — wiki disabled degrades to Gmail-only).
		miniknowledge.PeopleMethods(miniknowledge.PeopleDeps{
			Client: func() (miniknowledge.PeopleClient, error) {
				return gmail.DefaultClient()
			},
			WikiStore: func() (miniknowledge.MemorySearcher, error) {
				store := hub.WikiStore()
				if store == nil {
					return nil, errWikiDisabled
				}
				return store, nil
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
			Funnel: func() genesis.SelfCorrectionFunnelSummary {
				if s.genesisTracker == nil {
					return genesis.SelfCorrectionFunnelSummary{}
				}
				return s.genesisTracker.SelfCorrectionFunnel()
			},
			LastNudgeAtMs: runtimeheartbeat.LastSelfCodingNudgeAtMillis,
		}),

		// Mini App unified search (miniapp.search.all). Single entry
		// point that fans out to wiki + diary + people in parallel.
		// Replaces the per-domain home menu entries — there's now one
		// search input on home that returns three result sections.
		// Either factory may be unavailable; the handler degrades
		// gracefully (Gmail-disabled gateway still serves wiki+diary).
		miniknowledge.SearchMethods(miniknowledge.SearchDeps{
			Store: func() (miniknowledge.MemorySearcher, error) {
				store := hub.WikiStore()
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

	// Conditional: provider methods.
	if s.providers != nil {
		domains = append(
			domains,
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
