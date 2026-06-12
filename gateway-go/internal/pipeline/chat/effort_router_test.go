package chat

import (
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// TestDecideThinkingOff exercises the false-easy-averse heuristic: only
// obviously-simple short conversational messages route to non-thinking.
func TestDecideThinkingOff(t *testing.T) {
	cases := []struct {
		params  RunParams
		wantOff bool
		name    string
	}{
		{RunParams{Message: "안녕"}, true, "greeting"},
		{RunParams{Message: "고마워!"}, true, "thanks"},
		{RunParams{Message: "오늘 일정 뭐야?"}, true, "short tool-ish query"},
		{RunParams{Message: "응 그걸로 해줘"}, true, "short ack"},
		{RunParams{Message: ""}, false, "empty"},
		{RunParams{Message: "안녕", EphemeralUser: true}, false, "ephemeral automation keeps thinking"},
		{RunParams{Message: "브리핑 보내줘", AutoDeliveredOutput: true}, true, "auto-delivered alone is interactive native chat — routable"},
		{RunParams{Message: "브리핑 보내줘", EphemeralUser: true, AutoDeliveredOutput: true}, false, "ephemeral automation keeps thinking even when auto-delivered"},
		{RunParams{Message: "아침 일정 브리핑 보내줘", SessionKey: "cron:morning-letter:123", AutoDeliveredOutput: true}, false, "cron session prefix keeps thinking (cron is not ephemeral)"},
		{RunParams{Message: "이거 봐줘", Attachments: []ChatAttachment{{}}}, false, "attachment caption keeps thinking"},
		{RunParams{Message: "이 코드 분석해줘"}, false, "hard signal: 분석"},
		{RunParams{Message: "내일 회의 계획 세워줘"}, false, "hard signal: 계획"},
		{RunParams{Message: "왜 이렇게 됐지?"}, false, "hard signal: 왜"},
		{RunParams{Message: "3 곱하기 47 계산해줘"}, false, "hard signal: 계산"},
		{RunParams{Message: "buggy code fix please"}, false, "hard signal: english"},
		{RunParams{Message: "어제 회의록이랑 오늘 메일 내용 종합해서 핵심 흐름이 어떻게 이어지는지 보고서 형태로 만들어줘 그리고 빠진 부분 짚어줘"}, false, "long"},
		{RunParams{Message: "```\nprint(1)\n```"}, false, "code fence"},
	}
	for _, c := range cases {
		off, reason := decideThinkingOff(c.params)
		if off != c.wantOff {
			t.Errorf("%s: decideThinkingOff(%q) = %v (reason=%s), want %v", c.name, c.params.Message, off, reason, c.wantOff)
		}
	}
}

// TestApplyEffortRouter_RouteAndRestore covers the route lifecycle: apply
// swaps Thinking to template-toggle "disabled", restore puts the session's
// original config back by plain assignment (no shared state).
func TestApplyEffortRouter_RouteAndRestore(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	orig := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	origMod := func(int) *llm.ThinkingConfig { return nil }
	cfg := agent.AgentConfig{Model: "deepseek-v4-flash", Thinking: orig, ThinkingModulator: origMod}

	route, decision := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, "thinking", nil)
	if route == nil || decision != "routed:short-conversational" {
		t.Fatalf("simple message on toggle model must route (decision=%q)", decision)
	}
	if !effortRouted(&cfg) {
		t.Fatal("routed config must report effortRouted")
	}
	if cfg.Thinking.Type != "disabled" || cfg.Thinking.TemplateKwarg != "thinking" {
		t.Fatalf("Thinking = %+v, want disabled+kwarg", cfg.Thinking)
	}
	// Per-step revert (Ares): early turns stay disabled, later turns get
	// thinking back. With no session thinking, the revert sentinel is the
	// provider-default "enabled".
	if cfg.ThinkingModulator == nil {
		t.Fatal("routed runs must install the step-revert modulator")
	}
	if got := cfg.ThinkingModulator(0); got == nil || got.Type != "disabled" {
		t.Fatalf("turn 0 must stay disabled, got %+v", got)
	}
	if got := cfg.ThinkingModulator(effortStepRevertTurn); got == nil || got.Type != "enabled" || got.BudgetTokens != 4096 {
		t.Fatalf("turn %d must revert to the session thinking, got %+v", effortStepRevertTurn, got)
	}

	restoreEffort(&cfg, route)
	if cfg.Thinking != orig {
		t.Fatalf("restore must put the ORIGINAL ThinkingConfig back, got %+v", cfg.Thinking)
	}
	if cfg.ThinkingModulator == nil {
		t.Fatal("restore must put the original modulator back")
	}
	if effortRouted(&cfg) {
		t.Fatal("restored config must not report effortRouted")
	}
}

// TestApplyEffortRouter_Gates verifies the no-route paths: flag off, no
// capability kwarg, hard message.
func TestApplyEffortRouter_Gates(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "")
	cfg := agent.AgentConfig{Model: "deepseek-v4-flash"}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, "thinking", nil); r != nil || d != "" {
		t.Error("flag off must not route and reports no decision")
	}
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, "", nil); r != nil || d != "" {
		t.Error("empty capability kwarg must not route")
	}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "이 코드 분석해줘"}, "thinking", nil); r != nil || d != "kept:hard-signal:분석" {
		t.Errorf("hard message must report kept decision, got %q", d)
	}
	if cfg.Thinking != nil {
		t.Error("no-route paths must leave Thinking untouched")
	}
	// force mode (eval baseline): routes even hard messages.
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "force")
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "이 코드 분석해줘"}, "thinking", nil); r == nil || d != "routed:forced" {
		t.Errorf("force mode must route everything eligible, got %q", d)
	}
}

// TestEscalatableEffortFailure covers the three router-attributable failure
// shapes plus the exclusions.
func TestEscalatableEffortFailure(t *testing.T) {
	if !escalatableEffortFailure(errModelStalled, nil) {
		t.Error("stall must escalate")
	}
	wrapped := errors.Join(errors.New("consume stream"), agent.ErrStreamIdle)
	if !escalatableEffortFailure(wrapped, nil) {
		t.Error("wrapped stream-idle must escalate")
	}
	empty := &agent.AgentResult{StopReason: "end_turn", AllText: "  "}
	if !escalatableEffortFailure(nil, empty) {
		t.Error("degenerate empty success must escalate")
	}
	withText := &agent.AgentResult{StopReason: "end_turn", AllText: "답변"}
	if escalatableEffortFailure(nil, withText) {
		t.Error("a real answer must not escalate")
	}
	withTools := &agent.AgentResult{StopReason: "end_turn", AllText: "", TotalToolCalls: 2}
	if escalatableEffortFailure(nil, withTools) {
		t.Error("empty text after tool calls is not the degenerate shape")
	}
	if !resultRanTools(withTools) || resultRanTools(empty) {
		t.Error("resultRanTools misclassifies")
	}
	if escalatableEffortFailure(errors.New("http 500"), nil) {
		t.Error("hard errors flow to the fallback chain, not escalation")
	}
}

// TestEffortMode checks the env mode parsing (off / adaptive / force).
func TestEffortMode(t *testing.T) {
	for v, want := range map[string]string{
		"": effortModeOff, "off": effortModeOff, "0": effortModeOff,
		"1": effortModeAdaptive, "true": effortModeAdaptive, "adaptive": effortModeAdaptive,
		"force": effortModeForce,
	} {
		t.Setenv("DENEB_ADAPTIVE_EFFORT", v)
		if got := effortMode(); got != want {
			t.Errorf("effortMode(%q) = %q, want %q", v, got, want)
		}
	}
}
