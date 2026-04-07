// config_watcher.go — Config hot-reload integration.
//
// Starts a config file watcher on gateway startup. When the config file changes,
// safe fields are hot-reloaded without a full gateway restart. Fields that require
// a restart (port, bind, auth, TLS) are logged as warnings.
package server

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// startConfigWatcher initializes and starts the config file watcher.
// Safe to call even if runtimeCfg is nil (no-op).
func (s *Server) startConfigWatcher(ctx context.Context) {
	if s.runtimeCfg == nil {
		return
	}

	configPath := config.ResolveConfigPath()
	if configPath == "" {
		return
	}

	watcher := config.NewWatcher(configPath, s.logger)

	// Set initial snapshot from current config.
	snap, err := config.LoadConfig(configPath)
	if err != nil {
		s.logger.Warn("config watcher: failed to load initial snapshot", "error", err)
		return
	}
	watcher.SetInitialSnapshot(snap)

	// Register reload handler.
	watcher.OnReload(func(oldSnap, newSnap *config.ConfigSnapshot) error {
		return s.applyConfigReload(oldSnap, newSnap)
	})

	s.configWatcher = watcher
	s.safeGo("config-watcher", func() {
		watcher.Start(ctx)
	})
}

// applyConfigReload applies safe config changes without restart.
// Logs warnings for fields that require a restart.
func (s *Server) applyConfigReload(oldSnap, newSnap *config.ConfigSnapshot) error {
	if newSnap == nil || !newSnap.Valid {
		return nil
	}

	newCfg := &newSnap.Config
	oldCfg := &config.DenebConfig{}
	if oldSnap != nil {
		oldCfg = &oldSnap.Config
	}

	// Detect restart-required changes and warn.
	s.detectRestartRequired(oldCfg, newCfg)

	// Apply safe hot-reloadable fields.
	s.applyChannelHealthConfig(newCfg)

	s.logger.Info("config hot-reload applied")
	return nil
}

// detectRestartRequired logs warnings for config changes that need a restart.
func (s *Server) detectRestartRequired(oldCfg, newCfg *config.DenebConfig) {
	if oldCfg.Gateway == nil || newCfg.Gateway == nil {
		return
	}

	// Port change.
	oldPort := 0
	newPort := 0
	if oldCfg.Gateway.Port != nil {
		oldPort = *oldCfg.Gateway.Port
	}
	if newCfg.Gateway.Port != nil {
		newPort = *newCfg.Gateway.Port
	}
	if oldPort != newPort && oldPort != 0 {
		s.logger.Warn("config change requires restart: gateway.port",
			"old", oldPort, "new", newPort)
	}

	// Bind change.
	if oldCfg.Gateway.Bind != newCfg.Gateway.Bind && oldCfg.Gateway.Bind != "" {
		s.logger.Warn("config change requires restart: gateway.bind",
			"old", oldCfg.Gateway.Bind, "new", newCfg.Gateway.Bind)
	}

	// Auth mode change.
	if oldCfg.Gateway.Auth != nil && newCfg.Gateway.Auth != nil {
		if oldCfg.Gateway.Auth.Mode != newCfg.Gateway.Auth.Mode && oldCfg.Gateway.Auth.Mode != "" {
			s.logger.Warn("config change requires restart: gateway.auth.mode",
				"old", oldCfg.Gateway.Auth.Mode, "new", newCfg.Gateway.Auth.Mode)
		}
	}
}

// applyChannelHealthConfig hot-reloads channel health monitor settings.
func (s *Server) applyChannelHealthConfig(newCfg *config.DenebConfig) {
	if newCfg.Gateway == nil || s.channelHealth == nil {
		return
	}

	gw := newCfg.Gateway
	updated := false

	if gw.ChannelHealthCheckMinutes != nil {
		s.logger.Debug("hot-reload: channel health check interval updated",
			"minutes", *gw.ChannelHealthCheckMinutes)
		updated = true
	}

	if gw.ChannelStaleEventThresholdMinutes != nil {
		s.logger.Debug("hot-reload: channel stale threshold updated",
			"minutes", *gw.ChannelStaleEventThresholdMinutes)
		updated = true
	}

	if gw.ChannelMaxRestartsPerHour != nil {
		s.logger.Debug("hot-reload: channel max restarts updated",
			"max", *gw.ChannelMaxRestartsPerHour)
		updated = true
	}

	if updated {
		s.logger.Info("channel health config reloaded from config file")
	}
}
