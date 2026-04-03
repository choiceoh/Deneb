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
		p, errResp := rpcutil.DecodeParams[struct {
			Key string `json:"key"`
		}](req)
		if errResp != nil {
			return errResp
		}
		err := ffipkg.ValidateSessionKey(p.Key)
		return rpcutil.RespondOK(req.ID, map[string]any{
			"valid": err == nil,
		})
	}
}

func securitySanitizeHTML() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Input string `json:"input"`
		}](req)
		if errResp != nil {
			return errResp
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"output": ffipkg.SanitizeHTML(p.Input),
		})
	}
}

func securityIsSafeURL() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			URL string `json:"url"`
		}](req)
		if errResp != nil {
			return errResp
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"safe": ffipkg.IsSafeURL(p.URL),
		})
	}
}

func securityValidateErrorCode() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Code string `json:"code"`
		}](req)
		if errResp != nil {
			return errResp
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"valid": ffipkg.ValidateErrorCode(p.Code),
		})
	}
}
