package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ContextEngineMethods returns handlers for context engine RPC methods.
func ContextEngineMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"context.assembly.new":   contextAssemblyNew(),
		"context.assembly.start": contextAssemblyStart(),
		"context.assembly.step":  contextAssemblyStep(),
		"context.expand.new":     contextExpandNew(),
		"context.expand.start":   contextExpandStart(),
		"context.expand.step":    contextExpandStep(),
		"context.engine.drop":    contextEngineDrop(),
	}
}

func contextAssemblyNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			FreshTailCount uint32 `json:"fresh_tail_count"`
		}](req)
		if errResp != nil {
			return errResp
		}
		handle, err := ffipkg.ContextAssemblyNew(p.ConversationID, p.TokenBudget, p.FreshTailCount)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"handle": handle})
	}
}

func contextAssemblyStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Handle uint32 `json:"handle"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Handle == 0 {
			return rpcerr.MissingParam("handle").Response(req.ID)
		}
		result, err := ffipkg.ContextAssemblyStart(p.Handle)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func contextAssemblyStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Handle == 0 {
			return rpcerr.MissingParam("handle").Response(req.ID)
		}
		if len(p.Response) == 0 {
			return rpcerr.MissingParam("response").Response(req.ID)
		}
		result, err := ffipkg.ContextAssemblyStep(p.Handle, p.Response)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func contextExpandNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SummaryID       string `json:"summary_id"`
			MaxDepth        uint32 `json:"max_depth"`
			IncludeMessages bool   `json:"include_messages"`
			TokenCap        uint64 `json:"token_cap"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.SummaryID == "" {
			return rpcerr.MissingParam("summary_id").Response(req.ID)
		}
		handle, err := ffipkg.ContextExpandNew(p.SummaryID, p.MaxDepth, p.IncludeMessages, p.TokenCap)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"handle": handle})
	}
}

func contextExpandStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Handle uint32 `json:"handle"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Handle == 0 {
			return rpcerr.MissingParam("handle").Response(req.ID)
		}
		result, err := ffipkg.ContextExpandStart(p.Handle)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func contextExpandStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Handle == 0 {
			return rpcerr.MissingParam("handle").Response(req.ID)
		}
		if len(p.Response) == 0 {
			return rpcerr.MissingParam("response").Response(req.ID)
		}
		result, err := ffipkg.ContextExpandStep(p.Handle, p.Response)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func contextEngineDrop() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Handle uint32 `json:"handle"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Handle == 0 {
			return rpcerr.MissingParam("handle").Response(req.ID)
		}
		ffipkg.ContextEngineDrop(p.Handle)
		return rpcutil.RespondOK(req.ID, map[string]any{"dropped": true})
	}
}
