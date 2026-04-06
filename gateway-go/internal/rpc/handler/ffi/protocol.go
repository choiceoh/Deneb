package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
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
		p, errResp := rpcutil.DecodeParams[struct {
			Frame string `json:"frame"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Frame == "" {
			return rpcerr.MissingParam("frame").Response(req.ID)
		}
		err := ffipkg.ValidateFrame(p.Frame)
		backend := "go"
		if err != nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"valid": false, "error": err.Error(), "backend": backend,
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"valid": true, "backend": backend,
		})
	}
}

func protocolValidateParams() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Method string `json:"method"`
			Params string `json:"params"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Method == "" {
			return rpcerr.MissingParam("method").Response(req.ID)
		}
		if p.Params == "" {
			return rpcerr.MissingParam("params").Response(req.ID)
		}
		valid, errorsJSON, err := ffipkg.ValidateParams(p.Method, p.Params)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		backend := "go"
		result := map[string]any{"valid": valid, "backend": backend}
		if errorsJSON != nil {
			result["errors"] = json.RawMessage(errorsJSON)
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}
