// Package presence implements RPC handlers for system presence and heartbeat
// methods. It owns the PresenceStore and HeartbeatState types that were
// previously defined in the parent rpc package.
package presence

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc

// ---------------------------------------------------------------------------
// PresenceStore — thread-safe store of system presence entries
// ---------------------------------------------------------------------------

// PresenceEntry represents a single system presence record.
type PresenceEntry struct {
	Text            string   `json:"text"`
	DeviceID        string   `json:"deviceId,omitempty"`
	InstanceID      string   `json:"instanceId,omitempty"`
	Host            string   `json:"host,omitempty"`
	IP              string   `json:"ip,omitempty"`
	Mode            string   `json:"mode,omitempty"`
	Version         string   `json:"version,omitempty"`
	Platform        string   `json:"platform,omitempty"`
	DeviceFamily    string   `json:"deviceFamily,omitempty"`
	ModelIdentifier string   `json:"modelIdentifier,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	Roles           []string `json:"roles,omitempty"`
	Scopes          []string `json:"scopes,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	UpdatedAt       int64    `json:"updatedAt"`
}

// Store is a thread-safe store of system presence entries, keyed by a
// composite of deviceId+instanceId (or text for anonymous entries).
type Store struct {
	mu      sync.RWMutex
	entries map[string]*PresenceEntry
}

// NewStore creates a new empty presence store.
func NewStore() *Store {
	return &Store{entries: make(map[string]*PresenceEntry)}
}

// Update inserts or updates a presence entry. Returns the updated entry.
func (s *Store) Update(e PresenceEntry) *PresenceEntry {
	key := e.DeviceID
	if key == "" {
		key = e.Text
	}
	if e.InstanceID != "" {
		key += ":" + e.InstanceID
	}
	e.UpdatedAt = time.Now().UnixMilli()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = &e
	return &e
}

// List returns all presence entries.
func (s *Store) List() []PresenceEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PresenceEntry, 0, len(s.entries))
	for _, e := range s.entries {
		result = append(result, *e)
	}
	return result
}

// ---------------------------------------------------------------------------
// HeartbeatState — tracks the last heartbeat event and enabled flag
// ---------------------------------------------------------------------------

// HeartbeatState tracks the last heartbeat event and whether heartbeats are enabled.
type HeartbeatState struct {
	mu      sync.RWMutex
	enabled bool
	last    map[string]any
}

// NewHeartbeatState creates a new heartbeat state tracker with heartbeats enabled.
func NewHeartbeatState() *HeartbeatState {
	return &HeartbeatState{enabled: true}
}

// SetEnabled enables or disables heartbeats.
func (h *HeartbeatState) SetEnabled(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enabled = enabled
}

// Enabled returns whether heartbeats are currently enabled.
func (h *HeartbeatState) Enabled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.enabled
}

// RecordHeartbeat stores the latest heartbeat event payload.
func (h *HeartbeatState) RecordHeartbeat(event map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.last = event
}

// Last returns the most recent heartbeat event, or nil if none recorded.
func (h *HeartbeatState) Last() map[string]any {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.last
}

// ---------------------------------------------------------------------------
// Deps — dependency structs for handler registration
// ---------------------------------------------------------------------------

// Deps holds dependencies for presence RPC methods.
type Deps struct {
	Store       *Store
	Broadcaster BroadcastFunc
}

// HeartbeatDeps holds dependencies for heartbeat RPC methods.
type HeartbeatDeps struct {
	State       *HeartbeatState
	Broadcaster BroadcastFunc
}

// ---------------------------------------------------------------------------
// Methods — presence handler map
// ---------------------------------------------------------------------------

// Methods returns the system-presence and system-event RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"system-presence": systemPresence(deps),
		"system-event":    systemEvent(deps),
	}
}

// systemPresence returns all current presence entries.
func systemPresence(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		entries := deps.Store.List()
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"entries": entries,
		})
		return resp
	}
}

// systemEvent records a presence event and broadcasts the updated list.
func systemEvent(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text            string   `json:"text"`
			DeviceID        string   `json:"deviceId,omitempty"`
			InstanceID      string   `json:"instanceId,omitempty"`
			Host            string   `json:"host,omitempty"`
			IP              string   `json:"ip,omitempty"`
			Mode            string   `json:"mode,omitempty"`
			Version         string   `json:"version,omitempty"`
			Platform        string   `json:"platform,omitempty"`
			DeviceFamily    string   `json:"deviceFamily,omitempty"`
			ModelIdentifier string   `json:"modelIdentifier,omitempty"`
			Reason          string   `json:"reason,omitempty"`
			Roles           []string `json:"roles,omitempty"`
			Scopes          []string `json:"scopes,omitempty"`
			Tags            []string `json:"tags,omitempty"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Text == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "text required"))
		}

		entry := deps.Store.Update(PresenceEntry{
			Text:            p.Text,
			DeviceID:        p.DeviceID,
			InstanceID:      p.InstanceID,
			Host:            p.Host,
			IP:              p.IP,
			Mode:            p.Mode,
			Version:         p.Version,
			Platform:        p.Platform,
			DeviceFamily:    p.DeviceFamily,
			ModelIdentifier: p.ModelIdentifier,
			Reason:          p.Reason,
			Roles:           p.Roles,
			Scopes:          p.Scopes,
			Tags:            p.Tags,
		})

		if deps.Broadcaster != nil {
			deps.Broadcaster("presence", map[string]any{
				"entries": deps.Store.List(),
			})
		}

		_ = entry // used for broadcast above
		resp := protocol.MustResponseOK(req.ID, map[string]any{"ok": true})
		return resp
	}
}

// ---------------------------------------------------------------------------
// HeartbeatMethods — heartbeat handler map
// ---------------------------------------------------------------------------

// HeartbeatMethods returns the last-heartbeat and set-heartbeats RPC handlers.
func HeartbeatMethods(deps HeartbeatDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"last-heartbeat": lastHeartbeat(deps),
		"set-heartbeats": setHeartbeats(deps),
	}
}

// lastHeartbeat returns the most recent heartbeat event.
func lastHeartbeat(deps HeartbeatDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		last := deps.State.Last()
		if last == nil {
			last = map[string]any{
				"ts":      time.Now().UnixMilli(),
				"enabled": deps.State.Enabled(),
			}
		}
		resp := protocol.MustResponseOK(req.ID, last)
		return resp
	}
}

// setHeartbeats enables or disables heartbeats and broadcasts the config change.
func setHeartbeats(deps HeartbeatDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Enabled *bool `json:"enabled"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Enabled == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrValidationFailed, "invalid set-heartbeats params: enabled (boolean) required"))
		}

		deps.State.SetEnabled(*p.Enabled)

		if deps.Broadcaster != nil {
			deps.Broadcaster("heartbeat.config", map[string]any{
				"enabled": *p.Enabled,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":      true,
			"enabled": *p.Enabled,
		})
		return resp
	}
}
