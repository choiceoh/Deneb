package rpcerr

import (
	"encoding/json"
	"errors"
	"fmt"
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
	cause := errors.New("db connection lost")
	e := Wrap(protocol.ErrDependencyFailed, cause)
	if e.Message != "db connection lost" {
		t.Errorf("message = %q", e.Message)
	}
	if e.Cause != cause {
		t.Error("Cause should be the original error")
	}
}

func TestUnwrap(t *testing.T) {
	sentinel := errors.New("root cause")
	e := Wrap(protocol.ErrDependencyFailed, sentinel)

	// errors.Unwrap should return the cause.
	if errors.Unwrap(e) != sentinel {
		t.Error("Unwrap should return the sentinel")
	}

	// errors.Is should traverse the chain.
	if !errors.Is(e, sentinel) {
		t.Error("errors.Is should find the sentinel through the chain")
	}

	// errors.As should find rpcerr.Error in a wrapped chain.
	wrapped := fmt.Errorf("outer: %w", e)
	var rpcErr *Error
	if !errors.As(wrapped, &rpcErr) {
		t.Fatal("errors.As should find *rpcerr.Error through wrapping")
	}
	if rpcErr.Code != protocol.ErrDependencyFailed {
		t.Errorf("code = %q, want %q", rpcErr.Code, protocol.ErrDependencyFailed)
	}
}

func TestWrapConvenienceConstructors(t *testing.T) {
	cause := errors.New("disk full")
	tests := []struct {
		name string
		err  *Error
		code string
	}{
		{"WrapUnavailable", WrapUnavailable("write failed", cause), protocol.ErrUnavailable},
		{"WrapInvalidRequest", WrapInvalidRequest("bad input", cause), protocol.ErrInvalidRequest},
		{"WrapDependencyFailed", WrapDependencyFailed("db error", cause), protocol.ErrDependencyFailed},
		{"WrapValidationFailed", WrapValidationFailed("schema error", cause), protocol.ErrValidationFailed},
		{"WrapConflict", WrapConflict("already running", cause), protocol.ErrConflict},
		{"WrapNotFound", WrapNotFound("page missing", cause), protocol.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("code = %q, want %q", tc.err.Code, tc.code)
			}
			if !errors.Is(tc.err, cause) {
				t.Error("errors.Is should find the cause through the chain")
			}
			if tc.err.Cause != cause {
				t.Error("Cause field should be set")
			}
		})
	}
}

func TestNilCauseUnwrap(t *testing.T) {
	e := New(protocol.ErrNotFound, "gone")
	if errors.Unwrap(e) != nil {
		t.Error("Unwrap should return nil when no cause is set")
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
		{"FeatureDisabled", FeatureDisabled("wiki"), protocol.ErrFeatureDisabled},
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
