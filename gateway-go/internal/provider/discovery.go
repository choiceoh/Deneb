package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
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

// Forwarder sends requests to the Node.js plugin host.
type Forwarder interface {
	Forward(ctx context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error)
}

// DiscoverFromBridge queries the Node.js plugin host for available providers
// and registers stub plugins for each discovered provider.
func (r *Registry) DiscoverFromBridge(ctx context.Context, fwd Forwarder) error {
	if fwd == nil {
		return nil
	}

	params, _ := json.Marshal(map[string]any{})
	req := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     fmt.Sprintf("discover-providers-%d", time.Now().UnixNano()),
		Method: "providers.list",
		Params: params,
	}

	resp, err := fwd.Forward(ctx, req)
	if err != nil {
		return err
	}
	if !resp.OK {
		return nil // Non-fatal: bridge may not support this method yet.
	}

	var result struct {
		Providers []struct {
			ID      string       `json:"id"`
			Label   string       `json:"label"`
			Auth    []AuthMethod `json:"auth,omitempty"`
			Aliases []string     `json:"aliases,omitempty"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil // Non-fatal: unexpected shape.
	}

	for _, p := range result.Providers {
		if r.Get(p.ID) != nil {
			continue // Already registered.
		}
		_ = r.Register(&bridgeStubPlugin{
			id:      p.ID,
			label:   p.Label,
			auth:    p.Auth,
			aliases: p.Aliases,
		})
	}
	return nil
}

// bridgeStubPlugin is a lightweight provider plugin registered from bridge discovery.
// It implements Plugin and AliasProvider but delegates actual work to the bridge.
type bridgeStubPlugin struct {
	id      string
	label   string
	auth    []AuthMethod
	aliases []string
}

func (p *bridgeStubPlugin) ID() string             { return p.id }
func (p *bridgeStubPlugin) Label() string           { return p.label }
func (p *bridgeStubPlugin) AuthMethods() []AuthMethod { return p.auth }
func (p *bridgeStubPlugin) Aliases() []string       { return p.aliases }
