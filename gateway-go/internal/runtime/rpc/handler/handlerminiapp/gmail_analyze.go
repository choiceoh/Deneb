// gmail_analyze.go — miniapp.gmail.analyze RPC.
//
// Operator taps "🔍 분석" on a Mini App email detail; the gateway runs the
// same analysis pipeline the agent's `gmail` tool uses (intent + key
// stakeholders + risks + next-step suggestions) and returns the result as
// markdown for inline rendering.
//
// Reuses `gmailpoll.AnalyzeEmailPipeline` verbatim — no separate prompt
// or LLM wrapper to maintain. The pipeline already falls back to a single
// LLM call when LocalClient is absent, so the Mini App path doesn't need
// to know about the two-stage detail.
//
// Long requests: the pipeline's stage-2 timeout is 240 seconds. The
// dispatcher wraps every handler in safeCall with the request context;
// the HTTP bridge does not impose its own deadline, so the call is bound
// by the operator's network and the LLM provider. Frontend shows a
// loading indicator and warns the operator after 30s.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// AnalyzePipeline is the subset of gmailpoll the analyze handler depends
// on. Pulling it behind an interface keeps the handler testable without
// standing up an LLM.
type AnalyzePipeline interface {
	Analyze(ctx context.Context, msg *gmail.MessageDetail) (string, error)
}

// GmailAnalyzeDeps groups the factories the handler needs. Client supplies
// the Gmail OAuth client; Pipeline supplies the analysis driver
// (production wires it to `gmailpoll.AnalyzeEmailPipeline` with a real
// LLM client + main model).
type GmailAnalyzeDeps struct {
	Client   func() (GmailClient, error)
	Pipeline func() (AnalyzePipeline, error)
}

// GmailAnalyzeMethods returns the miniapp.gmail.analyze handler. Returns
// nil if either factory is missing so registration can skip cleanly when
// the LLM client hasn't been wired (e.g. early in startup).
func GmailAnalyzeMethods(deps GmailAnalyzeDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil || deps.Pipeline == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.gmail.analyze": gmailAnalyze(deps),
	}
}

func gmailAnalyze(deps GmailAnalyzeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	type out struct {
		ID         string `json:"id"`
		Subject    string `json:"subject,omitempty"`
		From       string `json:"from,omitempty"`
		Date       string `json:"date,omitempty"`
		Analysis   string `json:"analysis"`
		DurationMs int64  `json:"durationMs"`
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
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}

		client, err := deps.Client()
		if err != nil {
			return rpcerr.WrapUnavailable("gmail client unavailable", err).Response(req.ID)
		}
		pipeline, err := deps.Pipeline()
		if err != nil {
			return rpcerr.WrapUnavailable("analysis pipeline unavailable", err).Response(req.ID)
		}

		msg, err := client.GetMessage(ctx, p.ID)
		if err != nil {
			return mapGmailError(req.ID, "gmail get failed", err)
		}
		if msg == nil {
			return rpcerr.NotFound("message " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}

		start := time.Now()
		analysis, err := pipeline.Analyze(ctx, msg)
		dur := time.Since(start)
		if err != nil {
			return rpcerr.WrapUnavailable("email analysis failed", err).Response(req.ID)
		}
		if strings.TrimSpace(analysis) == "" {
			return rpcerr.Unavailable("analysis returned empty result").Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, out{
			ID:         msg.ID,
			Subject:    msg.Subject,
			From:       msg.From,
			Date:       normalizeDate(msg.Date),
			Analysis:   analysis,
			DurationMs: dur.Milliseconds(),
		})
	}
}

// ErrAnalyzeNoLLM is returned by the production pipeline factory when no
// LLM client / main model is configured (e.g., dev environment without
// any provider credentials). Surfaced as UNAVAILABLE to the client.
var ErrAnalyzeNoLLM = errors.New("analyze pipeline: LLM client not configured")

// PipelineFromGmailpoll adapts gmailpoll.AnalyzeEmailPipeline to the
// AnalyzePipeline interface. Returns ErrAnalyzeNoLLM when the inputs are
// missing so callers can map cleanly to UNAVAILABLE without touching the
// gmailpoll package internals.
func PipelineFromGmailpoll(gmailClient *gmail.Client, llmClient *llm.Client, mainModel string) (AnalyzePipeline, error) {
	if llmClient == nil || strings.TrimSpace(mainModel) == "" {
		return nil, ErrAnalyzeNoLLM
	}
	return &gmailpollPipeline{
		deps: gmailpoll.PipelineDeps{
			GmailClient: gmailClient,
			LLMClient:   llmClient,
			MainModel:   mainModel,
		},
	}, nil
}

type gmailpollPipeline struct {
	deps gmailpoll.PipelineDeps
}

func (g *gmailpollPipeline) Analyze(ctx context.Context, msg *gmail.MessageDetail) (string, error) {
	return gmailpoll.AnalyzeEmailPipeline(ctx, g.deps, msg)
}
