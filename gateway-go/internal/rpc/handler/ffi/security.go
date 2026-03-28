package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SecurityMethods returns handlers for security-related RPC methods.
func SecurityMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"security.validate_session_key": securityValidateSessionKey(),
		"security.sanitize_html":        securitySanitizeHTML(),
		"security.is_safe_url":          securityIsSafeURL(),
		"security.validate_error_code":  securityValidateErrorCode(),
	}
}

func securityValidateSessionKey() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Key string `json:"key"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		err := ffipkg.ValidateSessionKey(p.Key)
		return protocol.MustResponseOK(req.ID, map[string]any{
			"valid": err == nil,
		})
	}
}

func securitySanitizeHTML() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Input string `json:"input"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"output": ffipkg.SanitizeHTML(p.Input),
		})
	}
}

func securityIsSafeURL() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			URL string `json:"url"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"safe": ffipkg.IsSafeURL(p.URL),
		})
	}
}

func securityValidateErrorCode() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Code string `json:"code"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"valid": ffipkg.ValidateErrorCode(p.Code),
		})
	}
}
