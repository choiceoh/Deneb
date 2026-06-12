package chat

import (
	"errors"
	"strings"
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
		off, reason := decideThinkingOff(c.params, nil)
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
	origMod := func(int, []agent.ToolActivity) *llm.ThinkingConfig { return nil }
	cfg := agent.AgentConfig{Model: "deepseek-v4-flash", Thinking: orig, ThinkingModulator: origMod}

	route, decision := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, nil, "thinking", nil)
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
	if got := cfg.ThinkingModulator(effortStepCeilingTurn, nil); got == nil || got.Type != "enabled" || got.BudgetTokens != 4096 {
		t.Fatalf("turn %d must revert to the session thinking, got %+v", effortStepCeilingTurn, got)
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
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, nil, "thinking", nil); r != nil || d != "" {
		t.Error("flag off must not route and reports no decision")
	}
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "안녕"}, nil, "", nil); r != nil || d != "" {
		t.Error("empty capability kwarg must not route")
	}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "이 코드 분석해줘"}, nil, "thinking", nil); r != nil || d != "kept:hard-signal:분석" {
		t.Errorf("hard message must report kept decision, got %q", d)
	}
	if cfg.Thinking != nil {
		t.Error("no-route paths must leave Thinking untouched")
	}
	// force mode (eval baseline): routes even hard messages — but never
	// automation/attachment-protected runs.
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "force")
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "이 코드 분석해줘"}, nil, "thinking", nil); r == nil || d != "routed:forced" {
		t.Errorf("force mode must route everything eligible, got %q", d)
	}
	if r, d := applyEffortRouter(&cfg, RunParams{Message: "메일 분석", SessionKey: "cron:mail:1"}, nil, "thinking", nil); r != nil || d != "kept:automation" {
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

// TestRecentContextHeavy + ack exception: a short follow-up in a heavy thread
// keeps thinking (Ares #3 — router sees h_t), pure acks stay routable.
func TestContextHeavyRouting(t *testing.T) {
	heavy := []llm.Message{
		llm.NewTextMessage("user", "이 코드 분석해줘"),
		{Role: "assistant", Content: []byte(`[{"type":"tool_use","id":"t1","name":"read","input":{}}]`)},
		{Role: "user", Content: []byte(`[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]`)},
		llm.NewTextMessage("assistant", "분석 결과입니다..."),
		llm.NewTextMessage("user", "그거 마저 해줘"),
	}
	if off, reason := decideThinkingOff(RunParams{Message: "그거 마저 해줘"}, heavy); off || reason != "context-heavy" {
		t.Errorf("short steer in heavy thread must keep thinking, got off=%v reason=%s", off, reason)
	}
	if off, _ := decideThinkingOff(RunParams{Message: "고마워!"}, heavy); !off {
		t.Error("pure ack stays routable even in a heavy thread")
	}
	for _, neg := range []string{"안되네", "여전히 안되네요", "안 좋아", "반응이 없네"} {
		if off, reason := decideThinkingOff(RunParams{Message: neg}, heavy); off {
			t.Errorf("negative report %q must NOT be an ack (reason=%s)", neg, reason)
		}
	}
	light := []llm.Message{llm.NewTextMessage("user", "안녕"), llm.NewTextMessage("assistant", "안녕하세요!"), llm.NewTextMessage("user", "잘 지냈어?")}
	if off, _ := decideThinkingOff(RunParams{Message: "잘 지냈어?"}, light); !off {
		t.Error("light history must not block routing")
	}
	// Plain-STRING assistant content (proactive cards, legacy persists) must
	// also count as heavy — ContentToBlocks normalizes it to a text block.
	card := []llm.Message{
		llm.NewTextMessage("assistant", strings.Repeat("메일 분석 ", 400)),
		llm.NewTextMessage("user", "그거 처리해줘"),
	}
	if off, reason := decideThinkingOff(RunParams{Message: "그거 처리해줘"}, card); off {
		t.Errorf("string-persisted long assistant card must mark context heavy (reason=%s)", reason)
	}
}

// TestEffortStepThinking covers the per-step policy e_t = f(turn, o_t), with
// o_t coming from the executor's run-scoped ToolActivity records (no message
// re-parsing — session history never reaches this function by construction).
func TestEffortStepThinking(t *testing.T) {
	disabled := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: "thinking"}
	revert := &llm.ThinkingConfig{Type: "enabled"}
	small := []agent.ToolActivity{{Name: "read", Turn: 1, OutputRunes: 2}}
	if got := effortStepThinking(0, nil, disabled, revert); got != disabled {
		t.Error("turn 0 must stay disabled")
	}
	if got := effortStepThinking(1, small, disabled, revert); got != disabled {
		t.Error("turn 1 with a small clean observation stays disabled")
	}
	big := []agent.ToolActivity{{Name: "read", Turn: 1, OutputRunes: 2100}}
	if got := effortStepThinking(1, big, disabled, revert); got != revert {
		t.Error("a big observation must revert to thinking")
	}
	errRes := []agent.ToolActivity{{Name: "exec", Turn: 1, IsError: true, OutputRunes: 4}}
	if got := effortStepThinking(1, errRes, disabled, revert); got != revert {
		t.Error("an error observation must revert to thinking")
	}
	if got := effortStepThinking(effortStepCeilingTurn, small, disabled, revert); got != revert {
		t.Error("ceiling turn must always revert")
	}
	// Latest-batch semantics: an earlier turn's big batch does not pin the
	// run to thinking once a later batch is small again (cumulative still
	// guards the total).
	recovered := []agent.ToolActivity{
		{Name: "grep", Turn: 1, OutputRunes: 2100},
		{Name: "read", Turn: 2, OutputRunes: 50},
	}
	if got := effortStepThinking(2, recovered, disabled, revert); got != disabled {
		t.Error("a small latest batch after an earlier big one stays disabled")
	}
	// Batch-awareness: within one turn the LARGEST result counts — a big or
	// failed call cannot hide behind a later tiny one.
	batch := []agent.ToolActivity{
		{Name: "exec", Turn: 1, IsError: true, OutputRunes: 2100},
		{Name: "read", Turn: 1, OutputRunes: 2},
	}
	if got := effortStepThinking(1, batch, disabled, revert); got != revert {
		t.Error("an error/big result earlier in the batch must not be masked by a later tiny one")
	}
	bigThenTiny := []agent.ToolActivity{
		{Name: "grep", Turn: 1, OutputRunes: 2100},
		{Name: "read", Turn: 1, OutputRunes: 2},
	}
	if got := effortStepThinking(1, bigThenTiny, disabled, revert); got != revert {
		t.Error("batch max (not last call) must size the observation")
	}
	// Cumulative guard: many individually-small observations still revert
	// once the run total exceeds the budget.
	var cumulative []agent.ToolActivity
	for i := 0; i < 5; i++ {
		cumulative = append(cumulative, agent.ToolActivity{Name: "read", Turn: 1, OutputRunes: 1900})
	}
	if got := effortStepThinking(2, cumulative, disabled, revert); got != revert {
		t.Error("cumulative observation size must revert even when each call is small")
	}
}
