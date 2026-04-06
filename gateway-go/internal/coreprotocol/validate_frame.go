package coreprotocol

import (
	"encoding/json"
	"fmt"
)

// FrameError describes a gateway frame validation failure.
type FrameError struct {
	Kind    string // "invalid_json", "unknown_type", "missing_field", "invalid_field"
	Field   string
	Message string
}

func (e *FrameError) Error() string { return e.Message }

// maxShortFieldLen is the maximum length for short string fields (id, method, event).
const maxShortFieldLen = 256

// ValidateFrame validates a JSON string as a gateway frame.
// Returns nil on success, or a FrameError describing the problem.
func ValidateFrame(jsonStr string) error {
	if len(jsonStr) == 0 {
		return &FrameError{Kind: "invalid_json", Message: "invalid JSON: unexpected end of input"}
	}

	var raw struct {
		Type         string          `json:"type"`
		ID           *string         `json:"id"`
		Method       *string         `json:"method"`
		OK           *bool           `json:"ok"`
		Event        *string         `json:"event"`
		Seq          *float64        `json:"seq"`
		Payload      json.RawMessage `json:"payload"`
		Params       json.RawMessage `json:"params"`
		Error        json.RawMessage `json:"error"`
		StateVersion json.RawMessage `json:"stateVersion"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return &FrameError{Kind: "invalid_json", Message: "invalid JSON: " + err.Error()}
	}

	switch raw.Type {
	case "req":
		if err := validateNonEmpty(raw.ID, "id"); err != nil {
			return err
		}
		if err := validateNonEmpty(raw.Method, "method"); err != nil {
			return err
		}
		return nil

	case "res":
		if err := validateNonEmpty(raw.ID, "id"); err != nil {
			return err
		}
		if raw.OK == nil {
			return &FrameError{Kind: "missing_field", Field: "ok", Message: "missing required field: ok"}
		}
		// If error field is present, validate it can be parsed as ErrorShape.
		if len(raw.Error) > 0 && string(raw.Error) != "null" {
			var es struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(raw.Error, &es); err != nil {
				return &FrameError{Kind: "invalid_json", Message: "invalid JSON: " + err.Error()}
			}
		}
		return nil

	case "event":
		if err := validateNonEmpty(raw.Event, "event"); err != nil {
			return err
		}
		if raw.Seq != nil {
			s := *raw.Seq
			if s < 0 {
				return &FrameError{
					Kind:    "invalid_field",
					Field:   "seq",
					Message: fmt.Sprintf("invalid field value: seq — must be non-negative, got %g", s),
				}
			}
			// Ensure it's a whole number.
			if s != float64(int64(s)) {
				return &FrameError{
					Kind:    "invalid_field",
					Field:   "seq",
					Message: "invalid field value: seq — must be integer",
				}
			}
		}
		return nil

	case "":
		return &FrameError{Kind: "missing_field", Field: "type", Message: "missing required field: type"}

	default:
		return &FrameError{Kind: "unknown_type", Message: "unknown frame type: " + raw.Type}
	}
}

func validateNonEmpty(value *string, field string) error {
	if value == nil {
		return &FrameError{
			Kind:    "missing_field",
			Field:   field,
			Message: "missing required field: " + field,
		}
	}
	if *value == "" {
		return &FrameError{
			Kind:    "invalid_field",
			Field:   field,
			Message: fmt.Sprintf("invalid field value: %s — must be non-empty", field),
		}
	}
	if len(*value) > maxShortFieldLen {
		return &FrameError{
			Kind:  "invalid_field",
			Field: field,
			Message: fmt.Sprintf("invalid field value: %s — exceeds maximum length (%d > %d)",
				field, len(*value), maxShortFieldLen),
		}
	}
	return nil
}
