package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestNewRequestFrame(t *testing.T) {
	req := testutil.Must(NewRequestFrame("req-1", "health", map[string]string{"key": "value"}))
	if req.Type != FrameTypeRequest {
		t.Errorf("Type = %q, want %q", req.Type, FrameTypeRequest)
	}
	if req.ID != "req-1" {
		t.Errorf("ID = %q, want %q", req.ID, "req-1")
	}

	b := testutil.Must(json.Marshal(req))

	var decoded RequestFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*req, decoded) {
		t.Errorf("round-trip mismatch:\n  want: %+v\n   got: %+v", *req, decoded)
	}
}

func TestNewRequestFrameNilParams(t *testing.T) {
	req := testutil.Must(NewRequestFrame("req-2", "status", nil))
	if req.Params != nil {
		t.Errorf("Params should be nil, got %s", string(req.Params))
	}

	b := testutil.Must(json.Marshal(req))

	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, ok := m["params"]; ok {
		t.Error("params should be omitted when nil")
	}
}

func TestNewRequestFrameValidation(t *testing.T) {
	_, err := NewRequestFrame("", "health", nil)
	if err == nil {
		t.Error("expected error for empty id")
	}
	_, err = NewRequestFrame("req-1", "", nil)
	if err == nil {
		t.Error("expected error for empty method")
	}
}

func TestResponseFrameOK(t *testing.T) {
	resp := testutil.Must(NewResponseOK("resp-1", map[string]string{"status": "ok"}))
	if !resp.OK {
		t.Error("OK should be true")
	}

	b := testutil.Must(json.Marshal(resp))

	var decoded ResponseFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*resp, decoded) {
		t.Errorf("round-trip mismatch:\n  want: %+v\n   got: %+v", *resp, decoded)
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

	b := testutil.Must(json.Marshal(resp))

	var decoded ResponseFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*resp, decoded) {
		t.Errorf("round-trip mismatch:\n  want: %+v\n   got: %+v", *resp, decoded)
	}
}

func TestEventFrameRoundTrip(t *testing.T) {
	ev := testutil.Must(NewEventFrame("tick", map[string]int64{"ts": 1234567890}))
	if ev.Type != FrameTypeEvent {
		t.Errorf("Type = %q, want %q", ev.Type, FrameTypeEvent)
	}

	seq := uint64(42)
	ev.Seq = &seq
	ev.StateVersion = &StateVersion{Health: 1}

	b := testutil.Must(json.Marshal(ev))

	var decoded EventFrame
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(*ev, decoded) {
		t.Errorf("round-trip mismatch:\n  want: %+v\n   got: %+v", *ev, decoded)
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
		t.Errorf("got %d, want 14 error codes", len(codes))
	}
	seen := make(map[string]struct{})
	for _, c := range codes {
		if c == "" {
			t.Error("empty error code")
		}
		if _, ok := seen[c]; ok {
			t.Errorf("duplicate error code: %s", c)
		}
		seen[c] = struct{}{}
	}
}
