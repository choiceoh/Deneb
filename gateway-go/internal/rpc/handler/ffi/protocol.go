package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProtocolMethods returns handlers for protocol validation RPC methods.
func ProtocolMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"protocol.validate":        protocolValidate(),
		"protocol.validate_params": protocolValidateParams(),
	}
}

func protocolValidate() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Frame string `json:"frame"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Frame == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "frame is required"))
		}
		err := ffipkg.ValidateFrame(p.Frame)
		backend := "go-fallback"
		if ffipkg.Available {
			backend = "rust"
		}
		if err != nil {
			return protocol.MustResponseOK(req.ID, map[string]any{
				"valid": false, "error": err.Error(), "backend": backend,
			})
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"valid": true, "backend": backend,
		})
	}
}

func protocolValidateParams() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Method string `json:"method"`
			Params string `json:"params"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Method == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "method is required"))
		}
		if p.Params == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params is required"))
		}
		valid, errorsJSON, err := ffipkg.ValidateParams(p.Method, p.Params)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		backend := "go-fallback"
		if ffipkg.Available {
			backend = "rust"
		}
		result := map[string]any{"valid": valid, "backend": backend}
		if errorsJSON != nil {
			result["errors"] = json.RawMessage(errorsJSON)
		}
		return protocol.MustResponseOK(req.ID, result)
	}
}
