package syncapi

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// NativeSyncStore defines the native-sync persistence operations used by RPC handlers.
type NativeSyncStore interface {
	Pull(afterSeq int64, limit int) (nativesync.PullResult, error)
}

// SyncDeps holds dependencies required by native-sync RPC handlers.
type SyncDeps struct {
	Store NativeSyncStore
}

const (
	defaultSyncLimit = 100
	maxSyncLimit     = 500
)

// SyncMethods registers native-sync RPC handlers.
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
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
			"truncated":    result.Truncated,
			"count":        len(result.Events),
			"serverTimeMs": time.Now().UnixMilli(),
		})
	})
}
