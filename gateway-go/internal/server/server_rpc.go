package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	handleragent "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/agent"
	handleraurorachannel "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/aurora_channel"
	handlergateway "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/gateway"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/process"
	handlerprovider "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/provider"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func (s *Server) registerExtendedMethods() {
	s.registerAgentMethods()
	s.registerProviderMethods()
	s.registerToolMethods()
	s.registerAuroraMethods()
	s.registerAuthRPCMethods()
	s.registerSessionRPCMethods()
}

func (s *Server) registerAgentMethods() {
	// ACP RPC methods.
	s.dispatcher.RegisterDomain(handlerprocess.ACPMethods(s.acpDeps))

	s.dispatcher.RegisterDomain(handleragent.ExtendedMethods(handleragent.ExtendedDeps{
		Sessions:    s.sessions,
		Channels:    s.channels,
		GatewaySubs: s.gatewaySubs,
		Processes:   s.processes,
		Cron:        s.cron,
		Hooks:       s.hooks,
		Broadcaster: s.broadcaster,
	}))
}

func (s *Server) registerProviderMethods() {
	s.dispatcher.RegisterDomain(handlerprovider.Methods(handlerprovider.Deps{
		Providers:   s.providers,
		AuthManager: s.authManager,
	}))
}

func (s *Server) registerToolMethods() {
	s.dispatcher.RegisterDomain(handlerskill.ToolMethods(handlerskill.ToolDeps{
		Processes: s.processes,
	}))
}

func (s *Server) registerAuroraMethods() {
	// Aurora channel methods (desktop app communication).
	s.dispatcher.RegisterDomain(handleraurorachannel.Methods(handleraurorachannel.Deps{
		Chat: s.chatHandler,
	}))
}

func (s *Server) registerPhase2Methods() {
	broadcastFn := func(event string, payload any) (int, []error) {
		return s.broadcaster.Broadcast(event, payload)
	}
	s.registerPhase2ChannelMethods(broadcastFn)
	s.registerPhase2SystemMethods(broadcastFn)
}

func (s *Server) registerPhase2ChannelMethods(broadcastFn func(string, any) (int, []error)) {
	s.registerEventsBroadcastMethods()
	s.registerConfigLifecycleMethods()
	s.registerSubscriptionMethods()
	s.registerHeartbeatMethods(broadcastFn)
}

func (s *Server) registerPhase2SystemMethods(broadcastFn func(string, any) (int, []error)) {
	s.registerMonitoringMethods()
	s.registerIdentityMethods()
	s.registerPresenceMethods(broadcastFn)
	s.registerModelsMethods()
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
	// Usage, logs, doctor, maintenance, update, and Telegram methods.
	s.registerSystemServiceMethods(denebDir)
}

func (s *Server) registerBuiltinMethods() {
	s.dispatcher.RegisterDomain(handlergateway.RuntimeMethods(handlergateway.Deps{
		Version:         s.version,
		StartedAt:       s.startedAt,
		RustFFI:         s.rustFFI,
		ChannelsStatus:  func() any { return s.channels.StatusAll() },
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
