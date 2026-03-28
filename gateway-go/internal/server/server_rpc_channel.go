package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/discord"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// registerChannelEventsMethods registers event broadcasting, config reload, monitoring,
// channel lifecycle, event subscriptions, identity, heartbeat, presence, and model-list methods.
func (s *Server) registerChannelEventsMethods(broadcastFn func(string, any) (int, []error)) {
	// Event broadcasting method.
	s.dispatcher.Register("events.broadcast", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		sent, _ := s.broadcaster.Broadcast(p.Event, p.Payload)
		return protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
	})

	// Config reload method with Go subsystem propagation.
	rpc.RegisterConfigReloadMethod(s.dispatcher, rpc.ConfigReloadDeps{
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
	})

	// Monitoring methods.
	rpc.RegisterMonitoringMethods(s.dispatcher, rpc.MonitoringDeps{
		ChannelHealth: s.channelHealth,
		Activity:      s.activity,
	})

	// Channel lifecycle RPC methods.
	rpc.RegisterChannelLifecycleMethods(s.dispatcher, rpc.ChannelLifecycleDeps{
		ChannelLifecycle: s.channelLifecycle,
		Hooks:            s.hooks,
		Broadcaster:      s.broadcaster,
	})

	// Event subscription methods.
	rpc.RegisterEventsMethods(s.dispatcher, rpc.EventsDeps{Broadcaster: s.broadcaster, Logger: s.logger})

	// Gateway identity method.
	rpc.RegisterIdentityMethods(s.dispatcher, s.version)

	// Heartbeat methods (last-heartbeat, set-heartbeats).
	if s.heartbeatState == nil {
		s.heartbeatState = rpc.NewHeartbeatState()
	}
	rpc.RegisterHeartbeatMethods(s.dispatcher, rpc.HeartbeatDeps{
		State:       s.heartbeatState,
		Broadcaster: broadcastFn,
	})

	// System presence methods (system-presence, system-event).
	if s.presenceStore == nil {
		s.presenceStore = rpc.NewPresenceStore()
	}
	rpc.RegisterPresenceMethods(s.dispatcher, rpc.PresenceDeps{
		Store:       s.presenceStore,
		Broadcaster: broadcastFn,
	})

	// Models list method.
	rpc.RegisterModelsMethods(s.dispatcher, rpc.ModelsDeps{
		Providers: s.providers,
	})
}

// registerAdvancedChannelMethods registers node, device, cron-advanced, skill, and
// config-advanced methods.
func (s *Server) registerAdvancedChannelMethods(broadcastFn func(string, any) (int, []error)) {
	canvasHost := ""
	if s.runtimeCfg != nil {
		canvasHost = fmt.Sprintf("http://%s:%d", s.runtimeCfg.BindHost, s.runtimeCfg.Port)
	}
	rpc.RegisterNodeMethods(s.dispatcher, rpc.NodeDeps{
		Nodes:       s.nodes,
		Broadcaster: broadcastFn,
		CanvasHost:  canvasHost,
	})

	rpc.RegisterDeviceMethods(s.dispatcher, rpc.DeviceDeps{
		Devices:     s.devices,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterCronAdvancedMethods(s.dispatcher, rpc.CronAdvancedDeps{
		Cron:        s.cron,
		RunLog:      s.cronRunLog,
		Broadcaster: broadcastFn,
	})

	rpc.RegisterConfigAdvancedMethods(s.dispatcher, rpc.ConfigAdvancedDeps{
		Broadcaster: broadcastFn,
	})

	rpc.RegisterSkillMethods(s.dispatcher, rpc.SkillDeps{
		Skills:      s.skills,
		Broadcaster: broadcastFn,
	})
}

// registerSystemServiceMethods registers native system management (usage, logs, doctor,
// maintenance, update) and channel plugin (Telegram, Discord) methods.
func (s *Server) registerSystemServiceMethods(denebDir string) {
	rpc.RegisterUsageMethods(s.dispatcher, rpc.UsageDeps{
		Tracker: s.usageTracker,
	})

	rpc.RegisterLogsMethods(s.dispatcher, rpc.LogsDeps{
		LogDir: filepath.Join(denebDir, "logs"),
	})

	rpc.RegisterDoctorMethods(s.dispatcher, rpc.DoctorDeps{})

	rpc.RegisterMaintenanceMethods(s.dispatcher, rpc.MaintenanceDeps{
		Runner: s.maintRunner,
	})

	rpc.RegisterUpdateMethods(s.dispatcher, rpc.UpdateDeps{
		DenebDir: denebDir,
	})

	// Telegram native channel plugin + messaging methods.
	// Loads Telegram config from deneb.json if available.
	if s.runtimeCfg != nil {
		tgCfg := loadTelegramConfig(s.runtimeCfg)
		if tgCfg != nil && tgCfg.BotToken != "" {
			s.telegramPlug = telegram.NewPlugin(tgCfg, s.logger)
			s.channels.Register(s.telegramPlug)
		}
	}
	rpc.RegisterMessagingMethods(s.dispatcher, rpc.MessagingDeps{
		TelegramPlugin: s.telegramPlug,
	})

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
