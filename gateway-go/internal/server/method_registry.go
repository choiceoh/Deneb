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

	handleragent "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/agent"
	handlerautoresearch "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/autoresearch"
	handleraurorachannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/aurora_channel"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/chat"
	handlerbridge "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/bridge"
	handlerevents "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/handlerevents"
	handlertask "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/handlertask"
	handlertelegram "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/handlertelegram"
	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/platform"
	handlerpresence "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/presence"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/system"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// registerEarlyMethods registers all RPC domains that don't depend on chatHandler.
// Called after buildHub() but before registerSessionRPCMethods().
func (s *Server) registerEarlyMethods(hub *rpcutil.GatewayHub, denebDir string) error {
	hub.AdvancePhase(rpcutil.PhaseEarly)

	// Fail fast if core hub fields are missing.
	if err := hub.Validate(); err != nil {
		return fmt.Errorf("server init: hub validation: %w", err)
	}

	// Lazy-init presence/heartbeat state.
	if s.presenceStore == nil {
		s.presenceStore = handlerpresence.NewStore()
	}
	if s.heartbeatState == nil {
		s.heartbeatState = handlerpresence.NewHeartbeatState()
	}

	// Create Telegram plugin from config if available.
	if s.runtimeCfg != nil {
		tgCfg := loadTelegramConfig(s.runtimeCfg)
		if tgCfg == nil {
			s.logger.Warn("telegram channel not configured (config missing or invalid)")
		} else if tgCfg.BotToken == "" {
			s.logger.Warn("telegram channel config found but botToken is empty")
		} else {
			s.telegramPlug = telegram.NewPlugin(tgCfg, s.logger)
			hub.SetTelegram(s.telegramPlug)
		}
	}

	// Table-driven domain registration: one slice, one loop.
	// Deps assembled inline from hub accessors — no adapter layer.
	domains := []map[string]rpcutil.HandlerFunc{
		// --- Agent orchestration ---
		handleragent.ExtendedMethods(handleragent.ExtendedDeps{
			Sessions:       hub.Sessions(),
			TelegramPlugin: hub.Telegram(),
			GatewaySubs:    hub.GatewaySubs(),
			Processes:      hub.Processes(),
			Cron:           hub.Cron(),
			InternalHooks:  hub.InternalHooks(),
			Broadcaster:    hub.Broadcast,
		}),
		handlerprocess.ACPMethods(s.acpDeps),
		handleragent.CRUDMethods(handleragent.AgentsDeps{
			Agents:      hub.Agents(),
			Broadcaster: hub.Broadcast,
		}),

		// --- Tools and skills ---
		handlerskill.ToolMethods(handlerskill.ToolDeps{Processes: hub.Processes()}),
		handlerskill.Methods(handlerskill.Deps{
			Skills:      hub.Skills(),
			Broadcaster: hub.Broadcast,
		}),

		// --- Inter-agent bridge ---
		handlerbridge.Methods(func() handlerbridge.Deps {
			s.bridgeInjector = &handlerbridge.Injector{}
			return handlerbridge.Deps{
				Broadcaster: hub.Broadcast,
				Injector:    s.bridgeInjector,
			}
		}()),

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
			InternalHooks:  hub.InternalHooks(),
			Broadcaster:    hub.Broadcaster(),
		}),
		handlertelegram.MessagingMethods(handlertelegram.MessagingDeps{
			TelegramPlugin: hub.Telegram(),
		}),

		// --- Scheduling ---
		handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
			Cron:        hub.Cron(),
			RunLog:      hub.CronPersistLog(),
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronService()}),

		// --- Background task control plane ---
		handlertask.Methods(handlertask.Deps{Registry: hub.Tasks()}),

		// --- Approvals ---
		handlerprocess.ApprovalMethods(handlerprocess.ApprovalDeps{
			Store:       hub.Approvals(),
			Broadcaster: hub.Broadcast,
		}),

		// --- Presence and heartbeat ---
		handlerpresence.Methods(handlerpresence.Deps{
			Store:       s.presenceStore,
			Broadcaster: hub.Broadcast,
		}),
		handlerpresence.HeartbeatMethods(handlerpresence.HeartbeatDeps{
			State:       s.heartbeatState,
			Broadcaster: hub.Broadcast,
		}),

		// --- System ---
		handlersystem.IdentityMethods(hub.Version()),
		handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{
			ChannelHealth: s.channelHealth,
			Activity:      s.activity,
			Dispatcher:    s.dispatcher,
		}),
		handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{
			Broadcaster: hub.Broadcast,
		}),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),
		handlersystem.DoctorMethods(handlersystem.DoctorDeps{}),
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),

		// --- Platform ---
		handlerplatform.WizardMethods(handlerplatform.WizardDeps{Engine: hub.Wizard()}),
		handlerplatform.TalkMethods(handlerplatform.TalkDeps{Talk: hub.Talk()}),
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

	domains := []map[string]rpcutil.HandlerFunc{
		handlerchat.Methods(handlerchat.Deps{Chat: hub.Chat()}),
		handlerchat.BtwMethods(handlerchat.BtwDeps{
			Chat:        hub.Chat(),
			Broadcaster: hub.Broadcast,
		}),
		handlersession.ExecMethods(handlersession.ExecDeps{
			Chat:       hub.Chat(),
			Agents:     hub.Agents(),
			JobTracker: hub.JobTracker(),
		}),
		handleraurorachannel.Methods(handleraurorachannel.Deps{Chat: hub.Chat()}),
		handlerautoresearch.Methods(handlerautoresearch.Deps{
			Runner: s.autoresearchRunner,
		}),
	}

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}

	// Wire bridge: send to the most recently active Telegram session.
	// Only one session to avoid duplicate Aurora writes (shared conversationID).
	if s.bridgeInjector != nil && s.chatHandler != nil {
		sessions := hub.Sessions()
		s.bridgeInjector.SetSend(
			s.chatHandler.SendDirect,
			func() []string {
				var bestKey string
				var bestUpdated int64
				for _, sess := range sessions.List() {
					if sess.Kind == session.KindDirect && sess.Channel == "telegram" {
						if sess.UpdatedAt > bestUpdated {
							bestUpdated = sess.UpdatedAt
							bestKey = sess.Key
						}
					}
				}
				if bestKey == "" {
					return nil
				}
				return []string{bestKey}
			},
		)
	}

	// Wire Telegram → chat pipeline now that both are ready.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
	}

	// Wire agent runner to cron service.
	if s.cronService != nil {
		s.cronService.SetAgentRunner(&cronChatAdapter{chat: s.chatHandler})
	}
}
