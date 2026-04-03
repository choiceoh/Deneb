package server

import (
	"net/http"

	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/server/pluginrouter"
)

// initPluginSubsystem sets up the plugin registry, discoverer, typed hook
// runner, conversation bindings, and plugin HTTP router. Must be called after
// registerSessionRPCMethods() so chatHandler exists for late-binding.
func (s *Server) initPluginSubsystem() {
	s.pluginFullRegistry = plugin.NewFullRegistry(s.logger)
	s.pluginDiscoverer = plugin.NewPluginDiscoverer(s.logger)
	// Use the FullRegistry's hook runner so plugin-registered hooks and
	// chat-fired hooks share the same TypedHookRunner instance.
	s.pluginTypedHookRunner = s.pluginFullRegistry.HookRunner()
	// Late-bind: pluginTypedHookRunner was nil when chatHandler was constructed
	// (registerSessionRPCMethods runs before plugin init). Wire it now.
	if s.chatHandler != nil {
		s.chatHandler.SetPluginHookRunner(s.pluginTypedHookRunner)
	}
	s.dispatcher.RegisterDomain(handlerskill.PluginMethods(handlerskill.PluginDeps{
		PluginRegistry: &pluginRegistryAdapter{registry: s.pluginFullRegistry},
	}))

	// Plugin HTTP router with auth check backed by the gateway auth validator.
	var pluginAuthCheck func(r *http.Request) bool
	if s.authValidator != nil {
		pluginAuthCheck = func(r *http.Request) bool {
			token := extractBearerToken(r)
			if token == "" {
				return false
			}
			_, err := s.authValidator.ValidateToken(token)
			return err == nil
		}
	}
	s.pluginRouter = pluginrouter.New(s.logger, pluginAuthCheck)
}
