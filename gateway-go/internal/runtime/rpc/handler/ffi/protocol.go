package ffi

import (
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ai/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// ProtocolMethods returns handlers for protocol validation RPC methods.
func ProtocolMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"protocol.validate":        protocolValidate(),
		"protocol.validate_params": protocolValidateParams(),
	}
}

func protocolValidate() rpcutil.HandlerFunc {
	type params struct {
		Frame string `json:"frame"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Frame == "" {
			return nil, rpcerr.MissingParam("frame")
		}
		err := ffipkg.ValidateFrame(p.Frame)
		backend := "go"
		if err != nil {
			// Validation failure is a success response with valid=false.
			return map[string]any{
				"valid": false, "error": err.Error(), "backend": backend,
			}, nil
		}
		return map[string]any{"valid": true, "backend": backend}, nil
	})
}

func protocolValidateParams() rpcutil.HandlerFunc {
	type params struct {
		Method string `json:"method"`
		Params string `json:"params"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Method == "" {
			return nil, rpcerr.MissingParam("method")
		}
		if p.Params == "" {
			return nil, rpcerr.MissingParam("params")
		}
		valid, errorsJSON, err := ffipkg.ValidateParams(p.Method, p.Params)
		if err != nil {
			return nil, rpcerr.WrapDependencyFailed("param validation failed", err)
		}
		backend := "go"
		result := map[string]any{"valid": valid, "backend": backend}
		if errorsJSON != nil {
			result["errors"] = json.RawMessage(errorsJSON)
		}
		return result, nil
	})
}
