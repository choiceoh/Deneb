package protocol

import (
	"encoding/json"
	"testing"
)

func TestNewRequestFrame(t *testing.T) {
	req, err := NewRequestFrame("req-1", "health", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	if req.Type != FrameTypeRequest {
		t.Errorf("Type = %q, want %q", req.Type, FrameTypeRequest)
	}
	if req.ID != "req-1" {
		t.Errorf("ID = %q, want %q", req.ID, "req-1")
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded RequestFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Type != req.Type || decoded.ID != req.ID || decoded.Method != req.Method {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}

func TestNewRequestFrameNilParams(t *testing.T) {
	req, err := NewRequestFrame("req-2", "status", nil)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	if req.Params != nil {
		t.Errorf("Params should be nil, got %s", string(req.Params))
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := m["params"]; ok {
		t.Error("params should be omitted when nil")
	}
}

func TestResponseFrameOK(t *testing.T) {
	resp, err := NewResponseOK("resp-1", map[string]string{"status": "ok"})
	if err != nil {
		t.Fatalf("NewResponseOK: %v", err)
	}
	if !resp.OK {
		t.Error("OK should be true")
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ResponseFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != resp.ID || decoded.OK != resp.OK {
		t.Errorf("round-trip mismatch: got %+v", decoded)
	}
}

func TestResponseFrameError(t *testing.T) {
	resp := NewResponseError("resp-2", NewError(ErrNotFound, "session not found"))
	if resp.OK {
		t.Error("OK should be false")
	}
	if resp.Error == nil || resp.Error.Code != ErrNotFound {
		t.Errorf("Error.Code = %v, want %q", resp.Error, ErrNotFound)
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ResponseFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Error == nil || decoded.Error.Code != ErrNotFound {
		t.Errorf("error round-trip mismatch: got %+v", decoded.Error)
	}
}

func TestEventFrameRoundTrip(t *testing.T) {
	ev, err := NewEventFrame("tick", map[string]int64{"ts": 1234567890})
	if err != nil {
		t.Fatalf("NewEventFrame: %v", err)
	}
	if ev.Type != FrameTypeEvent {
		t.Errorf("Type = %q, want %q", ev.Type, FrameTypeEvent)
	}

	seq := uint64(42)
	ev.Seq = &seq
	ev.StateVersion = &StateVersion{Health: 1}

	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded EventFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Event != ev.Event {
		t.Errorf("Event = %q, want %q", decoded.Event, ev.Event)
	}
	if decoded.Seq == nil || *decoded.Seq != 42 {
		t.Error("Seq round-trip failed")
	}
	if decoded.StateVersion == nil || decoded.StateVersion.Health != 1 {
		t.Error("StateVersion round-trip failed")
	}
}

func TestParseFrameType(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    FrameType
		wantErr bool
	}{
		{"request", `{"type":"req","id":"1","method":"health"}`, FrameTypeRequest, false},
		{"response", `{"type":"res","id":"1","ok":true}`, FrameTypeResponse, false},
		{"event", `{"type":"event","event":"tick"}`, FrameTypeEvent, false},
		{"missing type", `{"id":"1"}`, "", true},
		{"invalid json", `not json`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFrameType([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorCodeConstants(t *testing.T) {
	codes := []string{
		ErrNotLinked, ErrNotPaired, ErrAgentTimeout, ErrInvalidRequest,
		ErrUnavailable, ErrMissingParam, ErrNotFound, ErrUnauthorized,
		ErrValidationFailed, ErrConflict, ErrForbidden, ErrNodeDisconnected,
		ErrDependencyFailed, ErrFeatureDisabled,
	}
	if len(codes) != 14 {
		t.Errorf("expected 14 error codes, got %d", len(codes))
	}
	seen := make(map[string]bool)
	for _, c := range codes {
		if c == "" {
			t.Error("empty error code")
		}
		if seen[c] {
			t.Errorf("duplicate error code: %s", c)
		}
		seen[c] = true
	}
}
