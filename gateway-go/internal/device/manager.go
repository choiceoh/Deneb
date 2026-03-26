// Package device manages device pairing and token lifecycle.
//
// This ports the TypeScript device management (src/gateway/server-methods/devices/)
// to Go, providing in-memory storage for paired devices and their tokens.
package device

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// PairState represents the state of a device pairing request.
type PairState string

const (
	PairStatePending  PairState = "pending"
	PairStateApproved PairState = "approved"
	PairStateRejected PairState = "rejected"
)

// PairedDevice represents a successfully paired device.
type PairedDevice struct {
	DeviceID   string    `json:"deviceId"`
	Label      string    `json:"label,omitempty"`
	Platform   string    `json:"platform,omitempty"`
	Token      string    `json:"token"`
	PairedAtMs int64     `json:"pairedAtMs"`
	LastSeenMs int64     `json:"lastSeenMs,omitempty"`
	State      PairState `json:"state"`
}

// PairEntry represents a pending or resolved device pair request.
type PairEntry struct {
	RequestID   string    `json:"requestId"`
	DeviceID    string    `json:"deviceId"`
	Label       string    `json:"label,omitempty"`
	Platform    string    `json:"platform,omitempty"`
	State       PairState `json:"state"`
	CreatedAtMs int64     `json:"createdAtMs"`
}

// Manager manages device pairing and token lifecycle.
type Manager struct {
	mu           sync.RWMutex
	pairRequests map[string]*PairEntry    // requestID -> entry
	devices      map[string]*PairedDevice // deviceID -> device
}

// NewManager creates a new device manager.
func NewManager() *Manager {
	return &Manager{
		pairRequests: make(map[string]*PairEntry),
		devices:      make(map[string]*PairedDevice),
	}
}

// ListPairs returns all pair entries (pending and resolved).
func (m *Manager) ListPairs() []*PairEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*PairEntry, 0, len(m.pairRequests))
	for _, e := range m.pairRequests {
		cp := *e
		result = append(result, &cp)
	}
	return result
}

// Approve approves a pending device pair request.
func (m *Manager) Approve(requestID string) (*PairedDevice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.pairRequests[requestID]
	if !ok {
		return nil, fmt.Errorf("pair request %q not found", requestID)
	}
	if entry.State != PairStatePending {
		return nil, fmt.Errorf("pair request %q already %s", requestID, entry.State)
	}

	entry.State = PairStateApproved
	token := genToken()
	now := time.Now().UnixMilli()

	dev := &PairedDevice{
		DeviceID:   entry.DeviceID,
		Label:      entry.Label,
		Platform:   entry.Platform,
		Token:      token,
		PairedAtMs: now,
		State:      PairStateApproved,
	}
	m.devices[entry.DeviceID] = dev

	cp := *dev
	return &cp, nil
}

// Reject rejects a pending device pair request.
func (m *Manager) Reject(requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.pairRequests[requestID]
	if !ok {
		return fmt.Errorf("pair request %q not found", requestID)
	}
	if entry.State != PairStatePending {
		return fmt.Errorf("pair request %q already %s", requestID, entry.State)
	}
	entry.State = PairStateRejected
	return nil
}

// Remove removes a paired device entirely.
func (m *Manager) Remove(deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.devices[deviceID]; !ok {
		return fmt.Errorf("device %q not found", deviceID)
	}
	delete(m.devices, deviceID)
	return nil
}

// RotateToken generates a new token for a paired device.
func (m *Manager) RotateToken(deviceID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return "", fmt.Errorf("device %q not found", deviceID)
	}
	newToken := genToken()
	dev.Token = newToken
	return newToken, nil
}

// RevokeToken clears the token for a device, effectively disconnecting it.
func (m *Manager) RevokeToken(deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dev, ok := m.devices[deviceID]
	if !ok {
		return fmt.Errorf("device %q not found", deviceID)
	}
	dev.Token = ""
	return nil
}

// AddPairRequest registers a new pair request (used during device pairing flow).
func (m *Manager) AddPairRequest(deviceID, label, platform string) *PairEntry {
	reqID := genID()
	entry := &PairEntry{
		RequestID:   reqID,
		DeviceID:    deviceID,
		Label:       label,
		Platform:    platform,
		State:       PairStatePending,
		CreatedAtMs: time.Now().UnixMilli(),
	}

	m.mu.Lock()
	m.pairRequests[reqID] = entry
	m.mu.Unlock()

	cp := *entry
	return &cp
}

func genID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func genToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
