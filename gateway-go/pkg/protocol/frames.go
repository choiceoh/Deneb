// Package protocol defines the gateway wire protocol types.
//
// These types mirror the Protobuf definitions in proto/gateway.proto
// and the TypeScript types in src/gateway/protocol/schema/frames.ts.
package protocol

import "encoding/json"

// FrameType discriminates gateway frames.
type FrameType string

const (
	FrameTypeRequest  FrameType = "req"
	FrameTypeResponse FrameType = "res"
	FrameTypeEvent    FrameType = "event"
)

// RequestFrame is a client-to-server RPC request.
type RequestFrame struct {
	Type   FrameType       `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ResponseFrame is a server-to-client RPC response.
type ResponseFrame struct {
	Type    FrameType       `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *ErrorShape     `json:"error,omitempty"`
}

// EventFrame is a server-to-client async event.
type EventFrame struct {
	Type         FrameType       `json:"type"`
	Event        string          `json:"event"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	Seq          *uint64         `json:"seq,omitempty"`
	StateVersion *StateVersion   `json:"stateVersion,omitempty"`
}

// ErrorShape describes an RPC error.
type ErrorShape struct {
	Code         string          `json:"code"`
	Message      string          `json:"message"`
	Details      json.RawMessage `json:"details,omitempty"`
	Retryable    *bool           `json:"retryable,omitempty"`
	RetryAfterMs *uint64         `json:"retryAfterMs,omitempty"`
	Cause        *string         `json:"cause,omitempty"`
}

// StateVersion tracks snapshot versioning for event diffing.
type StateVersion struct {
	Presence uint64 `json:"presence"`
	Health   uint64 `json:"health"`
}

// ProtocolVersion is the current gateway protocol version.
const ProtocolVersion = 3
