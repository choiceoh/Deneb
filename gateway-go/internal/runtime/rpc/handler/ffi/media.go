package ffi

import (
	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ai/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// MediaMethods returns handlers for media detection RPC methods.
func MediaMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"media.detect_mime": mediaDetectMIME(),
	}
}

func mediaDetectMIME() rpcutil.HandlerFunc {
	type params struct {
		Data []byte `json:"data"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		return map[string]any{"mime": ffipkg.DetectMIME(p.Data)}, nil
	})
}
