package ffi

import (
	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ai/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
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
	type params struct {
		Key string `json:"key"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		err := ffipkg.ValidateSessionKey(p.Key)
		return map[string]any{"valid": err == nil}, nil
	})
}

func securitySanitizeHTML() rpcutil.HandlerFunc {
	type params struct {
		Input string `json:"input"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		return map[string]any{"output": ffipkg.SanitizeHTML(p.Input)}, nil
	})
}

func securityIsSafeURL() rpcutil.HandlerFunc {
	type params struct {
		URL string `json:"url"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		return map[string]any{"safe": ffipkg.IsSafeURL(p.URL)}, nil
	})
}

func securityValidateErrorCode() rpcutil.HandlerFunc {
	type params struct {
		Code string `json:"code"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		return map[string]any{"valid": ffipkg.ValidateErrorCode(p.Code)}, nil
	})
}
