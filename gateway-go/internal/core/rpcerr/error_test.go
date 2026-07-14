package rpcerr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestNewCreatesErrorWithoutDetails(t *testing.T) {
	e := New(protocol.ErrNotFound, "session not found")
	if e.Code != protocol.ErrNotFound {
		t.Errorf("code = %q, want %q", e.Code, protocol.ErrNotFound)
	}
	if e.Message != "session not found" {
		t.Errorf("message = %q", e.Message)
	}
	if details := e.ToShape().Details; details != nil {
		t.Errorf("new error details = %s, want nil", details)
	}
}

func TestWithSessionAndWithMethodChainIntoDetails(t *testing.T) {
	e := New(protocol.ErrNotFound, "session not found").
		WithSession("abc-123").
		WithMethod("sessions.get")

	var details map[string]string
	if err := json.Unmarshal(e.ToShape().Details, &details); err != nil {
		t.Fatalf("decode details: %v", err)
	}
	if got := details["sessionKey"]; got != "abc-123" {
		t.Errorf("sessionKey = %q", got)
	}
	if got := details["method"]; got != "sessions.get" {
		t.Errorf("method = %q", got)
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

func TestResponseReturnsErrorEnvelopeWithRequestID(t *testing.T) {
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

func TestWrapPreservesCauseAndSetsMessageFromError(t *testing.T) {
	cause := errors.New("db connection lost")
	e := Wrap(protocol.ErrDependencyFailed, cause)
	if e.Message != "db connection lost" {
		t.Errorf("message = %q", e.Message)
	}
	if !errors.Is(e.Cause, cause) {
		t.Error("Cause should be the original error")
	}
}

func TestUnwrapPreservesErrorChainForIsAndAs(t *testing.T) {
	sentinel := errors.New("root cause")
	e := Wrap(protocol.ErrDependencyFailed, sentinel)

	// errors.Unwrap should return the cause.
	if !errors.Is(errors.Unwrap(e), sentinel) {
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

func TestWrapConvenienceConstructorsPreserveCodeAndCause(t *testing.T) {
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("code = %q, want %q", tc.err.Code, tc.code)
			}
			if !errors.Is(tc.err, cause) {
				t.Error("errors.Is should find the cause through the chain")
			}
			if !errors.Is(tc.err.Cause, cause) {
				t.Error("Cause field should be set")
			}
		})
	}
}

func TestLogAttrsReturnsCodeMessageAndSessionKeyForLogging(t *testing.T) {
	e := New(protocol.ErrNotFound, "missing").WithSession("s1")
	attrs := e.LogAttrs()
	if len(attrs) != 3 {
		t.Fatalf("got %d attrs, want 3: %v", len(attrs), attrs)
	}

	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	logger.LogAttrs(context.Background(), slog.LevelError, "rpc error", attrs...)

	var entry map[string]any
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v", err)
	}
	for key, want := range map[string]string{
		"code":       protocol.ErrNotFound,
		"message":    "missing",
		"sessionKey": "s1",
	} {
		if got := entry[key]; got != want {
			t.Errorf("log %s = %v, want %q", key, got, want)
		}
	}
}
