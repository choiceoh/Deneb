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
	RunAction(itemID, actionID string) (workfeed.ActionResult, error)
}

type rangedWorkFeedStore interface {
	ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]workfeed.Item, int, error)
}

type WorkFeedDeps struct {
	Store WorkFeedStore
	// OnAnswer, if set, records a deal-question card's answer (team → deal wiki
	// page) after the action settles. Best-effort, fire-and-forget; nil disables.
	// item is the settled card (carries Source + RefID); actionID is the tapped
	// answer (e.g. "dept:pl1").
	OnAnswer func(item workfeed.Item, actionID string)
	// DeliverAnswer, when set, durably sends a free-text answer into the asking
	// session before the card is settled. This prevents the answer from being lost
	// if a second, client-side delivery request fails after the card is acked.
	// Returns the surfaced reply text/model from the run when available.
	DeliverAnswer func(ctx context.Context, sessionKey, answer string) (text, model string, err error)
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
		"miniapp.workfeed.list":       workFeedList(deps),
		"miniapp.workfeed.ack":        workFeedAck(deps),
		"miniapp.workfeed.action.run": workFeedActionRun(deps),
		"miniapp.workfeed.answer":     workFeedAnswer(deps),
	}
}

// workFeedAnswer routes a free-text reply for a question card into the asking
// session, then settles the card. DeliverAnswer, when wired, makes that reply
// durable server-side before the card can disappear from the feed, closing the
// loss window where the client acked the card but failed to forward the answer.
// Choice answers go through action.run instead (ActionAnswer/ActionAck chips);
// this is the free-text reply path for question cards without fixed options.
func workFeedAnswer(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ItemID string `json:"itemId"`
		Answer string `json:"answer"`
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
		itemID := strings.TrimSpace(p.ItemID)
		answer := strings.TrimSpace(p.Answer)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		if answer == "" {
			return rpcerr.MissingParam("answer").Response(req.ID)
		}
		var sessionKey string
		if deps.DeliverAnswer != nil {
			// Resolve the asking session before settling the card so a delivery
			// failure leaves the card retryable instead of losing the user's reply.
			items, _, err := deps.Store.List(0, true)
			if err != nil {
				return rpcerr.WrapUnavailable("work feed unavailable", err).Response(req.ID)
			}
			found := false
			for _, item := range items {
				if item.ID != itemID {
					continue
				}
				sessionKey = item.SessionKey
				found = true
				break
			}
			if !found {
				return rpcerr.NotFound("work feed item").Response(req.ID)
			}
			text, model, err := deps.DeliverAnswer(ctx, sessionKey, answer)
			if err != nil {
				return rpcerr.WrapDependencyFailed("work feed answer delivery failed", err).Response(req.ID)
			}
			item, err := deps.Store.Ack(itemID)
			if err != nil {
				if errors.Is(err, workfeed.ErrNotFound) {
					return rpcerr.NotFound("work feed item").Response(req.ID)
				}
				return rpcerr.WrapUnavailable("work feed unavailable", err).Response(req.ID)
			}
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":             true,
				"item":           item,
				"sessionKey":     sessionKey,
				"text":           text,
				"model":          model,
				"removeFromFeed": true,
			})
		}
		// Legacy fallback: ack the card and let the client deliver the answer as a
		// second step. Kept for tests/embedders that haven't wired DeliverAnswer.
		item, err := deps.Store.Ack(itemID)
		if err != nil {
			if errors.Is(err, workfeed.ErrNotFound) {
				return rpcerr.NotFound("work feed item").Response(req.ID)
			}
			return rpcerr.WrapUnavailable("work feed unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":             true,
			"sessionKey":     item.SessionKey,
			"prompt":         answer,
			"removeFromFeed": true,
		})
	}
}

func workFeedList(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit        int   `json:"limit,omitempty"`
		IncludeAcked bool  `json:"includeAcked,omitempty"`
		SinceMs      int64 `json:"sinceMs,omitempty"`
		BeforeMs     int64 `json:"beforeMs,omitempty"`
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
		if p.SinceMs > 0 && p.BeforeMs > 0 && p.BeforeMs <= p.SinceMs {
			return rpcerr.InvalidParams(errors.New("beforeMs must be greater than sinceMs")).Response(req.ID)
		}
		var (
			items []workfeed.Item
			total int
			err   error
		)
		if p.SinceMs > 0 || p.BeforeMs > 0 {
			ranged, ok := deps.Store.(rangedWorkFeedStore)
			if !ok {
				return rpcerr.WrapUnavailable("work feed range unavailable", errors.New("store does not support range queries")).Response(req.ID)
			}
			items, total, err = ranged.ListRange(limit, p.IncludeAcked, p.SinceMs, p.BeforeMs)
		} else {
			items, total, err = deps.Store.List(limit, p.IncludeAcked)
		}
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

func workFeedActionRun(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ItemID   string `json:"itemId"`
		ActionID string `json:"actionId"`
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
		itemID := strings.TrimSpace(p.ItemID)
		actionID := strings.TrimSpace(p.ActionID)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		if actionID == "" {
			return rpcerr.MissingParam("actionId").Response(req.ID)
		}
		result, err := deps.Store.RunAction(itemID, actionID)
		if err != nil {
			switch {
			case errors.Is(err, workfeed.ErrNotFound):
				return rpcerr.NotFound("work feed item").Response(req.ID)
			case errors.Is(err, workfeed.ErrActionNotFound):
				return rpcerr.NotFound("work feed action").Response(req.ID)
			default:
				return rpcerr.WrapUnavailable("work feed unavailable", err).Response(req.ID)
			}
		}
		// Record a deal-question card's answer (team → deal wiki page) now that the
		// card has settled. Best-effort: never block or fail the action response.
		if deps.OnAnswer != nil &&
			result.Item.Source == "deal_question" &&
			strings.HasPrefix(actionID, "dept:") {
			deps.OnAnswer(result.Item, actionID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":             true,
			"item":           result.Item,
			"action":         result.Action,
			"sessionKey":     result.SessionKey,
			"prompt":         result.Prompt,
			"message":        result.Message,
			"removeFromFeed": result.RemoveFromFeed,
		})
	}
}
