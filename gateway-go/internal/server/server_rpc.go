package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func (s *Server) registerExtendedMethods() {
	// ACP RPC methods.
	rpc.RegisterACPMethods(s.dispatcher, s.acpDeps)

	rpc.RegisterExtendedMethods(s.dispatcher, rpc.ExtendedDeps{
		Sessions:    s.sessions,
		Channels:    s.channels,
		GatewaySubs: s.gatewaySubs,
		Processes:   s.processes,
		Cron:        s.cron,
		Hooks:       s.hooks,
		Broadcaster: s.broadcaster,
	})

	// Provider methods.
	rpc.RegisterProviderMethods(s.dispatcher, rpc.ProviderDeps{
		Providers:   s.providers,
		AuthManager: s.authManager,
	})

	// Tool methods.
	rpc.RegisterToolMethods(s.dispatcher, rpc.ToolDeps{
		Processes: s.processes,
	})

	// Aurora channel methods (desktop app communication).
	rpc.RegisterAuroraChannelMethods(s.dispatcher, rpc.AuroraChannelDeps{
		Chat: s.chatHandler,
	})

	// Auth, web-login stub, and channel-logout methods.
	s.registerAuthRPCMethods()

	// Session state, repair, daemon status, and chat pipeline methods.
	s.registerSessionRPCMethods()
}

func (s *Server) registerPhase2Methods() {
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}
	// Channel events, monitoring, lifecycle, heartbeat, presence, and model-list methods.
	s.registerChannelEventsMethods(broadcastFn)
}

// registerAdvancedWorkflowMethods registers Phase 3 RPC methods for exec approvals,
// nodes, devices, agents, cron advanced, config advanced, skills, wizard, secrets, and talk.
func (s *Server) registerAdvancedWorkflowMethods() {
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}
	// Exec approval, agents, talk, wizard, and autonomous methods.
	s.registerApprovalAgentMethods(broadcastFn)
	// Node, device, cron-advanced, skill, and config-advanced methods.
	s.registerAdvancedChannelMethods(broadcastFn)
	// Gmail polling service: periodic new-email analysis via LLM.
	s.initGmailPoll()
}

func (s *Server) registerNativeSystemMethods(denebDir string) {
	// Usage, logs, doctor, maintenance, update, Telegram, and Discord methods.
	s.registerSystemServiceMethods(denebDir)
}

func (s *Server) registerBuiltinMethods() {
	s.dispatcher.Register("health", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"status": "ok",
			"uptime": time.Since(s.startedAt).Milliseconds(),
		})
		return resp
	})

	s.dispatcher.Register("status", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"version":     s.version,
			"channels":    s.channels.StatusAll(),
			"sessions":    s.sessions.Count(),
			"connections": s.clientCnt.Load(),
		})
		return resp
	})

	// gateway.identity.get: returns the gateway's identity and runtime information.
	s.dispatcher.Register("gateway.identity.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"version": s.version,
			"runtime": "go",
			"uptime":  time.Since(s.startedAt).Milliseconds(),
			"rustFFI": s.rustFFI,
		})
		return resp
	})

	// last-heartbeat: returns the last heartbeat timestamp.
	s.dispatcher.Register("last-heartbeat", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var ts int64
		if s.activity != nil {
			ts = s.activity.LastActivityAt()
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"lastHeartbeatMs": ts,
		})
		return resp
	})

	// set-heartbeats: configure heartbeat settings (accepted but no-op in Go gateway;
	// the tick broadcaster runs at a fixed 1000ms interval).
	s.dispatcher.Register("set-heartbeats", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	})

	// system-presence: broadcast a presence event to all connected clients.
	s.dispatcher.Register("system-presence", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var payload any
		if len(req.Params) > 0 {
			var p struct {
				Payload any `json:"payload"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrInvalidRequest, "invalid params"))
			}
			payload = p.Payload
		}
		sent, _ := s.broadcaster.Broadcast("presence", payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})

	// system-event: broadcast an arbitrary system event.
	s.dispatcher.Register("system-event", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		var p struct {
			Event   string `json:"event"`
			Payload any    `json:"payload"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Event == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "event is required"))
		}
		sent, _ := s.broadcaster.Broadcast(p.Event, p.Payload)
		resp := protocol.MustResponseOK(req.ID, map[string]int{"sent": sent})
		return resp
	})

	// models.list: return provider model list if available.
	s.dispatcher.Register("models.list", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.providers == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]any{"models": []any{}})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"models": s.providers.List(),
		})
		return resp
	})

	// config.get: returns the resolved runtime config for diagnostics.
	s.dispatcher.Register("config.get", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if s.runtimeCfg == nil {
			resp := protocol.MustResponseOK(req.ID, map[string]string{"status": "not_loaded"})
			return resp
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"bindHost":      s.runtimeCfg.BindHost,
			"port":          s.runtimeCfg.Port,
			"authMode":      s.runtimeCfg.AuthMode,
			"tailscaleMode": s.runtimeCfg.TailscaleMode,
		})
		return resp
	})
}

// pluginRegistryAdapter bridges plugin.FullRegistry to the rpc.PluginRegistry interface.
type pluginRegistryAdapter struct {
	registry *plugin.FullRegistry
}

func (a *pluginRegistryAdapter) ListPlugins() []protocol.PluginMeta {
	raw := a.registry.ListPlugins()
	result := make([]protocol.PluginMeta, len(raw))
	for i, p := range raw {
		result[i] = protocol.PluginMeta{
			ID:      p.ID,
			Name:    p.Label,
			Kind:    protocol.PluginKind(p.Kind),
			Version: p.Version,
			Enabled: p.Enabled,
		}
	}
	return result
}

func (a *pluginRegistryAdapter) GetPluginHealth(id string) *protocol.PluginHealthStatus {
	p := a.registry.GetPlugin(id)
	if p == nil {
		return nil
	}
	return &protocol.PluginHealthStatus{
		PluginID: p.ID,
		Healthy:  p.Enabled,
	}
}

// truncateForDedup returns at most maxLen bytes of s for use as a dedup key.
func truncateForDedup(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
