package server

import (
	"sync"
	"time"

	handlerwire "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
)

// registerConfigLifecycleMethods stays as a standalone helper because it
// contains debounce timer logic that goes beyond simple Deps wiring.
func (s *Server) registerConfigLifecycleMethods() {
	// Resolve reload debounce/deferral settings from infrabind.
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

	// Debounce timer: collapses rapid infrabind.reload calls into a single
	// propagation pass using gateway.reload.debounceMs.
	var debounceMu sync.Mutex
	var debounceTimer *time.Timer

	s.dispatcher.RegisterDomain(handlerwire.SystemConfigReloadMethods(handlerwire.SystemConfigReloadDeps{
		OnReloaded: func(snap *infrabind.ConfigSnapshot) {
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
func (s *Server) propagateConfigReload(_ *infrabind.ConfigSnapshot, deferralTimeoutMs int) {
	// Broadcast config change to subscribers via publisher.
	s.publisher.PublishConfigChanged("config")

	// Invalidate the process manager's cached environment so new processes
	// pick up any env changes introduced by the reloaded infrabind.
	if s.processes != nil {
		s.processes.InvalidateEnvCache()
	}

	// Restart autonomous service so periodic tasks pick up config changes.
	if s.autonomousSvc != nil {
		s.safeGo("config:restart-autonomous", func() {
			s.autonomousSvc.Stop()
			s.autonomousSvc.Start()
			s.logger.Info("config reload: autonomous service restarted")
		})
	}
}
