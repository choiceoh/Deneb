package server

import (
	"context"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	handlersystem "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/system"
)

// registerConfigLifecycleMethods stays as a standalone helper because it
// contains debounce timer logic that goes beyond simple Deps wiring.
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
}

// propagateConfigReload performs the post-reload side effects: hooks, channel
// restart (bounded by deferralTimeoutMs), cron restart, and process env cache
// invalidation.
func (s *Server) propagateConfigReload(snap *config.ConfigSnapshot, deferralTimeoutMs int) {
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
	// Restart autonomous service so periodic tasks pick up config changes.
	if s.autonomousSvc != nil {
		s.safeGo("config:restart-autonomous", func() {
			s.autonomousSvc.Stop()
			s.autonomousSvc.Start()
			s.logger.Info("config reload: autonomous service restarted")
		})
	}
	// Stop autoresearch if running; user can re-trigger via tool.
	if s.autoresearchRunner != nil && s.autoresearchRunner.IsRunning() {
		s.safeGo("config:stop-autoresearch", func() {
			s.autoresearchRunner.Stop()
			s.logger.Info("config reload: autoresearch runner stopped")
		})
	}
}
