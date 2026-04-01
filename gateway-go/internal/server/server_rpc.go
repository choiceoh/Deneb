// Gateway Initialization Sequence
//
// server.New():
//   1. Core structs (ServerTransport, ServerRPC, ServerRuntime, ServerIntegrations)
//   2. Event infra (Broadcaster, Publisher, KeyCache, GatewaySubs)
//   3. Process manager, Cron scheduler/service, Hooks registry
//   4. Monitoring (activity tracker, channel events, auth rate limiter)
//   5. Provider auth (AuthManager, ProviderRuntime) — conditional
//   6. Workflow subsystems (approvals, nodes, devices, agents, skills, wizard, secrets)
//   7. ACP subsystem (registry, bindings, lifecycle sync)
//   8. RPC Dispatcher + middleware:
//      a. hub = buildHub()              — GatewayHub (Chat=nil at this point)
//      b. registerBuiltinMethods()      — gateway.status, gateway.ping
//      c. rpc.RegisterBuiltinMethods()  — session.list, session.get, etc.
//      d. registerEarlyMethods(hub)     — ~30 domains via hub adapters (method_registry.go)
//      e. registerSessionRPCMethods()   — chat pipeline init + handler creation
//      f. registerLateMethods(hub)      — Chat-dependent domains (method_registry.go)
//      g. registerWorkflowSideEffects() — non-RPC: autonomous, dreaming, notifier
//   9. Plugin system init
//
// initAndListen():
//  10. HTTP server + TLS
//  11. Background subsystems (tick broadcaster, monitoring, process pruner, session GC)
//  12. Telegram plugin start (channel callbacks wired in registerLateMethods)
//  13. Cron service start + session restore
//  14. Run state machine + autonomous service
//
// GatewayHub (gateway_hub.go):
//  Central service registry built from Server fields via buildHub().
//  Hub-to-Deps adapters in hub_adapters.go preserve handler testability.

package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	handlergateway "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/gateway"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func (s *Server) registerBuiltinMethods() {
	s.dispatcher.RegisterDomain(handlergateway.RuntimeMethods(handlergateway.Deps{
		Version:         s.version,
		StartedAt:       s.startedAt,
		RustFFI:         s.rustFFI,
		ChannelsStatus: func() any {
			if s.telegramPlug != nil {
				return map[string]telegram.Status{"telegram": s.telegramPlug.Status()}
			}
			return map[string]telegram.Status{}
		},
		SessionCount:    s.sessions.Count,
		ConnectionCount: func() int64 { return int64(s.clientCnt.Load()) },
		LastHeartbeatMs: func() int64 {
			if s.activity == nil {
				return 0
			}
			return s.activity.LastActivityAt()
		},
		Broadcast: s.broadcaster.Broadcast,
		Models: func() any {
			if s.providers == nil {
				return []any{}
			}
			return s.providers.List()
		},
		RuntimeConfig: func() map[string]any {
			if s.runtimeCfg == nil {
				return nil
			}
			return map[string]any{
				"bindHost":      s.runtimeCfg.BindHost,
				"port":          s.runtimeCfg.Port,
				"authMode":      s.runtimeCfg.AuthMode,
				"tailscaleMode": s.runtimeCfg.TailscaleMode,
			}
		},
		DaemonStatus: func() (any, bool) {
			if s.daemon == nil {
				return nil, false
			}
			return s.daemon.Status(), true
		},
		AgentActiveRuns: func() int {
			if s.jobTracker == nil {
				return 0
			}
			return s.jobTracker.ActiveRunCount()
		},
		AgentCacheSize: func() int {
			if s.jobTracker == nil {
				return 0
			}
			return s.jobTracker.CacheSize()
		},
	}))
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
	if p != nil {
		return &protocol.PluginHealthStatus{
			PluginID: p.ID,
			Healthy:  p.Enabled,
		}
	}
	return nil
}

// truncateForDedup returns at most maxLen bytes of s for use as a dedup key.
func truncateForDedup(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
