// Package bridge manages the Node.js plugin host subprocess.
//
// The Go gateway delegates plugin/extension execution to a Node.js process
// that hosts the TypeScript plugin SDK. Communication uses Unix domain sockets
// with the existing frame-based protocol (RequestFrame/ResponseFrame).
package bridge

import (
	"encoding/json"
	"fmt"
	"sync"
)

// PluginHost manages a Node.js subprocess for plugin execution.
type PluginHost struct {
	mu      sync.Mutex
	running bool
}

// RequestFrame mirrors the TypeScript RequestFrame for plugin calls.
type RequestFrame struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ResponseFrame mirrors the TypeScript ResponseFrame from plugin calls.
type ResponseFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorShape     `json:"error,omitempty"`
}

// ErrorShape mirrors the TypeScript ErrorShape.
type ErrorShape struct {
	Code         string          `json:"code"`
	Message      string          `json:"message"`
	Details      json.RawMessage `json:"details,omitempty"`
	Retryable    *bool           `json:"retryable,omitempty"`
	RetryAfterMs *uint64         `json:"retryAfterMs,omitempty"`
	Cause        *string         `json:"cause,omitempty"`
}

// New creates a new PluginHost (not yet started).
func New() *PluginHost {
	return &PluginHost{}
}

// NewRequestFrame creates a request frame for a plugin method call.
func NewRequestFrame(id, method string, params any) (*RequestFrame, error) {
	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		rawParams = b
	}
	return &RequestFrame{
		Type:   "req",
		ID:     id,
		Method: method,
		Params: rawParams,
	}, nil
}

// IsRunning reports whether the plugin host subprocess is active.
func (h *PluginHost) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}
