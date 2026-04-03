package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
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
		p, errResp := rpcutil.DecodeParams[struct {
			Config       string `json:"config"`
			StoredTokens uint64 `json:"stored_tokens"`
			LiveTokens   uint64 `json:"live_tokens"`
			TokenBudget  uint64 `json:"token_budget"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		result, err := ffipkg.CompactionEvaluate(p.Config, p.StoredTokens, p.LiveTokens, p.TokenBudget)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepNew() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Config         string `json:"config"`
			ConversationID uint64 `json:"conversation_id"`
			TokenBudget    uint64 `json:"token_budget"`
			Force          bool   `json:"force"`
			HardTrigger    bool   `json:"hard_trigger"`
			NowMs          int64  `json:"now_ms"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Config == "" {
			p.Config = `{"contextThreshold":0.75}`
		}
		handle, err := ffipkg.CompactionSweepNew(p.Config, p.ConversationID, p.TokenBudget, p.Force, p.HardTrigger, p.NowMs)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"handle": handle})
	}
}

func compactionSweepStart() rpcutil.HandlerFunc {
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
		result, err := ffipkg.CompactionSweepStart(p.Handle)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepStep() rpcutil.HandlerFunc {
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
		result, err := ffipkg.CompactionSweepStep(p.Handle, p.Response)
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func compactionSweepDrop() rpcutil.HandlerFunc {
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
		ffipkg.CompactionSweepDrop(p.Handle)
		return rpcutil.RespondOK(req.ID, map[string]any{"dropped": true})
	}
}
