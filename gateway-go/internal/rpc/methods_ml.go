package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// ML RPC methods (Rust FFI — Phase 0 scaffolding)
// ---------------------------------------------------------------------------

func mlEmbed() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		result, err := ffi.MLEmbed(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func mlRerank() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		result, err := ffi.MLRerank(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}
