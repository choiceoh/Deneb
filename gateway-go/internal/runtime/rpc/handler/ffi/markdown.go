package ffi

import (
	"encoding/json"

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
	type params struct {
		Markdown string `json:"markdown"`
		Options  string `json:"options"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		ir, err := ffipkg.MarkdownToIR(p.Markdown, p.Options)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		// ir is already JSON; wrap in the response directly.
		return json.RawMessage(ir), nil
	})
}

func markdownDetectFences() rpcutil.HandlerFunc {
	type params struct {
		Text string `json:"text"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		fences, err := ffipkg.MarkdownDetectFences(p.Text)
		if err != nil {
			return nil, rpcerr.Wrap(protocol.ErrInvalidRequest, err)
		}
		return map[string]any{"fences": fences}, nil
	})
}
