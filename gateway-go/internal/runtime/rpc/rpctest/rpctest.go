// Package rpctest provides shared test helpers for RPC handler tests.
//
// These helpers were previously duplicated across handler test files
// (node_test.go, session_test.go, agent_test.go, gateway_test.go,
// process_test.go, ffi_test.go). This package centralizes them.
package rpctest

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Call invokes a named handler from the given map. If params is nil the
// request is sent with nil Params; otherwise params is JSON-marshaled.
// Returns nil when the method is not found in the map.
func Call(m map[string]rpcutil.HandlerFunc, method string, params any) *protocol.ResponseFrame {
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	req := &protocol.RequestFrame{ID: "t1", Method: method, Params: raw}
	h, ok := m[method]
	if !ok {
		return nil
	}
	return h(context.Background(), req)
}

// MustOK asserts that resp is non-nil and carries no error.
func MustOK(t *testing.T, resp *protocol.ResponseFrame) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

// MustErr asserts that resp is non-nil and carries an error.
func MustErr(t *testing.T, resp *protocol.ResponseFrame) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.Error == nil {
		t.Fatalf("expected error, got success: %s", resp.Payload)
	}
}

// Result unmarshals resp.Payload into T and fails the test on error.
func Result[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return out
}

// NewLogger returns a quiet logger suitable for tests (error-level only).
func NewLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}
