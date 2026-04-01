// Centralized RPC method registration via GatewayHub.
//
// Replaces 18 register* wrapper methods with two functions:
//   - registerEarlyMethods: ~30 domains that don't need chatHandler
//   - registerLateMethods:  ~4 domains that depend on chatHandler
//
// Special-case helpers (registerConfigLifecycleMethods, registerAuthRPCMethods)
// are called inline because they contain business logic beyond simple registration.
package server

import (
	"fmt"
	"path/filepath"

	handleragent "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/agent"
	handleraurorachannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/aurora_channel"
	handlerchannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/channel"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/chat"
	handlernode "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/node"
	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/platform"
	handlerpresence "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/presence"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/provider"
	handlersession "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/session"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/system"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// registerEarlyMethods registers all RPC domains that don't depend on chatHandler.
// Called after buildHub() but before registerSessionRPCMethods().
func (s *Server) registerEarlyMethods(hub *GatewayHub, denebDir string) {
	// Lazy-init presence/heartbeat state.
	if s.presenceStore == nil {
		s.presenceStore = handlerpresence.NewStore()
	}
	if s.heartbeatState == nil {
		s.heartbeatState = handlerpresence.NewHeartbeatState()
	}

	// Compute canvasHost from runtime config.
	canvasHost := ""
	if s.runtimeCfg != nil {
		canvasHost = fmt.Sprintf("http://%s:%d", s.runtimeCfg.BindHost, s.runtimeCfg.Port)
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
			hub.Telegram = s.telegramPlug
			s.logger.Info("telegram channel configured")
		}
	}

	// Table-driven domain registration: one slice, one loop.
	domains := []map[string]rpcutil.HandlerFunc{
		// Agent orchestration.
		handleragent.ExtendedMethods(agentExtendedDepsFromHub(hub)),
		handlerprocess.ACPMethods(s.acpDeps),
		handleragent.CRUDMethods(agentCRUDDepsFromHub(hub)),

		// Tools and skills.
		handlerskill.ToolMethods(toolDepsFromHub(hub)),
		handlerskill.Methods(skillDepsFromHub(hub)),

		// Channel events and lifecycle.
		handlerchannel.BroadcastMethods(channelEventsDepsFromHub(hub)),
		handlerchannel.EventsMethods(channelEventsDepsFromHub(hub)),
		handlerchannel.LifecycleMethods(channelLifecycleDepsFromHub(hub)),
		handlerchannel.MessagingMethods(messagingDepsFromHub(hub)),

		// Node and device management.
		handlernode.Methods(nodeDepsFromHub(hub, canvasHost)),
		handlernode.DeviceMethods(deviceDepsFromHub(hub)),

		// Scheduling.
		handlerprocess.CronAdvancedMethods(cronAdvancedDepsFromHub(hub)),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronSvc}),

		// Approvals.
		handlerprocess.ApprovalMethods(approvalDepsFromHub(hub)),

		// Presence and heartbeat.
		handlerpresence.Methods(handlerpresence.Deps{
			Store:       s.presenceStore,
			Broadcaster: hub.Broadcast,
		}),
		handlerpresence.HeartbeatMethods(handlerpresence.HeartbeatDeps{
			State:       s.heartbeatState,
			Broadcaster: hub.Broadcast,
		}),

		// System: identity, monitoring, config, usage, logs, doctor, maintenance, update.
		handlersystem.IdentityMethods(hub.Version),
		handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{
			ChannelHealth: s.channelHealth,
			Activity:      s.activity,
		}),
		handlersystem.ConfigAdvancedMethods(configAdvancedDepsFromHub(hub)),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),
		handlersystem.DoctorMethods(handlersystem.DoctorDeps{}),
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),

		// Platform.
		handlerplatform.WizardMethods(handlerplatform.WizardDeps{Engine: hub.Wizard}),
		handlerplatform.TalkMethods(handlerplatform.TalkDeps{Talk: hub.Talk}),
	}

	// Conditional: provider methods (only when a provider registry is configured).
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
}

// registerLateMethods registers RPC domains that depend on chatHandler.
// Called after registerSessionRPCMethods() which creates the chat handler.
func (s *Server) registerLateMethods(hub *GatewayHub) {
	// Late-bind chatHandler into hub.
	hub.Chat = s.chatHandler

	domains := []map[string]rpcutil.HandlerFunc{
		handlerchat.Methods(handlerchat.Deps{Chat: hub.Chat}),
		handlerchat.BtwMethods(btwDepsFromHub(hub)),
		handlersession.ExecMethods(execDepsFromHub(hub)),
	}

	// Aurora channel (desktop app) — requires chatHandler.
	domains = append(domains, handleraurorachannel.Methods(handleraurorachannel.Deps{
		Chat: hub.Chat,
	}))

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
	}

	// Wire Telegram → chat pipeline now that both are ready.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
	}

	// Wire agent runner to cron service so scheduled jobs can execute agent turns.
	if s.cronService != nil {
		s.cronService.SetAgentRunner(&cronChatAdapter{chat: s.chatHandler})
	}
}
