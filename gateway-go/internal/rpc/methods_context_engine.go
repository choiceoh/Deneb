package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Context Engine RPC methods (Rust FFI state-machine based)
// ---------------------------------------------------------------------------

func contextAssemblyNew() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			FreshTailCount uint32 `json:"fresh_tail_count"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		handle, err := ffi.ContextAssemblyNew(p.ConversationID, p.TokenBudget, p.FreshTailCount)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"handle": handle})
		return resp
	}
}

func contextAssemblyStart() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffi.ContextAssemblyStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func contextAssemblyStep() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffi.ContextAssemblyStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func contextExpandNew() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SummaryID       string `json:"summary_id"`
			MaxDepth        uint32 `json:"max_depth"`
			IncludeMessages bool   `json:"include_messages"`
			TokenCap        uint64 `json:"token_cap"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.SummaryID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "summary_id is required"))
		}
		handle, err := ffi.ContextExpandNew(p.SummaryID, p.MaxDepth, p.IncludeMessages, p.TokenCap)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"handle": handle})
		return resp
	}
}

func contextExpandStart() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffi.ContextExpandStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func contextExpandStep() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle   uint32          `json:"handle"`
			Response json.RawMessage `json:"response"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		if len(p.Response) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "response is required"))
		}
		result, err := ffi.ContextExpandStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func contextEngineDrop() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		ffi.ContextEngineDrop(p.Handle)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"dropped": true})
		return resp
	}
}
