package handlerminiapp

import (
	"context"
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// WorkFeedStore defines the work-feed persistence operations used by RPC handlers.
type WorkFeedStore interface {
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
	Ack(id string) (workfeed.Item, error)
	MarkRead(id string) (workfeed.Item, error)
	RunAction(itemID, actionID, comment string) (workfeed.ActionResult, error)
}

type rangedWorkFeedStore interface {
	ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]workfeed.Item, int, error)
}

// WorkFeedDeps holds dependencies required by work-feed RPC handlers.
type WorkFeedDeps struct {
	Store WorkFeedStore
	// OnAnswer, if set, records a deal-question card's answer (team → deal wiki
	// page) after the action settles. Best-effort, fire-and-forget; nil disables.
	// item is the settled card (carries Source + RefID); actionID is the tapped
	// answer (e.g. "dept:pl1").
	OnAnswer func(item workfeed.Item, actionID string)
	// OnMetaProposal, if set, applies a meta-proposal card's adopt/reject
	// decision after the action settles (RSI P2 feed-card adoption). Same
	// best-effort contract as OnAnswer.
	OnMetaProposal func(item workfeed.Item, actionID string)
	// OnEvolveVerdict records an operator label for a low-confidence accepted
	// skill evolve, optionally rolling it back. Best-effort after settlement.
	OnEvolveVerdict func(item workfeed.Item, actionID string)
	// OnLadder, if set, applies a graduation-ladder card's veto (재잠금) after
	// the action settles. Same best-effort contract as OnAnswer.
	OnLadder func(item workfeed.Item, actionID string)
	// OnDeadlineDone, if set, stamps a wiki page's due_done when the operator
	// long-presses a morning-card deadline row (actionID "deadline_done:<path>").
	// Non-settling (ActionMark): the card stays for its other deadlines.
	OnDeadlineDone func(item workfeed.Item, actionID string)
	// OnIdentityReviewed, if set, records the operator's answer to a wiki
	// person-identity question (동명이인·중복 인물) so the scan stops asking.
	OnIdentityReviewed func(item workfeed.Item, actionID string)
}

const (
	defaultWorkFeedLimit = 20
	maxWorkFeedLimit     = 100
)

// WorkFeedMethods registers work-feed RPC handlers.
func WorkFeedMethods(deps WorkFeedDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.workfeed.list":       workFeedList(deps),
		"miniapp.workfeed.ack":        workFeedAck(deps),
		"miniapp.workfeed.read":       workFeedRead(deps),
		"miniapp.workfeed.action.run": workFeedActionRun(deps),
		"miniapp.workfeed.answer":     workFeedAnswer(deps),
	}
}

// workFeedRead stamps a card as read (the user opened it) without settling it: the
// card stays in the feed, just de-emphasized client-side. Idempotent. Softer than
// ack — reading is not 완료.
func workFeedRead(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ItemID string `json:"itemId"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		itemID := strings.TrimSpace(p.ItemID)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		item, err := deps.Store.MarkRead(itemID)
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
	})
}

// workFeedAnswer settles a question card and returns its asking SessionKey so the
// native can deliver the user's free-text answer there (the agent then reacts).
// Choice answers go through action.run instead (ActionAnswer/ActionAck chips);
// this is the free-text reply path for question cards without fixed options.
func workFeedAnswer(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ItemID string `json:"itemId"`
		Answer string `json:"answer"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		itemID := strings.TrimSpace(p.ItemID)
		answer := strings.TrimSpace(p.Answer)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		if answer == "" {
			return rpcerr.MissingParam("answer").Response(req.ID)
		}
		// Ack settles the card and returns it (carrying the asking SessionKey).
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
	})
}

func workFeedList(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit        int   `json:"limit,omitempty"`
		IncludeAcked bool  `json:"includeAcked,omitempty"`
		SinceMs      int64 `json:"sinceMs,omitempty"`
		BeforeMs     int64 `json:"beforeMs,omitempty"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

func workFeedAck(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

func workFeedActionRun(deps WorkFeedDeps) rpcutil.HandlerFunc {
	type params struct {
		ItemID   string `json:"itemId"`
		ActionID string `json:"actionId"`
		Comment  string `json:"comment,omitempty"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		itemID := strings.TrimSpace(p.ItemID)
		actionID := strings.TrimSpace(p.ActionID)
		if itemID == "" {
			return rpcerr.MissingParam("itemId").Response(req.ID)
		}
		if actionID == "" {
			return rpcerr.MissingParam("actionId").Response(req.ID)
		}
		result, err := deps.Store.RunAction(itemID, actionID, p.Comment)
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
		if deps.OnMetaProposal != nil &&
			result.Item.Source == "genesis-meta" &&
			strings.HasPrefix(actionID, "meta:") {
			deps.OnMetaProposal(result.Item, actionID)
		}
		if deps.OnEvolveVerdict != nil &&
			result.Item.Source == "genesis-evolve-verdict" &&
			strings.HasPrefix(actionID, "evolve-verdict:") {
			deps.OnEvolveVerdict(result.Item, actionID)
		}
		if deps.OnLadder != nil &&
			result.Item.Source == "genesis-ladder" &&
			strings.HasPrefix(actionID, "ladder:") {
			deps.OnLadder(result.Item, actionID)
		}
		if deps.OnDeadlineDone != nil && strings.HasPrefix(actionID, "deadline_done:") {
			deps.OnDeadlineDone(result.Item, actionID)
		}
		if deps.OnIdentityReviewed != nil && strings.HasPrefix(actionID, "identity_reviewed:") {
			deps.OnIdentityReviewed(result.Item, actionID)
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
	})
}
