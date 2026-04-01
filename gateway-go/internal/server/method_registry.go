// Centralized RPC method registration via GatewayHub.
//
// Replaces 18 register* wrapper methods with two functions:
//   - registerEarlyMethods: ~30 domains that don't need chatHandler
//   - registerLateMethods:  ~4 domains that depend on chatHandler
//
// Deps structs are assembled inline from hub fields — no adapter layer.
// Handlers still accept their own Deps structs (testability preserved);
// only this file knows about the hub→Deps mapping.
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
func (s *Server) registerEarlyMethods(hub *rpcutil.GatewayHub, denebDir string) {
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
	// Deps assembled inline from hub fields — no adapter layer.
	domains := []map[string]rpcutil.HandlerFunc{
		// Agent orchestration.
		handleragent.ExtendedMethods(handleragent.ExtendedDeps{
			Sessions:       hub.Sessions,
			TelegramPlugin: hub.Telegram,
			GatewaySubs:    hub.GatewaySubs,
			Processes:      hub.Processes,
			Cron:           hub.Cron,
			Hooks:          hub.Hooks,
			Broadcaster:    hub.Broadcaster,
		}),
		handlerprocess.ACPMethods(s.acpDeps),
		handleragent.CRUDMethods(handleragent.AgentsDeps{
			Agents:      hub.Agents,
			Broadcaster: hub.Broadcast,
		}),

		// Tools and skills.
		handlerskill.ToolMethods(handlerskill.ToolDeps{Processes: hub.Processes}),
		handlerskill.Methods(handlerskill.Deps{
			Skills:      hub.Skills,
			Broadcaster: hub.Broadcast,
		}),

		// Channel events and lifecycle.
		handlerchannel.BroadcastMethods(handlerchannel.EventsDeps{
			Broadcaster: hub.Broadcaster,
			Logger:      hub.Logger,
		}),
		handlerchannel.EventsMethods(handlerchannel.EventsDeps{
			Broadcaster: hub.Broadcaster,
			Logger:      hub.Logger,
		}),
		handlerchannel.LifecycleMethods(handlerchannel.LifecycleDeps{
			TelegramPlugin: hub.Telegram,
			Hooks:          hub.Hooks,
			Broadcaster:    hub.Broadcaster,
		}),
		handlerchannel.MessagingMethods(handlerchannel.MessagingDeps{
			TelegramPlugin: hub.Telegram,
		}),

		// Node and device management.
		handlernode.Methods(handlernode.Deps{
			Nodes:       hub.Nodes,
			Broadcaster: hub.Broadcast,
			CanvasHost:  canvasHost,
		}),
		handlernode.DeviceMethods(handlernode.DeviceDeps{
			Devices:     hub.Devices,
			Broadcaster: hub.Broadcast,
		}),

		// Scheduling.
		handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
			Cron:        hub.Cron,
			RunLog:      hub.CronRunLog,
			Broadcaster: hub.Broadcast,
		}),
		handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{Service: hub.CronSvc}),

		// Approvals.
		handlerprocess.ApprovalMethods(handlerprocess.ApprovalDeps{
			Store:       hub.Approvals,
			Broadcaster: hub.Broadcast,
		}),

		// Presence and heartbeat.
		handlerpresence.Methods(handlerpresence.Deps{
			Store:       s.presenceStore,
			Broadcaster: hub.Broadcast,
		}),
		handlerpresence.HeartbeatMethods(handlerpresence.HeartbeatDeps{
			State:       s.heartbeatState,
			Broadcaster: hub.Broadcast,
		}),

		// System.
		handlersystem.IdentityMethods(hub.Version),
		handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{
			ChannelHealth: s.channelHealth,
			Activity:      s.activity,
		}),
		handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{
			Broadcaster: hub.Broadcast,
		}),
		handlersystem.UsageMethods(handlersystem.UsageDeps{Tracker: s.usageTracker}),
		handlersystem.LogsMethods(handlersystem.LogsDeps{LogDir: filepath.Join(denebDir, "logs")}),
		handlersystem.DoctorMethods(handlersystem.DoctorDeps{}),
		handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{Runner: s.maintRunner}),
		handlersystem.UpdateMethods(handlersystem.UpdateDeps{DenebDir: denebDir}),

		// Platform.
		handlerplatform.WizardMethods(handlerplatform.WizardDeps{Engine: hub.Wizard}),
		handlerplatform.TalkMethods(handlerplatform.TalkDeps{Talk: hub.Talk}),
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
}

// registerLateMethods registers RPC domains that depend on chatHandler.
// Called after registerSessionRPCMethods() which creates the chat handler.
func (s *Server) registerLateMethods(hub *rpcutil.GatewayHub) {
	hub.Chat = s.chatHandler

	domains := []map[string]rpcutil.HandlerFunc{
		handlerchat.Methods(handlerchat.Deps{Chat: hub.Chat}),
		handlerchat.BtwMethods(handlerchat.BtwDeps{
			Chat:        hub.Chat,
			Broadcaster: hub.Broadcast,
		}),
		handlersession.ExecMethods(handlersession.ExecDeps{
			Chat:       hub.Chat,
			Agents:     hub.Agents,
			JobTracker: hub.JobTracker,
		}),
		handleraurorachannel.Methods(handleraurorachannel.Deps{Chat: hub.Chat}),
	}

	for _, d := range domains {
		if d != nil {
			s.dispatcher.RegisterDomain(d)
		}
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
