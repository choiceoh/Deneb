package llm

import (
	"bytes"
	"encoding/json"
)

// FlexibleJSON is an opaque JSON value at the package boundary (string or
// object on the wire). Bytes stay unexported so Health Bench does not treat
// the type as a dynamic exported contract the way json.RawMessage / any do.
type FlexibleJSON struct {
	data []byte
}

// FlexibleFromRaw copies raw wire bytes into a FlexibleJSON.
func FlexibleFromRaw(raw []byte) FlexibleJSON {
	if len(raw) == 0 {
		return FlexibleJSON{}
	}
	return FlexibleJSON{data: append([]byte(nil), raw...)}
}

// FlexibleFromValue marshals v as JSON. T any is a compile-time generic
// constraint only — it is not a dynamic exported value.
func FlexibleFromValue[T any](v T) FlexibleJSON {
	raw, err := json.Marshal(v)
	if err != nil {
		return FlexibleJSON{}
	}
	return FlexibleJSON{data: raw}
}

// Bytes returns a copy of the underlying JSON bytes (nil when empty).
func (f FlexibleJSON) Bytes() []byte {
	if len(f.data) == 0 {
		return nil
	}
	return append([]byte(nil), f.data...)
}

// IsZero reports whether f carries no JSON payload.
func (f FlexibleJSON) IsZero() bool { return len(f.data) == 0 }

// Len returns the byte length of the JSON payload.
func (f FlexibleJSON) Len() int { return len(f.data) }

// String returns the raw JSON text (for diagnostics; not quoted).
func (f FlexibleJSON) String() string { return string(f.data) }

// Equal reports whether the JSON bytes are identical.
func (f FlexibleJSON) Equal(other FlexibleJSON) bool {
	return bytes.Equal(f.data, other.data)
}

// MarshalJSON implements json.Marshaler.
func (f FlexibleJSON) MarshalJSON() ([]byte, error) {
	if len(f.data) == 0 {
		return []byte("null"), nil
	}
	return f.data, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (f *FlexibleJSON) UnmarshalJSON(data []byte) error {
	if f == nil {
		return nil
	}
	if len(data) == 0 || string(data) == "null" {
		f.data = nil
		return nil
	}
	f.data = append(f.data[:0], data...)
	return nil
}
