// Package insights provides the insights.generate RPC handler.
//
// It is a thin wrapper that takes a *insights.Engine and returns both the
// structured Report (for programmatic callers) and a ready-to-send MarkdownV2
// string (for slash-command flows that forward directly to Telegram).
package insights

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps carries the dependencies for insights handlers.
// Logger is optional; if nil, slog.Default() is used.
type Deps struct {
	Engine *insights.Engine
	Logger *slog.Logger
}

// GenerateParams is the RPC argument shape for insights.generate.
type GenerateParams struct {
	Days int `json:"days"`
}

// GenerateResult is the RPC response shape.
type GenerateResult struct {
	Report   *insights.Report `json:"report"`
	Markdown string           `json:"markdown"`
	Plain    string           `json:"plain"`
}

// Methods returns the insights domain handler map.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Engine == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"insights.generate": generateHandler(deps),
	}
}

func generateHandler(deps Deps) rpcutil.HandlerFunc {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p GenerateParams
		if len(req.Params) > 0 {
			if _, errResp := rpcutil.DecodeParams[GenerateParams](req); errResp != nil {
				return errResp
			}
			_ = rpcutil.UnmarshalParams(req.Params, &p)
		}
		if p.Days <= 0 {
			p.Days = 30
		}
		rep, err := deps.Engine.Generate(ctx, p.Days)
		if err != nil {
			// Error case = user-observable failure for someone invoking the RPC.
			logger.Error("insights.generate failed", "days", p.Days, "error", err)
			return rpcerr.WrapUnavailable("insights generation failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, &GenerateResult{
			Report:   rep,
			Markdown: insights.RenderMarkdownV2(rep),
			Plain:    insights.RenderPlain(rep),
		})
	}
}
