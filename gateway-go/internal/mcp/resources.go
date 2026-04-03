package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// staticResources returns the fixed resource list.
func staticResources() []Resource {
	return []Resource{
		{URI: "deneb://status", Name: "System Status", Description: "Gateway version, hostname, architecture, uptime", MimeType: "application/json"},
		{URI: "deneb://sessions", Name: "Active Sessions", Description: "List of active sessions with status", MimeType: "application/json"},
		{URI: "deneb://config", Name: "Configuration", Description: "Current gateway configuration", MimeType: "application/json"},
		{URI: "deneb://skills", Name: "Skills", Description: "Installed skills and their status", MimeType: "application/json"},
		{URI: "deneb://models", Name: "Models", Description: "Available LLM models across providers", MimeType: "application/json"},
	}
}

// resourceRoute maps a URI prefix to a gateway RPC method.
var resourceRoutes = []struct {
	prefix    string
	rpcMethod string
}{
	{"deneb://status", "gateway.identity.get"},
	{"deneb://sessions", "sessions.list"},
	{"deneb://config", "config.get"},
	{"deneb://skills", "skills.status"},
	{"deneb://models", "models.list"},
}

// ResourceManager handles resource listing, reading, and subscriptions.
type ResourceManager struct {
	bridge *Bridge

	subsMu sync.RWMutex
	subs   map[string]bool // subscribed URIs
}

// NewResourceManager creates a resource manager.
func NewResourceManager(bridge *Bridge) *ResourceManager {
	return &ResourceManager{
		bridge: bridge,
		subs:   make(map[string]bool),
	}
}

// List returns all available resources.
func (rm *ResourceManager) List() []Resource {
	return staticResources()
}

// Read fetches the content of a resource by URI.
func (rm *ResourceManager) Read(ctx context.Context, uri string) (*ResourceReadResult, error) {
	// Match URI to RPC method.
	rpcMethod, params, err := rm.resolveURI(uri)
	if err != nil {
		return nil, err
	}

	var paramsRaw json.RawMessage
	if params != nil {
		paramsRaw, _ = json.Marshal(params)
	}

	payload, err := rm.bridge.Call(ctx, rpcMethod, paramsRaw)
	if err != nil {
		return nil, fmt.Errorf("resource read %s: %w", uri, err)
	}

	// Format the payload as readable JSON.
	var pretty json.RawMessage
	if err := json.Unmarshal(payload, &pretty); err != nil {
		pretty = payload
	}
	formatted, _ := json.MarshalIndent(pretty, "", "  ")

	return &ResourceReadResult{
		Contents: []ResourceContent{
			{URI: uri, MimeType: "application/json", Text: string(formatted)},
		},
	}, nil
}

// Subscribe registers interest in a resource URI.
func (rm *ResourceManager) Subscribe(uri string) {
	rm.subsMu.Lock()
	rm.subs[uri] = true
	rm.subsMu.Unlock()
}

// Unsubscribe removes interest in a resource URI.
func (rm *ResourceManager) Unsubscribe(uri string) {
	rm.subsMu.Lock()
	delete(rm.subs, uri)
	rm.subsMu.Unlock()
}

// IsSubscribed checks if a URI is subscribed.
func (rm *ResourceManager) IsSubscribed(uri string) bool {
	rm.subsMu.RLock()
	defer rm.subsMu.RUnlock()
	return rm.subs[uri]
}

// SubscribedURIs returns all subscribed URIs.
func (rm *ResourceManager) SubscribedURIs() []string {
	rm.subsMu.RLock()
	defer rm.subsMu.RUnlock()
	out := make([]string, 0, len(rm.subs))
	for uri := range rm.subs {
		out = append(out, uri)
	}
	return out
}

// resolveURI maps a resource URI to an RPC method and optional params.
func (rm *ResourceManager) resolveURI(uri string) (string, map[string]any, error) {
	// Dynamic URIs: deneb://sessions/{key}, deneb://memory/{query}
	if strings.HasPrefix(uri, "deneb://sessions/") {
		key := strings.TrimPrefix(uri, "deneb://sessions/")
		if strings.HasSuffix(key, "/history") {
			key = strings.TrimSuffix(key, "/history")
			return "sessions.preview", map[string]any{"keys": []string{key}}, nil
		}
		return "sessions.preview", map[string]any{"keys": []string{key}}, nil
	}
	if strings.HasPrefix(uri, "deneb://memory/") {
		query := strings.TrimPrefix(uri, "deneb://memory/")
		return "vega.memory-search", map[string]any{"query": query}, nil
	}

	// Static URIs.
	for _, route := range resourceRoutes {
		if uri == route.prefix {
			return route.rpcMethod, nil, nil
		}
	}

	return "", nil, fmt.Errorf("unknown resource URI: %s", uri)
}
