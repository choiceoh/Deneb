package rpc

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

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

// PresenceStore is a thread-safe store of system presence entries, keyed by a
// composite of deviceId+instanceId (or text for anonymous entries).
type PresenceStore struct {
	mu      sync.RWMutex
	entries map[string]*PresenceEntry
}

// NewPresenceStore creates a new empty presence store.
func NewPresenceStore() *PresenceStore {
	return &PresenceStore{entries: make(map[string]*PresenceEntry)}
}

// Update inserts or updates a presence entry. Returns the updated entry.
func (s *PresenceStore) Update(e PresenceEntry) *PresenceEntry {
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
func (s *PresenceStore) List() []PresenceEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PresenceEntry, 0, len(s.entries))
	for _, e := range s.entries {
		result = append(result, *e)
	}
	return result
}

// PresenceDeps holds dependencies for presence RPC methods.
type PresenceDeps struct {
	Store       *PresenceStore
	Broadcaster BroadcastFunc
}

// RegisterPresenceMethods registers system-presence and system-event RPC methods.
func RegisterPresenceMethods(d *Dispatcher, deps PresenceDeps) {
	d.Register("system-presence", systemPresence(deps))
	d.Register("system-event", systemEvent(deps))
}

func systemPresence(deps PresenceDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		entries := deps.Store.List()
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"entries": entries,
		})
		return resp
	}
}

func systemEvent(deps PresenceDeps) HandlerFunc {
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
