// Package shadow provides RPC handlers for the shadow monitoring service.
package shadow

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	shadowsvc "github.com/choiceoh/deneb/gateway-go/internal/shadow"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for shadow RPC methods.
type Deps struct {
	Shadow *shadowsvc.Service
}

// Methods returns all shadow-related RPC handlers.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Shadow == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		// Core.
		"shadow.status":       handleStatus(deps),
		"shadow.status.full":  handleExtendedStatus(deps),
		"shadow.tasks":        handleTasks(deps),
		"shadow.task.dismiss": handleDismiss(deps),
		// Analytics.
		"shadow.analytics": handleAnalytics(deps),
		// Memory.
		"shadow.facts": handleFacts(deps),
		// Errors.
		"shadow.errors":         handleErrors(deps),
		"shadow.errors.insight": handleErrorInsight(deps),
		// Code review.
		"shadow.reviews": handleReviews(deps),
		// Cron suggestions.
		"shadow.cron.suggestions":        handleCronSuggestions(deps),
		"shadow.cron.suggestion.dismiss": handleCronDismiss(deps),
		// Session continuity.
		"shadow.continuity":        handleContinuity(deps),
		"shadow.continuity.resume": handleResumeSummary(deps),
	}
}

// --- shadow.status ---

func handleStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Shadow.Status())
	}
}

// --- shadow.status.full ---

func handleExtendedStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Shadow.ExtendedStatus())
	}
}

// --- shadow.tasks ---

func handleTasks(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		tasks := deps.Shadow.PendingTasks()
		if tasks == nil {
			tasks = []shadowsvc.TrackedTask{}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"tasks": tasks,
			"count": len(tasks),
		})
	}
}

// --- shadow.task.dismiss ---

type dismissParams struct {
	ID string `json:"id"`
}

func handleDismiss(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p dismissParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.ID == "" {
			return rpcerr.New(protocol.ErrMissingParam, "id required").Response(req.ID)
		}
		ok := deps.Shadow.DismissTask(p.ID)
		if !ok {
			return rpcerr.New(protocol.ErrNotFound, "task not found or already dismissed").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"dismissed": true, "id": p.ID})
	}
}

// --- shadow.analytics ---

func handleAnalytics(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.RespondOK(req.ID, deps.Shadow.UsageAnalytics().GetReport())
	}
}

// --- shadow.facts ---

type factsParams struct {
	Category string `json:"category,omitempty"`
}

func handleFacts(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p factsParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		facts := deps.Shadow.MemoryConsolidator().GetExtractedFacts(p.Category)
		if facts == nil {
			facts = []shadowsvc.ExtractedFact{}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"facts": facts,
			"count": len(facts),
		})
	}
}

// --- shadow.errors ---

func handleErrors(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		recurring := deps.Shadow.ErrorLearner().GetRecurringErrors()
		if recurring == nil {
			recurring = []shadowsvc.ErrorRecord{}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"recurringErrors": recurring,
			"count":           len(recurring),
		})
	}
}

// --- shadow.errors.insight ---

type insightParams struct {
	Content string `json:"content"`
}

func handleErrorInsight(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p insightParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Content == "" {
			return rpcerr.New(protocol.ErrMissingParam, "content required").Response(req.ID)
		}
		insight := deps.Shadow.ErrorLearner().GetInsight(p.Content)
		if insight == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{"found": false})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"found": true, "insight": insight})
	}
}

// --- shadow.reviews ---

func handleReviews(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		reviews := deps.Shadow.CodeReviewer().GetRecentReviews()
		if reviews == nil {
			reviews = []shadowsvc.CodeReviewResult{}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"reviews": reviews,
			"count":   len(reviews),
		})
	}
}

// --- shadow.cron.suggestions ---

func handleCronSuggestions(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		suggestions := deps.Shadow.CronSuggester().GetSuggestions()
		if suggestions == nil {
			suggestions = []shadowsvc.CronSuggestion{}
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"suggestions": suggestions,
			"count":       len(suggestions),
		})
	}
}

// --- shadow.cron.suggestion.dismiss ---

func handleCronDismiss(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p dismissParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.ID == "" {
			return rpcerr.New(protocol.ErrMissingParam, "id required").Response(req.ID)
		}
		ok := deps.Shadow.CronSuggester().DismissSuggestion(p.ID)
		if !ok {
			return rpcerr.NotFound("suggestion").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"dismissed": true, "id": p.ID})
	}
}

// --- shadow.continuity ---

func handleContinuity(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		snapshot := deps.Shadow.SessionContinuity().LoadSnapshot()
		if snapshot == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{"available": false})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"available": true,
			"snapshot":  snapshot,
		})
	}
}

// --- shadow.continuity.resume ---

func handleResumeSummary(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		summary := deps.Shadow.SessionContinuity().GetResumeSummary()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"available": summary != "",
			"summary":   summary,
		})
	}
}
