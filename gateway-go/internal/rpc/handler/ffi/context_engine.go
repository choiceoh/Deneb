package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
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
		var p struct {
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			FreshTailCount uint32 `json:"fresh_tail_count"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		handle, err := ffipkg.ContextAssemblyNew(p.ConversationID, p.TokenBudget, p.FreshTailCount)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"handle": handle})
	}
}

func contextAssemblyStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffipkg.ContextAssemblyStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextAssemblyStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffipkg.ContextAssemblyStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextExpandNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SummaryID       string `json:"summary_id"`
			MaxDepth        uint32 `json:"max_depth"`
			IncludeMessages bool   `json:"include_messages"`
			TokenCap        uint64 `json:"token_cap"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.SummaryID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "summary_id is required"))
		}
		handle, err := ffipkg.ContextExpandNew(p.SummaryID, p.MaxDepth, p.IncludeMessages, p.TokenCap)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"handle": handle})
	}
}

func contextExpandStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffipkg.ContextExpandStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextExpandStep() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffipkg.ContextExpandStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func contextEngineDrop() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		ffipkg.ContextEngineDrop(p.Handle)
		return protocol.MustResponseOK(req.ID, map[string]any{"dropped": true})
	}
}
