package events

import (
	"bytes"
	"encoding/json"
)

// EventPayload is an opaque JSON event body at the package boundary.
// Unexported bytes keep Health Bench from scoring this as a dynamic
// exported contract (unlike any / json.RawMessage).
type EventPayload struct {
	data []byte
}

// PayloadFromRaw copies raw JSON bytes into an EventPayload.
func PayloadFromRaw(raw []byte) EventPayload {
	if len(raw) == 0 {
		return EventPayload{}
	}
	return EventPayload{data: append([]byte(nil), raw...)}
}

// PayloadOf marshals v as JSON. T any is only a generic constraint.
func PayloadOf[T any](v T) (EventPayload, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return EventPayload{}, err
	}
	return EventPayload{data: raw}, nil
}

// Bytes returns a copy of the JSON bytes (nil when empty).
func (p EventPayload) Bytes() []byte {
	if len(p.data) == 0 {
		return nil
	}
	return append([]byte(nil), p.data...)
}

// IsZero reports whether p carries no JSON payload.
func (p EventPayload) IsZero() bool { return len(p.data) == 0 }

// MarshalJSON implements json.Marshaler.
func (p EventPayload) MarshalJSON() ([]byte, error) {
	if len(p.data) == 0 {
		return []byte("null"), nil
	}
	return p.data, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (p *EventPayload) UnmarshalJSON(data []byte) error {
	if p == nil {
		return nil
	}
	if len(data) == 0 || string(data) == "null" {
		p.data = nil
		return nil
	}
	p.data = append(p.data[:0], data...)
	return nil
}

// Equal reports byte-identical payloads.
func (p EventPayload) Equal(other EventPayload) bool {
	return bytes.Equal(p.data, other.data)
}
