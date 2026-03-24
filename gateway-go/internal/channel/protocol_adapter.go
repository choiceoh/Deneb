package channel

import (
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProtocolAdapter wraps a channel Registry and adapts it to the
// protocol.PluginRegistry interface for RPC wire-format serialization.
// Channel plugins are exposed as plugins of kind "channel" in the
// unified plugin registry snapshot.
type ProtocolAdapter struct {
	registry *Registry
}

// NewProtocolAdapter creates an adapter from a channel registry.
func NewProtocolAdapter(r *Registry) *ProtocolAdapter {
	return &ProtocolAdapter{registry: r}
}

// ListPlugins returns channel plugins as protocol.PluginMeta.
func (a *ProtocolAdapter) ListPlugins() []protocol.PluginMeta {
	snap := a.registry.Snapshot()
	result := make([]protocol.PluginMeta, 0, len(snap))
	for _, p := range snap {
		status := p.Status()
		enabled := status.Connected
		result = append(result, protocol.PluginMeta{
			ID:      p.ID(),
			Name:    p.Meta().Label,
			Kind:    protocol.PluginKindChannel,
			Version: "0.0.0",
			Enabled: enabled,
		})
	}
	return result
}

// GetPluginHealth returns the health status for a channel plugin.
func (a *ProtocolAdapter) GetPluginHealth(id string) *protocol.PluginHealthStatus {
	ch := a.registry.Get(id)
	if ch == nil {
		return nil
	}
	status := ch.Status()
	health := &protocol.PluginHealthStatus{
		PluginID: id,
		Healthy:  status.Connected,
	}
	if status.Error != "" {
		health.Error = &status.Error
	}
	return health
}
