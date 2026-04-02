package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
)

// initHooksFromConfig creates the hook registries and loads user-defined hooks
// from deneb.json so they fire on gateway events.
func (s *Server) initHooksFromConfig() {
	s.hooks = hooks.NewRegistry(s.logger)
	if snap, err := config.LoadConfigFromDefaultPath(); err == nil && snap != nil && snap.Config.Hooks != nil {
		for _, entry := range snap.Config.Hooks.Entries {
			enabled := true
			if entry.Enabled != nil {
				enabled = *entry.Enabled
			}
			timeoutMs := int64(30000)
			if entry.TimeoutMs != nil {
				timeoutMs = int64(*entry.TimeoutMs)
			}
			blocking := false
			if entry.Blocking != nil {
				blocking = *entry.Blocking
			}
			if err := s.hooks.Register(hooks.Hook{
				ID:        entry.ID,
				Event:     hooks.Event(entry.Event),
				Command:   entry.Command,
				TimeoutMs: timeoutMs,
				Blocking:  blocking,
				Enabled:   enabled,
			}); err != nil {
				s.logger.Warn("failed to register hook", "id", entry.ID, "error", err)
			}
		}
		if len(snap.Config.Hooks.Entries) > 0 {
			s.logger.Info("loaded user-defined hooks from config", "count", len(snap.Config.Hooks.Entries))
		}
	}
	s.internalHooks = hooks.NewInternalRegistry(s.logger)
}
