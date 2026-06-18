package handlerminiapp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compactuner"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// PromptTuner is the runnable prompt-improvement engine surface. Today the
// concrete implementation is the compaction-guideline tuner; the RPC shape is
// target-based so mail/persona prompt tuners can be added without another
// client contract.
type PromptTuner interface {
	RunWithReport(ctx context.Context) compactuner.Report
}

type PromptTunerDeps struct {
	Tuner func() PromptTuner
}

// PromptTunerRunResponse is returned by miniapp.prompt_tuner.run.
//
//deneb:wire
type PromptTunerRunResponse struct {
	Target string            `json:"target"`
	Report PromptTunerReport `json:"report"`
}

// PromptTunerReport is the miniapp wire copy of compactuner.Report.
//
//deneb:wire
type PromptTunerReport struct {
	Ran           bool     `json:"ran"`
	Changed       bool     `json:"changed"`
	Reason        string   `json:"reason"`
	Error         string   `json:"error,omitempty"`
	LeafSummaries int      `json:"leafSummaries"`
	MinSummaries  int      `json:"minSummaries"`
	Proposed      []string `json:"proposed,omitempty"`
	Added         []string `json:"added,omitempty"`
	BeforeCount   int      `json:"beforeCount"`
	AfterCount    int      `json:"afterCount"`
}

func PromptTunerMethods(deps PromptTunerDeps) map[string]rpcutil.HandlerFunc {
	if deps.Tuner == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.prompt_tuner.run": promptTunerRun(deps),
	}
}

func promptTunerRun(deps PromptTunerDeps) rpcutil.HandlerFunc {
	type params struct {
		Target string `json:"target,omitempty"`
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
		target := strings.TrimSpace(p.Target)
		if target == "" {
			target = "compaction"
		}
		if target != "compaction" {
			return rpcerr.InvalidRequest("unsupported prompt tuner target: " + rpcutil.TruncateForError(target)).Response(req.ID)
		}
		tuner := deps.Tuner()
		if tuner == nil {
			return rpcerr.Unavailable("prompt tuner unavailable").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, PromptTunerRunResponse{
			Target: target,
			Report: promptTunerReport(tuner.RunWithReport(ctx)),
		})
	}
}

func promptTunerReport(r compactuner.Report) PromptTunerReport {
	return PromptTunerReport{
		Ran:           r.Ran,
		Changed:       r.Changed,
		Reason:        r.Reason,
		Error:         r.Error,
		LeafSummaries: r.LeafSummaries,
		MinSummaries:  r.MinSummaries,
		Proposed:      append([]string{}, r.Proposed...),
		Added:         append([]string{}, r.Added...),
		BeforeCount:   r.BeforeCount,
		AfterCount:    r.AfterCount,
	}
}
