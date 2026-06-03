package handlerminiapp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type NativeSyncStore interface {
	Pull(afterSeq int64, limit int) (nativesync.PullResult, error)
}

type SyncDeps struct {
	Store NativeSyncStore
}

const (
	defaultSyncLimit = 100
	maxSyncLimit     = 500
)

func SyncMethods(deps SyncDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.sync.pull": syncPull(deps),
	}
}

func syncPull(deps SyncDeps) rpcutil.HandlerFunc {
	type params struct {
		Cursor int64 `json:"cursor,omitempty"`
		Limit  int   `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultSyncLimit
		}
		if limit > maxSyncLimit {
			limit = maxSyncLimit
		}
		result, err := deps.Store.Pull(p.Cursor, limit)
		if err != nil {
			return rpcerr.WrapUnavailable("native sync unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"events":       result.Events,
			"cursor":       result.Cursor,
			"latestSeq":    result.LatestSeq,
			"hasMore":      result.HasMore,
			"count":        len(result.Events),
			"serverTimeMs": time.Now().UnixMilli(),
		})
	}
}
