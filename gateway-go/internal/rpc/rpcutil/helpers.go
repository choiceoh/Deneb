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

	"github.com/choiceoh/deneb/gateway-go/internal/middleware"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// HandlerFunc is an alias for middleware.HandlerFunc so that the dispatcher,
// domain handler packages, and the middleware chain all share one type.
// This eliminates type conversions on the hot dispatch path.
type HandlerFunc = middleware.HandlerFunc

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

// DecodeParams unmarshals request params into T. On success it returns the
// decoded value and nil; on failure it returns the zero value and a ready-made
// INVALID_REQUEST error response.
//
// Usage (inline struct type is valid Go generic syntax):
//
//	p, errResp := rpcutil.DecodeParams[struct {
//	    Key string `json:"key"`
//	}](req)
//	if errResp != nil {
//	    return errResp
//	}
func DecodeParams[T any](req *protocol.RequestFrame) (T, *protocol.ResponseFrame) {
	var v T
	if err := UnmarshalParams(req.Params, &v); err != nil {
		return v, rpcerr.InvalidParams(err).Response(req.ID)
	}
	return v, nil
}

// RespondOK is a shorthand for protocol.MustResponseOK.
// It builds a success ResponseFrame, panicking only on JSON marshal failure
// (which should never happen for well-formed result values).
func RespondOK(reqID string, result any) *protocol.ResponseFrame {
	return protocol.MustResponseOK(reqID, result)
}

// BindHandler returns a HandlerFunc that decodes params into P, calls fn, and
// builds the response. It eliminates the closure-wrapping boilerplate that
// otherwise repeats in every handler:
//
//	return rpcutil.BindHandler[params](func(p params) (any, error) {
//	    return deps.Manager.DoSomething(p.Name), nil
//	})
func BindHandler[P any](fn func(P) (any, error)) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return Bind[P](req, fn)
	}
}

// Bind unmarshals request params into P, calls fn with the decoded value, and
// returns a ready-made ResponseFrame. fn returns the success payload and an
// optional error; *rpcerr.Error values are converted to error responses
// directly, any other error is wrapped as INVALID_REQUEST.
//
// Typical usage with a local named type:
//
//	type params struct { Name string `json:"name"` }
//	return rpcutil.Bind[params](req, func(p params) (any, error) {
//	    if p.Name == "" { return nil, rpcerr.MissingParam("name") }
//	    return deps.Manager.DoSomething(p.Name), nil
//	})
func Bind[P any](req *protocol.RequestFrame, fn func(P) (any, error)) *protocol.ResponseFrame {
	p, errResp := DecodeParams[P](req)
	if errResp != nil {
		return errResp
	}
	result, err := fn(p)
	if err != nil {
		var rpcErr *rpcerr.Error
		if errors.As(err, &rpcErr) {
			return rpcErr.Response(req.ID)
		}
		return rpcerr.InvalidParams(err).Response(req.ID)
	}
	return RespondOK(req.ID, result)
}
