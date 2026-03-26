package rpcerr

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestNewBasic(t *testing.T) {
	e := New(protocol.ErrNotFound, "session not found")
	if e.Code != protocol.ErrNotFound {
		t.Errorf("code = %q, want %q", e.Code, protocol.ErrNotFound)
	}
	if e.Message != "session not found" {
		t.Errorf("message = %q", e.Message)
	}
	if len(e.Context) != 0 {
		t.Errorf("context should be empty, got %v", e.Context)
	}
}

func TestWithChaining(t *testing.T) {
	e := New(protocol.ErrNotFound, "session not found").
		WithSession("abc-123").
		WithModel("claude-3").
		WithMethod("sessions.get").
		With("extra", 42)

	if e.Context["sessionKey"] != "abc-123" {
		t.Errorf("sessionKey = %v", e.Context["sessionKey"])
	}
	if e.Context["model"] != "claude-3" {
		t.Errorf("model = %v", e.Context["model"])
	}
	if e.Context["method"] != "sessions.get" {
		t.Errorf("method = %v", e.Context["method"])
	}
	if e.Context["extra"] != 42 {
		t.Errorf("extra = %v", e.Context["extra"])
	}
}

func TestToShapePreservesDetails(t *testing.T) {
	e := New(protocol.ErrConflict, "running").WithSession("key-1")
	shape := e.ToShape()

	if shape.Code != protocol.ErrConflict {
		t.Errorf("code = %q", shape.Code)
	}
	if shape.Message != "running" {
		t.Errorf("message = %q", shape.Message)
	}
	if shape.Details == nil {
		t.Fatal("details should not be nil")
	}
	var details map[string]any
	if err := json.Unmarshal(shape.Details, &details); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if details["sessionKey"] != "key-1" {
		t.Errorf("details.sessionKey = %v", details["sessionKey"])
	}
}

func TestToShapeNoDetailsWhenEmpty(t *testing.T) {
	e := New(protocol.ErrMissingParam, "key is required")
	shape := e.ToShape()
	if shape.Details != nil {
		t.Errorf("details should be nil, got %s", shape.Details)
	}
}

func TestResponse(t *testing.T) {
	e := MissingParam("key")
	resp := e.Response("req-1")
	if resp.ID != "req-1" {
		t.Errorf("id = %q", resp.ID)
	}
	if resp.OK {
		t.Error("response should not be OK")
	}
	if resp.Error == nil {
		t.Fatal("error should not be nil")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("error code = %q", resp.Error.Code)
	}
}

func TestWrap(t *testing.T) {
	e := Wrap(protocol.ErrDependencyFailed, errors.New("db connection lost"))
	if e.Message != "db connection lost" {
		t.Errorf("message = %q", e.Message)
	}
}

func TestErrorInterface(t *testing.T) {
	e := New(protocol.ErrNotFound, "gone").WithSession("s1")
	msg := e.Error()
	if msg == "" {
		t.Error("Error() should not be empty")
	}
}

func TestConvenienceConstructors(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		code string
	}{
		{"MissingParam", MissingParam("key"), protocol.ErrMissingParam},
		{"InvalidParams", InvalidParams(errors.New("bad")), protocol.ErrInvalidRequest},
		{"NotFound", NotFound("session"), protocol.ErrNotFound},
		{"Unavailable", Unavailable("down"), protocol.ErrUnavailable},
		{"Conflict", Conflict("running"), protocol.ErrConflict},
		{"FeatureDisabled", FeatureDisabled("vega"), protocol.ErrFeatureDisabled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("code = %q, want %q", tc.err.Code, tc.code)
			}
		})
	}
}

func TestLogAttrs(t *testing.T) {
	e := New(protocol.ErrNotFound, "missing").WithSession("s1")
	attrs := e.LogAttrs()
	// Should have: code, ErrNotFound, message, "missing", sessionKey, "s1"
	if len(attrs) < 6 {
		t.Errorf("expected at least 6 attrs, got %d: %v", len(attrs), attrs)
	}
}
