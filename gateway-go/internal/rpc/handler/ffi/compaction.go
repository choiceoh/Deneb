package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CompactionMethods returns handlers for compaction RPC methods.
func CompactionMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"compaction.evaluate":    compactionEvaluate(),
		"compaction.sweep.new":   compactionSweepNew(),
		"compaction.sweep.start": compactionSweepStart(),
		"compaction.sweep.step":  compactionSweepStep(),
		"compaction.sweep.drop":  compactionSweepDrop(),
	}
}

func compactionEvaluate() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Config       string `json:"config"`
			StoredTokens uint64 `json:"stored_tokens"`
			LiveTokens   uint64 `json:"live_tokens"`
			TokenBudget  uint64 `json:"token_budget"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		result, err := ffipkg.CompactionEvaluate(p.Config, p.StoredTokens, p.LiveTokens, p.TokenBudget)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Config         string `json:"config"`
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			Force          bool   `json:"force"`
			HardTrigger    bool   `json:"hard_trigger"`
			NowMs          int64  `json:"now_ms"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		handle, err := ffipkg.CompactionSweepNew(p.Config, p.ConversationID, p.TokenBudget, p.Force, p.HardTrigger, p.NowMs)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, map[string]any{"handle": handle})
	}
}

func compactionSweepStart() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		result, err := ffipkg.CompactionSweepStart(p.Handle)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepStep() rpcutil.HandlerFunc {
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
		result, err := ffipkg.CompactionSweepStep(p.Handle, p.Response)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		return protocol.MustResponseOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepDrop() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Handle uint32 `json:"handle"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil || p.Handle == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "handle is required"))
		}
		ffipkg.CompactionSweepDrop(p.Handle)
		return protocol.MustResponseOK(req.ID, map[string]any{"dropped": true})
	}
}
