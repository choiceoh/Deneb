// Package protocol defines the gateway wire protocol types.
//
// These types mirror the Protobuf definitions in proto/gateway.proto
// and the TypeScript types in src/gateway/protocol/schema/frames.ts.
package protocol

import (
	"encoding/json"
	"fmt"
)

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

// NewError creates a new ErrorShape with the given code and message.
func NewError(code, message string) *ErrorShape {
	return &ErrorShape{Code: code, Message: message}
}

// NewRequestFrame creates a new RequestFrame with optional params.
// Both id and method must be non-empty.
func NewRequestFrame(id, method string, params any) (*RequestFrame, error) {
	if id == "" {
		return nil, fmt.Errorf("request id must not be empty")
	}
	if method == "" {
		return nil, fmt.Errorf("method must not be empty")
	}
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		raw = b
	}
	return &RequestFrame{
		Type:   FrameTypeRequest,
		ID:     id,
		Method: method,
		Params: raw,
	}, nil
}

// NewResponseOK creates a successful ResponseFrame.
func NewResponseOK(id string, payload any) (*ResponseFrame, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		raw = b
	}
	return &ResponseFrame{
		Type:    FrameTypeResponse,
		ID:      id,
		OK:      true,
		Payload: raw,
	}, nil
}

// NewResponseError creates an error ResponseFrame.
func NewResponseError(id string, err *ErrorShape) *ResponseFrame {
	return &ResponseFrame{
		Type:  FrameTypeResponse,
		ID:    id,
		OK:    false,
		Error: err,
	}
}

// NewEventFrame creates a new EventFrame.
func NewEventFrame(event string, payload any) (*EventFrame, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		raw = b
	}
	return &EventFrame{
		Type:    FrameTypeEvent,
		Event:   event,
		Payload: raw,
	}, nil
}

// ParseFrameType extracts the "type" field from raw JSON without full unmarshal.
func ParseFrameType(data []byte) (FrameType, error) {
	var peek struct {
		Type FrameType `json:"type"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return "", fmt.Errorf("parse frame type: %w", err)
	}
	if peek.Type == "" {
		return "", fmt.Errorf("missing frame type")
	}
	return peek.Type, nil
}
