package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compactuner"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakePromptTuner struct {
	calls  int
	report compactuner.Report
}

func (f *fakePromptTuner) RunWithReport(context.Context) compactuner.Report {
	f.calls++
	return f.report
}

func TestPromptTunerRun(t *testing.T) {
	tuner := &fakePromptTuner{report: compactuner.Report{
		Ran:           true,
		Changed:       true,
		Reason:        "updated",
		LeafSummaries: 4,
		Added:         []string{"금액은 정확한 숫자와 통화로 보존하라"},
		AfterCount:    1,
	}}
	methods := PromptTunerMethods(PromptTunerDeps{Tuner: func() PromptTuner { return tuner }})
	if methods["miniapp.prompt_tuner.run"] == nil {
		t.Fatalf("PromptTunerMethods missing run method: %#v", methods)
	}

	var out PromptTunerRunResponse
	decode(t, methods["miniapp.prompt_tuner.run"](
		authedCtx(),
		reqWith(t, "miniapp.prompt_tuner.run", map[string]any{"target": "compaction"}),
	), &out)

	if tuner.calls != 1 {
		t.Fatalf("calls=%d, want 1", tuner.calls)
	}
	if out.Target != "compaction" || !out.Report.Changed || out.Report.Reason != "updated" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestPromptTunerRun_DefaultTarget(t *testing.T) {
	tuner := &fakePromptTuner{report: compactuner.Report{Reason: "too_few_summaries"}}
	methods := PromptTunerMethods(PromptTunerDeps{Tuner: func() PromptTuner { return tuner }})

	var out PromptTunerRunResponse
	decode(t, methods["miniapp.prompt_tuner.run"](
		authedCtx(),
		reqWith(t, "miniapp.prompt_tuner.run", map[string]any{}),
	), &out)
	if out.Target != "compaction" || out.Report.Reason != "too_few_summaries" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestPromptTunerRun_Errors(t *testing.T) {
	methods := PromptTunerMethods(PromptTunerDeps{Tuner: func() PromptTuner { return nil }})

	resp := methods["miniapp.prompt_tuner.run"](
		authedCtx(),
		reqWith(t, "miniapp.prompt_tuner.run", map[string]any{"target": "mail"}),
	)
	if resp.OK || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("unsupported target resp = %+v", resp)
	}

	resp = methods["miniapp.prompt_tuner.run"](
		authedCtx(),
		reqWith(t, "miniapp.prompt_tuner.run", map[string]any{}),
	)
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("nil tuner resp = %+v", resp)
	}

	resp = methods["miniapp.prompt_tuner.run"](context.Background(), reqWith(t, "miniapp.prompt_tuner.run", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("unauth resp = %+v", resp)
	}
}

func TestPromptTunerMethods_NilFactoryReturnsNil(t *testing.T) {
	if got := PromptTunerMethods(PromptTunerDeps{}); got != nil {
		t.Fatalf("PromptTunerMethods(nil) = %#v, want nil", got)
	}
}
