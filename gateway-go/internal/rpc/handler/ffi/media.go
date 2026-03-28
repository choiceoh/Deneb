package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MediaMethods returns handlers for media detection RPC methods.
func MediaMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"media.detect_mime": mediaDetectMIME(),
	}
}

func mediaDetectMIME() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Data []byte `json:"data"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"mime": ffipkg.DetectMIME(p.Data),
		})
	}
}
