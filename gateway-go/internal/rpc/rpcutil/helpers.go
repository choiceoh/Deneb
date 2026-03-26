// Package rpcutil provides shared helpers for RPC handler packages.
//
// This package is imported by domain handler packages (handler/session,
// handler/chat, etc.) without creating circular dependencies with the
// parent rpc package.
package rpcutil

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandlerFunc is the canonical RPC handler signature. Domain handler packages
// return maps of method name to HandlerFunc. The rpc.Dispatcher accepts this
// same signature, so no conversion is needed.
type HandlerFunc func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame

// MaxKeyInErrorMsg is the maximum key length included in error messages.
// Prevents log inflation from pathologically large keys.
const MaxKeyInErrorMsg = 128

// UnmarshalParams safely unmarshals request params, handling nil/empty params.
func UnmarshalParams(params json.RawMessage, v any) error {
	if len(params) == 0 {
		return errors.New("missing params")
	}
	return json.Unmarshal(params, v)
}

// TruncateForError truncates a string for safe inclusion in error messages.
func TruncateForError(s string) string {
	if len(s) <= MaxKeyInErrorMsg {
		return s
	}
	return s[:MaxKeyInErrorMsg] + "..."
}

// RequireKey trims and validates a session key, returning a MISSING_PARAM
// error response if the key is empty.
func RequireKey(reqID, key string) (string, *protocol.ResponseFrame) {
	k := strings.TrimSpace(key)
	if k == "" {
		return "", rpcerr.MissingParam("key").Response(reqID)
	}
	return k, nil
}

// ErrMissingKey returns a standard MISSING_PARAM response for a missing key.
func ErrMissingKey(reqID string) *protocol.ResponseFrame {
	return rpcerr.MissingParam("key").Response(reqID)
}

// ParamError returns an INVALID_REQUEST error response for bad params.
func ParamError(reqID string, err error) *protocol.ResponseFrame {
	return rpcerr.InvalidParams(err).Response(reqID)
}
