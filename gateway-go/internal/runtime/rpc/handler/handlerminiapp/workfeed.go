package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type WorkFeedStore interface {
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
	Ack(id string) (workfeed.Item, error)
}

type WorkFeedDeps struct {
	Store WorkFeedStore
}

const (
	defaultWorkFeedLimit = 20
	maxWorkFeedLimit     = 100
)

func WorkFeedMethods(deps WorkFeedDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.workfeed.list": workFeedList(deps),
		"miniapp.workfeed.ack":  workFeedAck(deps),
	}
}

func workFeedList(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit        int  `json:"limit,omitempty"`
		IncludeAcked bool `json:"includeAcked,omitempty"`
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
			limit = defaultWorkFeedLimit
		}
		if limit > maxWorkFeedLimit {
			limit = maxWorkFeedLimit
		}
		items, total, err := deps.Store.List(limit, p.IncludeAcked)
		if err != nil {
			return rpcerr.WrapUnavailable("work feed unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"items": items,
			"count": len(items),
			"total": total,
		})
	}
}

func workFeedAck(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
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
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		item, err := deps.Store.Ack(id)
		if err != nil {
			if errors.Is(err, workfeed.ErrNotFound) {
				return rpcerr.NotFound("work feed item").Response(req.ID)
			}
			return rpcerr.WrapUnavailable("work feed unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":   true,
			"item": item,
		})
	}
}
