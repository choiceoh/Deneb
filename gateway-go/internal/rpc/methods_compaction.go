package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Compaction RPC methods (Rust context compression engine)
// ---------------------------------------------------------------------------

func compactionEvaluate() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Config       string `json:"config"`
			StoredTokens uint64 `json:"stored_tokens"`
			LiveTokens   uint64 `json:"live_tokens"`
			TokenBudget  uint64 `json:"token_budget"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		result, err := ffi.CompactionEvaluate(p.Config, p.StoredTokens, p.LiveTokens, p.TokenBudget)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func compactionSweepNew() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Config         string `json:"config"`
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			Force          bool   `json:"force"`
			HardTrigger    bool   `json:"hard_trigger"`
			NowMs          int64  `json:"now_ms"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		handle, err := ffi.CompactionSweepNew(p.Config, p.ConversationID, p.TokenBudget, p.Force, p.HardTrigger, p.NowMs)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"handle": handle})
		return resp
	}
}

func compactionSweepStart() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffi.CompactionSweepStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func compactionSweepStep() HandlerFunc {
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
		result, err := ffi.CompactionSweepStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp, _ := protocol.NewResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func compactionSweepDrop() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := unmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		ffi.CompactionSweepDrop(p.Handle)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"dropped": true})
		return resp
	}
}
