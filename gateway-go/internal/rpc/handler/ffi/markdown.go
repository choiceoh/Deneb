package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MarkdownMethods returns handlers for markdown processing RPC methods.
func MarkdownMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"markdown.to_ir":         markdownToIR(),
		"markdown.detect_fences": markdownDetectFences(),
	}
}

func markdownToIR() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Markdown string `json:"markdown"`
			Options  string `json:"options"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		ir, err := ffipkg.MarkdownToIR(p.Markdown, p.Options)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		// ir is already JSON; wrap in the response directly.
		return protocol.MustResponseOK(req.ID, json.RawMessage(ir))
	}
}

func markdownDetectFences() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Text string `json:"text"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		fences, err := ffipkg.MarkdownDetectFences(p.Text)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"fences": fences,
		})
	}
}
