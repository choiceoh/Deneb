package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/plugin"
	"github.com/choiceoh/deneb/gateway-go/internal/server/pluginrouter"
)

// PluginSubsystem groups plugin registry, discovery, hook execution, HTTP routing,
// and conversation binding state. All fields are late-initialized at the end of
// New() after the chat handler is created.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type PluginSubsystem struct {
	pluginFullRegistry    *plugin.FullRegistry
	pluginDiscoverer      *plugin.PluginDiscoverer
	pluginTypedHookRunner *plugin.TypedHookRunner
	pluginRouter          *pluginrouter.Router
}
