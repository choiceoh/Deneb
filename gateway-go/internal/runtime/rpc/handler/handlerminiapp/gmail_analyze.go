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
	Analyze(ctx context.Context, msg *gmail.MessageDetail) (gmailpoll.AnalysisResult, error)
}

// ProjectRef is a related project wiki page surfaced to the Mini App: the
// path plus enriched title/summary so the client renders a chip without a
// second round trip.
type ProjectRef struct {
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// WikiAnalysisInput is the payload the handler hands to SaveToWiki when a
// fresh analysis succeeds. Kept as a flat struct so the handler stays
// ignorant of the wiki package's frontmatter/page types.
type WikiAnalysisInput struct {
	MsgID           string
	Subject         string
	From            string
	Date            string
	Analysis        string
	RelatedProjects []string // wiki paths of related project pages → page.Related
}

// GmailAnalyzeDeps groups the factories the handler needs. Client supplies
// the Gmail OAuth client; Pipeline supplies the analysis driver
// (production wires it to `gmailpoll.AnalyzeEmailPipeline` with a real
// LLM client + main model). Cache and SaveToWiki are optional; nil/zero
// values disable cache lookups and wiki persistence respectively so the
// handler keeps working when those subsystems aren't wired yet.
type GmailAnalyzeDeps struct {
	Client     func() (GmailClient, error)
	Pipeline   func() (AnalyzePipeline, error)
	Cache      *AnalysisStore
	SaveToWiki func(in WikiAnalysisInput) error
	// WikiStore (optional) enriches related-project paths with their
	// title/summary for display. nil → chips fall back to the bare path.
	WikiStore func() (MemorySearcher, error)
}

// GmailAnalyzeMethods returns the miniapp.gmail.analyze handler. Returns
// nil if either factory is missing so registration can skip cleanly when
// the LLM client hasn't been wired (e.g. early in startup).
func GmailAnalyzeMethods(deps GmailAnalyzeDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil || deps.Pipeline == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.gmail.analyze":         gmailAnalyze(deps),
		"miniapp.gmail.analysis_cached": gmailAnalysisCached(deps),
	}
}

func gmailAnalyze(deps GmailAnalyzeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID    string `json:"id"`
		Force bool   `json:"force,omitempty"`
	}
	type out struct {
		ID              string       `json:"id"`
		Subject         string       `json:"subject,omitempty"`
		From            string       `json:"from,omitempty"`
		Date            string       `json:"date,omitempty"`
		Analysis        string       `json:"analysis"`
		RelatedProjects []ProjectRef `json:"relatedProjects,omitempty"`
		DurationMs      int64        `json:"durationMs"`
		Cached          bool         `json:"cached"`
		CreatedAt       time.Time    `json:"createdAt"`
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

		// Cache lookup. force=true skips it so "🔄 다시 분석" always
		// re-runs the LLM. Load errors are logged-and-ignored so a
		// corrupt cache file never blocks a fresh run.
		if !p.Force && deps.Cache != nil {
			if rec, err := deps.Cache.load(p.ID); err == nil && rec != nil {
				return rpcutil.RespondOK(req.ID, out{
					ID:              rec.MsgID,
					Subject:         rec.Subject,
					From:            rec.From,
					Date:            rec.Date,
					Analysis:        rec.Analysis,
					RelatedProjects: enrichProjects(deps, rec.RelatedProjects),
					DurationMs:      rec.DurationMs,
					Cached:          true,
					CreatedAt:       rec.CreatedAt,
				})
			}
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
		result, err := pipeline.Analyze(ctx, msg)
		dur := time.Since(start)
		if err != nil {
			return rpcerr.WrapUnavailable("email analysis failed", err).Response(req.ID)
		}
		if strings.TrimSpace(result.Text) == "" {
			return rpcerr.Unavailable("analysis returned empty result").Response(req.ID)
		}

		date := normalizeDate(msg.Date)
		now := time.Now().UTC()
		rec := &analysisRecord{
			MsgID:           msg.ID,
			Subject:         msg.Subject,
			From:            msg.From,
			Date:            date,
			Analysis:        result.Text,
			RelatedProjects: result.RelatedProjects,
			DurationMs:      dur.Milliseconds(),
			PromptVersion:   AnalysisPromptVersion,
			CreatedAt:       now,
		}
		// Persistence is best-effort. A working LLM result must not be
		// surfaced as a failure just because disk or wiki write blipped.
		if deps.Cache != nil {
			_ = deps.Cache.save(rec)
		}
		if deps.SaveToWiki != nil {
			_ = deps.SaveToWiki(WikiAnalysisInput{
				MsgID:           msg.ID,
				Subject:         msg.Subject,
				From:            msg.From,
				Date:            date,
				Analysis:        result.Text,
				RelatedProjects: result.RelatedProjects,
			})
		}

		return rpcutil.RespondOK(req.ID, out{
			ID:              msg.ID,
			Subject:         msg.Subject,
			From:            msg.From,
			Date:            date,
			Analysis:        result.Text,
			RelatedProjects: enrichProjects(deps, result.RelatedProjects),
			DurationMs:      dur.Milliseconds(),
			Cached:          false,
			CreatedAt:       now,
		})
	}
}

