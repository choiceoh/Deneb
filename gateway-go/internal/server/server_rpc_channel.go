package server

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	handlerchannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/channel"
	handlernode "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/node"
	handlerpresence "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/presence"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/provider"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/system"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

func (s *Server) registerEventsBroadcastMethods() {
	s.dispatcher.RegisterDomain(handlerchannel.BroadcastMethods(handlerchannel.EventsDeps{
		Broadcaster: s.broadcaster,
		Logger:      s.logger,
	}))
}

func (s *Server) registerConfigLifecycleMethods() {
	s.dispatcher.RegisterDomain(handlersystem.ConfigReloadMethods(handlersystem.ConfigReloadDeps{
		OnReloaded: func(_ *config.ConfigSnapshot) {
			// Notify hooks of config change.
			if s.hooks != nil {
				s.safeGo("hooks:config.reloaded", func() {
					s.hooks.Fire(context.Background(), hooks.Event("config.reloaded"), nil)
				})
			}
			// Broadcast config change to subscribers.
			s.broadcaster.Broadcast("config.changed", map[string]any{
				"ts": time.Now().UnixMilli(),
			})
			// Restart channels to pick up config changes.
			if s.channelLifecycle != nil {
				s.safeGo("config:restart-channels", func() {
					reloadCtx := context.Background()
					if errs := s.channelLifecycle.StopAll(reloadCtx); len(errs) > 0 {
						for id, err := range errs {
							s.logger.Warn("config reload: channel stop failed", "channel", id, "error", err)
						}
					}
					if errs := s.channelLifecycle.StartAll(reloadCtx); len(errs) > 0 {
						for id, err := range errs {
							s.logger.Warn("config reload: channel start failed", "channel", id, "error", err)
						}
					}
					s.logger.Info("config reload: channels restarted")
				})
			}
			// Restart cron scheduler.
			if s.cron != nil {
				s.safeGo("config:restart-cron", func() {
					s.cron.Close()
					s.cron = cron.NewScheduler(s.logger)
					s.logger.Info("config reload: cron scheduler restarted")
				})
			}
		},
	}))
	s.dispatcher.RegisterDomain(handlerchannel.LifecycleMethods(handlerchannel.LifecycleDeps{
		ChannelLifecycle: s.channelLifecycle,
		Hooks:            s.hooks,
		Broadcaster:      s.broadcaster,
	}))
}

func (s *Server) registerMonitoringMethods() {
	s.dispatcher.RegisterDomain(handlersystem.MonitoringMethods(handlersystem.MonitoringDeps{
		ChannelHealth: s.channelHealth,
		Activity:      s.activity,
	}))
}

func (s *Server) registerSubscriptionMethods() {
	s.dispatcher.RegisterDomain(handlerchannel.EventsMethods(handlerchannel.EventsDeps{Broadcaster: s.broadcaster, Logger: s.logger}))
}

func (s *Server) registerIdentityMethods() {
	s.dispatcher.RegisterDomain(handlersystem.IdentityMethods(s.version))
}

func (s *Server) registerHeartbeatMethods(broadcastFn func(string, any) (int, []error)) {
	if s.heartbeatState == nil {
		s.heartbeatState = handlerpresence.NewHeartbeatState()
	}
	s.dispatcher.RegisterDomain(handlerpresence.HeartbeatMethods(handlerpresence.HeartbeatDeps{
		State:       s.heartbeatState,
		Broadcaster: broadcastFn,
	}))
}

func (s *Server) registerPresenceMethods(broadcastFn func(string, any) (int, []error)) {
	if s.presenceStore == nil {
		s.presenceStore = handlerpresence.NewStore()
	}
	s.dispatcher.RegisterDomain(handlerpresence.Methods(handlerpresence.Deps{
		Store:       s.presenceStore,
		Broadcaster: broadcastFn,
	}))
}

func (s *Server) registerModelsMethods() {
	s.dispatcher.RegisterDomain(handlerprovider.ModelsMethods(handlerprovider.ModelsDeps{
		Providers: s.providers,
	}))
}

