package chat

import (
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

// enabledProfile is the shape the resolver produces for a dual-mode model: the
// shipped defaults plus a template toggle. disabledProfile is an inert model
// (no toggle) — the router must not fire.
func enabledProfile() router.Profile {
	p := router.DefaultProfile()
	p.Enabled = true
	p.ToggleKwarg = "thinking"
	return p
}

func disabledProfile() router.Profile { return router.DefaultProfile() }

// TestIsAutomationRun pins the chat-side automation classification the router
// consumes as Request.IsAutomation: ephemeral + cron/acp prefixes are
// automation; AutoDeliveredOutput alone is an interactive native chat.
func TestIsAutomationRun(t *testing.T) {
	cases := []struct {
		params RunParams
		want   bool
		name   string
	}{
		{RunParams{Message: "안녕", EphemeralUser: true}, true, "ephemeral automation"},
		{RunParams{Message: "아침 브리핑", SessionKey: "cron:morning-letter:123"}, true, "cron prefix"},
		{RunParams{Message: "x", SessionKey: "acp:subagent:1"}, true, "acp prefix"},
		{RunParams{Message: "브리핑 보내줘", AutoDeliveredOutput: true}, false, "auto-delivered alone is interactive"},
		{RunParams{Message: "안녕"}, false, "plain interactive"},
	}
	for _, c := range cases {
		if got := isAutomationRun(c.params); got != c.want {
			t.Errorf("%s: isAutomationRun = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestApplyEffortRouter_RouteAndRestore covers the route lifecycle: apply swaps
// Thinking to template-toggle "disabled", restore puts the session's original
// config back by plain assignment (no shared state).
func TestApplyEffortRouter_RouteAndRestore(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	orig := &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}
	origMod := func(int, []agent.ToolActivity) *llm.ThinkingConfig { return nil }
	cfg := agent.AgentConfig{Model: "deepseek-v4-flash", Thinking: orig, ThinkingModulator: origMod}
	profile := enabledProfile()

	route, decision := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, nil, profile, nil)
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
	if got := cfg.ThinkingModulator(0, nil); got == nil || got.Type != "disabled" {
		t.Fatalf("turn 0 must stay disabled, got %+v", got)
	}
	if got := cfg.ThinkingModulator(profile.StepCeilingTurn, nil); got == nil || got.Type != "enabled" || got.BudgetTokens != 4096 {
		t.Fatalf("turn %d must revert to the session thinking, got %+v", profile.StepCeilingTurn, got)
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

// TestApplyEffortRouter_Gates verifies the no-route paths: flag off, inert
// profile (no toggle), hard message — plus force-mode behavior.
func TestApplyEffortRouter_Gates(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "")
	cfg := agent.AgentConfig{Model: "deepseek-v4-flash"}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, nil, enabledProfile(), nil); r != nil || d != "" {
		t.Error("flag off must not route and reports no decision")
	}
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, nil, disabledProfile(), nil); r != nil || d != "" {
		t.Error("inert profile (no toggle) must not route")
	}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "이 코드 분석해줘"}, nil, enabledProfile(), nil); r != nil || d != "kept:hard-signal:분석" {
		t.Errorf("hard message must report kept decision, got %q", d)
	}
	if cfg.Thinking != nil {
		t.Error("no-route paths must leave Thinking untouched")
	}
	// force mode (eval baseline): routes even hard messages — but never
	// automation/attachment-protected runs.
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "force")
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "이 코드 분석해줘"}, nil, enabledProfile(), nil); r == nil || d != "routed:forced" {
		t.Errorf("force mode must route everything eligible, got %q", d)
	}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "메일 분석", SessionKey: "cron:mail:1"}, nil, enabledProfile(), nil); r != nil || d != "kept:automation" {
		t.Errorf("force must NOT override the automation guard, got %q", d)
	}
	cfg.Thinking = nil
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

// TestEffortStepThinking covers the per-step policy e_t = f(turn, o_t) with the
// thresholds sourced from the profile. o_t comes from the executor's run-scoped
// ToolActivity records (no message re-parsing).
func TestEffortStepThinking(t *testing.T) {
	profile := enabledProfile()
	disabled := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: "thinking"}
	revert := &llm.ThinkingConfig{Type: "enabled"}
	small := []agent.ToolActivity{{Name: "read", Turn: 1, OutputRunes: 2}}
	if got := effortStepThinking(profile, 0, nil, disabled, revert); got != disabled {
		t.Error("turn 0 must stay disabled")
	}
	if got := effortStepThinking(profile, 1, small, disabled, revert); got != disabled {
		t.Error("turn 1 with a small clean observation stays disabled")
	}
	big := []agent.ToolActivity{{Name: "read", Turn: 1, OutputRunes: 2100}}
	if got := effortStepThinking(profile, 1, big, disabled, revert); got != revert {
		t.Error("a big observation must revert to thinking")
	}
	errRes := []agent.ToolActivity{{Name: "exec", Turn: 1, IsError: true, OutputRunes: 4}}
	if got := effortStepThinking(profile, 1, errRes, disabled, revert); got != revert {
		t.Error("an error observation must revert to thinking")
	}
	if got := effortStepThinking(profile, profile.StepCeilingTurn, small, disabled, revert); got != revert {
		t.Error("ceiling turn must always revert")
	}
	// Latest-batch semantics: an earlier turn's big batch does not pin the run
	// to thinking once a later batch is small again (cumulative still guards).
	recovered := []agent.ToolActivity{
		{Name: "grep", Turn: 1, OutputRunes: 2100},
		{Name: "read", Turn: 2, OutputRunes: 50},
	}
	if got := effortStepThinking(profile, 2, recovered, disabled, revert); got != disabled {
		t.Error("a small latest batch after an earlier big one stays disabled")
	}
	// Batch-awareness: within one turn the LARGEST result counts.
	batch := []agent.ToolActivity{
		{Name: "exec", Turn: 1, IsError: true, OutputRunes: 2100},
		{Name: "read", Turn: 1, OutputRunes: 2},
	}
	if got := effortStepThinking(profile, 1, batch, disabled, revert); got != revert {
		t.Error("an error/big result earlier in the batch must not be masked by a later tiny one")
	}
	bigThenTiny := []agent.ToolActivity{
		{Name: "grep", Turn: 1, OutputRunes: 2100},
		{Name: "read", Turn: 1, OutputRunes: 2},
	}
	if got := effortStepThinking(profile, 1, bigThenTiny, disabled, revert); got != revert {
		t.Error("batch max (not last call) must size the observation")
	}
	// Cumulative guard: many individually-small observations still revert once
	// the run total exceeds the budget.
	var cumulative []agent.ToolActivity
	for i := 0; i < 5; i++ {
		cumulative = append(cumulative, agent.ToolActivity{Name: "read", Turn: 1, OutputRunes: 1900})
	}
	if got := effortStepThinking(profile, 2, cumulative, disabled, revert); got != revert {
		t.Error("cumulative observation size must revert even when each call is small")
	}
}
