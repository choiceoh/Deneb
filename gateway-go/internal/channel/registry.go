// Package channel manages channel plugin registration and dispatch.
//
// This mirrors the channel framework in src/channels/ and the plugin
// registry in src/channels/plugins/.
package channel

import (
	"context"
	"fmt"
	"sync"
)

// Capabilities describes what a channel supports.
// Mirrors proto/channel.proto ChannelCapabilities.
type Capabilities struct {
	ChatTypes      []string `json:"chatTypes"`
	Polls          bool     `json:"polls,omitempty"`
	Reactions      bool     `json:"reactions,omitempty"`
	Edit           bool     `json:"edit,omitempty"`
	Unsend         bool     `json:"unsend,omitempty"`
	Reply          bool     `json:"reply,omitempty"`
	Threads        bool     `json:"threads,omitempty"`
	Media          bool     `json:"media,omitempty"`
	BlockStreaming bool     `json:"blockStreaming,omitempty"`
}

// Meta describes channel metadata.
// Mirrors proto/channel.proto ChannelMeta.
type Meta struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	SelectionLabel string   `json:"selectionLabel"`
	DocsPath       string   `json:"docsPath"`
	Blurb          string   `json:"blurb"`
	Order          int      `json:"order,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
}

// Status represents the current runtime state of a channel.
type Status struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// Plugin is the interface that channel plugins must implement.
// This mirrors the ChannelPlugin contract from TypeScript.
type Plugin interface {
	ID() string
	Meta() Meta
	Capabilities() Capabilities
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Status() Status
}

// Registry manages registered channel plugins.
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
}

// NewRegistry creates an empty channel registry.
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

// Register adds a channel plugin to the registry.
func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := p.ID()
	if _, exists := r.plugins[id]; exists {
		return fmt.Errorf("channel %q already registered", id)
	}
	r.plugins[id] = p
	return nil
}

// Get returns a channel plugin by ID, or nil if not found.
func (r *Registry) Get(id string) Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.plugins[id]
}

// List returns all registered channel IDs.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.plugins))
	for id := range r.plugins {
		ids = append(ids, id)
	}
	return ids
}

// StatusAll returns the status of all registered channels.
func (r *Registry) StatusAll() map[string]Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]Status, len(r.plugins))
	for id, p := range r.plugins {
		result[id] = p.Status()
	}
	return result
}

// Snapshot returns a copy of all registered plugins for iteration
// without holding the registry lock.
func (r *Registry) Snapshot() map[string]Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	snap := make(map[string]Plugin, len(r.plugins))
	for id, p := range r.plugins {
		snap[id] = p
	}
	return snap
}
