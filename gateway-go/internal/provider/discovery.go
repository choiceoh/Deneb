package provider

import (
	"strings"
)

// NormalizeProviderID normalizes a provider identifier to its canonical form.
// Mirrors src/agents/provider-id.ts normalizeProviderId.
func NormalizeProviderID(id string) string {
	normalized := strings.TrimSpace(strings.ToLower(id))
	switch normalized {
	case "z.ai", "z-ai":
		return "zai"
	case "opencode-zen":
		return "opencode"
	case "opencode-go-auth":
		return "opencode-go"
	case "qwen":
		return "qwen-portal"
	case "kimi", "kimi-code", "kimi-coding":
		return "kimi"
	case "bedrock", "aws-bedrock":
		return "amazon-bedrock"
	case "bytedance", "doubao":
		return "volcengine"
	}
	return normalized
}

// NormalizeProviderIDForAuth normalizes a provider ID for auth lookup.
// Coding-plan variants share auth credentials with their base provider.
func NormalizeProviderIDForAuth(id string) string {
	normalized := NormalizeProviderID(id)
	switch normalized {
	case "volcengine-plan":
		return "volcengine"
	case "byteplus-plan":
		return "byteplus"
	}
	return normalized
}

// GetByNormalizedID looks up a provider plugin by normalized ID, checking
// both direct ID match and plugin aliases.
func (r *Registry) GetByNormalizedID(id string) Plugin {
	normalized := NormalizeProviderID(id)

	r.mu.RLock()
	defer r.mu.RUnlock()

	// Direct match first.
	if p, ok := r.providers[normalized]; ok {
		return p
	}

	// Check all providers for a matching normalized ID or alias.
	for pid, p := range r.providers {
		if NormalizeProviderID(pid) == normalized {
			return p
		}
		// Check aliases if the plugin supports them.
		if ap, ok := p.(AliasProvider); ok {
			for _, alias := range ap.Aliases() {
				if NormalizeProviderID(alias) == normalized {
					return p
				}
			}
		}
	}
	return nil
}

// AliasProvider is an optional interface for provider plugins that
// support alternative names.
type AliasProvider interface {
	Aliases() []string
}