// registerAdvancedChannelMethods registers node, device, cron-advanced, skill, and
// config-advanced methods.
func (s *Server) registerAdvancedChannelMethods(broadcastFn func(string, any) (int, []error)) {
	canvasHost := ""
	if s.runtimeCfg != nil {
		canvasHost = fmt.Sprintf("http://%s:%d", s.runtimeCfg.BindHost, s.runtimeCfg.Port)
	}
	s.dispatcher.RegisterDomain(handlernode.Methods(handlernode.Deps{
		Nodes:       s.nodes,
		Broadcaster: broadcastFn,
		CanvasHost:  canvasHost,
	}))

	s.dispatcher.RegisterDomain(handlernode.DeviceMethods(handlernode.DeviceDeps{
		Devices:     s.devices,
		Broadcaster: broadcastFn,
	}))

	s.dispatcher.RegisterDomain(handlerprocess.CronAdvancedMethods(handlerprocess.CronAdvancedDeps{
		Cron:        s.cron,
		RunLog:      s.cronRunLog,
		Broadcaster: broadcastFn,
	}))

	s.dispatcher.RegisterDomain(handlersystem.ConfigAdvancedMethods(handlersystem.ConfigAdvancedDeps{
		Broadcaster: broadcastFn,
	}))

	s.dispatcher.RegisterDomain(handlerskill.Methods(handlerskill.Deps{
		Skills:      s.skills,
		Broadcaster: broadcastFn,
	}))
}

// registerSystemServiceMethods registers native system management (usage, logs, doctor,
// maintenance, update) and channel plugin (Telegram, Discord) methods.
func (s *Server) registerSystemServiceMethods(denebDir string) {
	s.dispatcher.RegisterDomain(handlersystem.UsageMethods(handlersystem.UsageDeps{
		Tracker: s.usageTracker,
	}))

	s.dispatcher.RegisterDomain(handlersystem.LogsMethods(handlersystem.LogsDeps{
		LogDir: filepath.Join(denebDir, "logs"),
	}))

	s.dispatcher.RegisterDomain(handlersystem.DoctorMethods(handlersystem.DoctorDeps{}))

	s.dispatcher.RegisterDomain(handlersystem.MaintenanceMethods(handlersystem.MaintenanceDeps{
		Runner: s.maintRunner,
	}))

	s.dispatcher.RegisterDomain(handlersystem.UpdateMethods(handlersystem.UpdateDeps{
		DenebDir: denebDir,
	}))

	// Telegram native channel plugin + messaging methods.
	// Loads Telegram config from deneb.json if available.
	if s.runtimeCfg != nil {
		tgCfg := loadTelegramConfig(s.runtimeCfg)
		if tgCfg == nil {
			s.logger.Warn("telegram channel not configured (config missing or invalid)")
		} else if tgCfg.BotToken == "" {
			s.logger.Warn("telegram channel config found but botToken is empty")
		} else {
			s.telegramPlug = telegram.NewPlugin(tgCfg, s.logger)
			s.channels.Register(s.telegramPlug)
			s.logger.Info("telegram channel registered")
		}
	}
	s.dispatcher.RegisterDomain(handlerchannel.MessagingMethods(handlerchannel.MessagingDeps{
		TelegramPlugin: s.telegramPlug,
	}))

	// Wire Telegram update handler → autoreply preprocessing → chat.send pipeline.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
	}

	// Discord native channel plugin (coding-focused).
	if s.runtimeCfg != nil {
		dcCfg := loadDiscordConfig(s.runtimeCfg)
		if dcCfg != nil && dcCfg.BotToken != "" {
			s.discordPlug = discord.NewPlugin(dcCfg, s.logger)
			s.channels.Register(s.discordPlug)
		}
	}

	// Wire Discord message handler → chat.send pipeline.
	if s.discordPlug != nil && s.chatHandler != nil {
		s.wireDiscordChatHandler()
	}
}
