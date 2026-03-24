package provider

import (
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProtocolAdapter wraps a provider Registry and adapts it to the
// protocol.ProviderCatalog interface for RPC wire-format serialization.
type ProtocolAdapter struct {
	registry *Registry
}

// NewProtocolAdapter creates an adapter from a provider registry.
func NewProtocolAdapter(r *Registry) *ProtocolAdapter {
	return &ProtocolAdapter{registry: r}
}

// ListProviders returns provider metadata in the proto wire format.
func (a *ProtocolAdapter) ListProviders() []protocol.ProviderMeta {
	snap := a.registry.Snapshot()
	result := make([]protocol.ProviderMeta, 0, len(snap))
	for _, p := range snap {
		meta := protocol.ProviderMeta{
			ID:    p.ID(),
			Label: p.Label(),
		}
		// Extract auth method env vars as hints.
		for _, am := range p.AuthMethods() {
			if am.Kind == "api_key" && am.Hint != "" {
				meta.EnvVars = append(meta.EnvVars, am.Hint)
			}
		}
		result = append(result, meta)
	}
	return result
}

// ListCatalogEntries returns an empty list; catalog discovery requires
// async context and is populated by the Node.js bridge at runtime.
func (a *ProtocolAdapter) ListCatalogEntries() []protocol.ProviderCatalogEntry {
	return nil
}