// enrichProjects resolves project wiki paths to ProjectRefs with title and
// summary for display. Best-effort: a path that can't be read falls back to
// just the path so the chip still links somewhere.
func enrichProjects(deps GmailAnalyzeDeps, paths []string) []ProjectRef {
	if len(paths) == 0 {
		return nil
	}
	var store MemorySearcher
	if deps.WikiStore != nil {
		store, _ = deps.WikiStore()
	}
	refs := make([]ProjectRef, 0, len(paths))
	for _, p := range paths {
		ref := ProjectRef{Path: p}
		if store != nil {
			if page, err := store.ReadPage(p); err == nil && page != nil {
				ref.Title = page.Meta.Title
				ref.Summary = page.Meta.Summary
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

// gmailAnalysisCached returns a stored analysis without ever running the
// LLM. The Mini App calls this when opening an email so a pre-computed
// analysis (from the autonomous poller or a prior manual run) shows up
// instantly, including its related projects. On a miss it returns
// cached=false with an empty analysis, and the client offers the manual
// analyze button.
func gmailAnalysisCached(deps GmailAnalyzeDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	type out struct {
		ID              string       `json:"id"`
		Analysis        string       `json:"analysis"`
		RelatedProjects []ProjectRef `json:"relatedProjects,omitempty"`
		Cached          bool         `json:"cached"`
		CreatedAt       time.Time    `json:"createdAt,omitempty"`
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
		if deps.Cache == nil {
			return rpcutil.RespondOK(req.ID, out{ID: p.ID})
		}
		rec, err := deps.Cache.load(p.ID)
		if err != nil || rec == nil {
			return rpcutil.RespondOK(req.ID, out{ID: p.ID})
		}
		return rpcutil.RespondOK(req.ID, out{
			ID:              rec.MsgID,
			Analysis:        rec.Analysis,
			RelatedProjects: enrichProjects(deps, rec.RelatedProjects),
			Cached:          true,
			CreatedAt:       rec.CreatedAt,
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
func PipelineFromGmailpoll(gmailClient *gmail.Client, llmClient *llm.Client, mainModel string, projectsFn func() []gmailpoll.ProjectCandidate) (AnalyzePipeline, error) {
	if llmClient == nil || strings.TrimSpace(mainModel) == "" {
		return nil, ErrAnalyzeNoLLM
	}
	return &gmailpollPipeline{
		deps: gmailpoll.PipelineDeps{
			GmailClient: gmailClient,
			LLMClient:   llmClient,
			MainModel:   mainModel,
			ProjectsFn:  projectsFn,
		},
	}, nil
}

type gmailpollPipeline struct {
	deps gmailpoll.PipelineDeps
}

func (g *gmailpollPipeline) Analyze(ctx context.Context, msg *gmail.MessageDetail) (gmailpoll.AnalysisResult, error) {
	return gmailpoll.AnalyzeEmailPipeline(ctx, g.deps, msg)
}
