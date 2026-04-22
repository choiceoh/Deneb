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
	"fmt"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/agent"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerevents"
	handlertask "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlertask"
	handlertelegram "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlertelegram"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
	handlerwiki "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

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
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),
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
		handlerchat.Methods(handlerchat.Deps{Chat: hub.Chat()}),
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
		s.cronService.SetAgentRunner(&cronChatAdapter{chat: s.chatHandler})
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
