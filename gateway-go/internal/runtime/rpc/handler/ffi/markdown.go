package ffi

import (
	"context"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ai/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
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
		p, errResp := rpcutil.DecodeParams[struct {
			Markdown string `json:"markdown"`
			Options  string `json:"options"`
		}](req)
		if errResp != nil {
			return errResp
		}
		ir, err := ffipkg.MarkdownToIR(p.Markdown, p.Options)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		// ir is already JSON; wrap in the response directly.
		return rpcutil.RespondOK(req.ID, ir)
	}
}

func markdownDetectFences() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Text string `json:"text"`
		}](req)
		if errResp != nil {
			return errResp
		}
		fences, err := ffipkg.MarkdownDetectFences(p.Text)
		if err != nil {
			return rpcerr.Wrap(protocol.ErrInvalidRequest, err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"fences": fences,
		})
	}
}
