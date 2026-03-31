package server

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
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
	// Resolve reload debounce/deferral settings from config.
	debounceMs := 300 // default
	deferralTimeoutMs := 300000
	if s.runtimeCfg != nil {
		if s.runtimeCfg.ReloadConfig.DebounceMs != nil {
			debounceMs = *s.runtimeCfg.ReloadConfig.DebounceMs
		}
		if s.runtimeCfg.ReloadConfig.DeferralTimeoutMs != nil {
			deferralTimeoutMs = *s.runtimeCfg.ReloadConfig.DeferralTimeoutMs
		}
	}

	// Debounce timer: collapses rapid config.reload calls into a single
	// propagation pass using gateway.reload.debounceMs.
	var debounceMu sync.Mutex
	var debounceTimer *time.Timer

	s.dispatcher.RegisterDomain(handlersystem.ConfigReloadMethods(handlersystem.ConfigReloadDeps{
		OnReloaded: func(snap *config.ConfigSnapshot) {
			debounceMu.Lock()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(time.Duration(debounceMs)*time.Millisecond, func() {
				s.propagateConfigReload(snap, deferralTimeoutMs)
			})
			debounceMu.Unlock()
		},
	}))
	s.dispatcher.RegisterDomain(handlerchannel.LifecycleMethods(handlerchannel.LifecycleDeps{
		TelegramPlugin: s.telegramPlug,
		Hooks:          s.hooks,
		Broadcaster:    s.broadcaster,
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

	// cron.Service-backed methods: cron.listPage (paginated list with search/sort)
	// and cron.get (single job lookup by ID).
	s.dispatcher.RegisterDomain(handlerprocess.CronServiceMethods(handlerprocess.CronServiceDeps{
		Service: s.cronService,
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
// maintenance, update) and channel plugin (Telegram) methods.
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
			s.logger.Info("telegram channel configured")
		}
	}
	s.dispatcher.RegisterDomain(handlerchannel.MessagingMethods(handlerchannel.MessagingDeps{
		TelegramPlugin: s.telegramPlug,
	}))

	// Wire Telegram update handler → autoreply preprocessing → chat.send pipeline.
	if s.telegramPlug != nil && s.chatHandler != nil {
		s.wireTelegramChatHandler()
	}

}

// propagateConfigReload performs the post-reload side effects: hooks, channel
// restart (bounded by deferralTimeoutMs), cron restart, and process env cache
// invalidation.
func (s *Server) propagateConfigReload(snap *config.ConfigSnapshot, deferralTimeoutMs int) {
	// Notify hooks of config change, passing the config path as metadata.
	if s.hooks != nil {
		hookEnv := map[string]string{"DENEB_CONFIG_PATH": snap.Path}
		s.safeGo("hooks:config.reloaded", func() {
			s.hooks.Fire(context.Background(), hooks.Event("config.reloaded"), hookEnv)
		})
	}
	// Broadcast config change to subscribers via publisher.
	s.publisher.PublishConfigChanged("config")

	// Invalidate the process manager's cached environment so new processes
	// pick up any env changes introduced by the reloaded config.
	if s.processes != nil {
		s.processes.InvalidateEnvCache()
	}

	// Restart Telegram to pick up config changes, bounded by deferralTimeoutMs.
	if s.telegramPlug != nil {
		s.safeGo("config:restart-telegram", func() {
			timeout := time.Duration(deferralTimeoutMs) * time.Millisecond
			reloadCtx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			if err := s.telegramPlug.Stop(reloadCtx); err != nil {
				s.logger.Warn("config reload: telegram stop failed", "error", err)
			}
			if err := s.telegramPlug.Start(reloadCtx); err != nil {
				s.logger.Warn("config reload: telegram start failed", "error", err)
			}
			s.logger.Info("config reload: telegram restarted")
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
}
