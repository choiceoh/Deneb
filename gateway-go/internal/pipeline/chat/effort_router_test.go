package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

// TestDecideThinkingOff exercises the false-easy-averse heuristic: only
// obviously-simple short conversational messages route to non-thinking.
func TestDecideThinkingOff(t *testing.T) {
	cases := []struct {
		msg     string
		wantOff bool
		name    string
	}{
		{"안녕", true, "greeting"},
		{"고마워!", true, "thanks"},
		{"오늘 일정 뭐야?", true, "short tool-ish query"},
		{"응 그걸로 해줘", true, "short ack"},
		{"", false, "empty (cron/automation keeps thinking)"},
		{"이 코드 분석해줘", false, "hard signal: 분석"},
		{"내일 회의 계획 세워줘", false, "hard signal: 계획"},
		{"왜 이렇게 됐지?", false, "hard signal: 왜"},
		{"3 곱하기 47 계산해줘", false, "hard signal: 계산"},
		{"buggy code fix please", false, "hard signal: english"},
		{"이 함수에서 에러가 나는데 봐줘", false, "hard signal: 에러"},
		{"어제 회의록이랑 오늘 메일 내용 종합해서 핵심 흐름이 어떻게 이어지는지 보고서 형태로 만들어줘 그리고 빠진 부분 짚어줘", false, "long"},
		{"```\nprint(1)\n```", false, "code fence"},
	}
	for _, c := range cases {
		off, reason := decideThinkingOff(c.msg)
		if off != c.wantOff {
			t.Errorf("%s: decideThinkingOff(%q) = %v (reason=%s), want %v", c.name, c.msg, off, reason, c.wantOff)
		}
	}
}

// TestSupportsThinkingToggle gates the router to dual-mode template models.
func TestSupportsThinkingToggle(t *testing.T) {
	yes := []string{"deepseek-v4-flash", "DeepSeek-V4-Flash", "vllm/deepseek-v4-flash", "deepseek_v4"}
	no := []string{"step3p7", "qwen3.6-35b-a3b", "claude-opus-4-6", "mimo-v2.5-pro", ""}
	for _, m := range yes {
		if !supportsThinkingToggle(m) {
			t.Errorf("supportsThinkingToggle(%q) = false, want true", m)
		}
	}
	for _, m := range no {
		if supportsThinkingToggle(m) {
			t.Errorf("supportsThinkingToggle(%q) = true, want false", m)
		}
	}
}

// TestEffortOverrideLifecycle covers apply-detect-strip used by the
// escalation path in runAgentWithFallback.
func TestEffortOverrideLifecycle(t *testing.T) {
	cfg := agent.AgentConfig{}
	if effortRouterApplied(&cfg) {
		t.Fatal("fresh config must not report applied")
	}
	cfg.ExtraBody = effortOverrideBody()
	if !effortRouterApplied(&cfg) {
		t.Fatal("override body must report applied")
	}
	ctk, ok := cfg.ExtraBody["chat_template_kwargs"].(map[string]any)
	if !ok || ctk["thinking"] != false {
		t.Fatalf("override body malformed: %#v", cfg.ExtraBody)
	}
	stripEffortOverride(&cfg)
	if effortRouterApplied(&cfg) || cfg.ExtraBody != nil {
		t.Fatalf("strip must clear the override, got %#v", cfg.ExtraBody)
	}
}

// TestAdaptiveEffortEnabled checks the env opt-in parsing.
func TestAdaptiveEffortEnabled(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "")
	if adaptiveEffortEnabled() {
		t.Error("default (unset) must be disabled")
	}
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	if !adaptiveEffortEnabled() {
		t.Error("'1' must enable")
	}
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "off")
	if adaptiveEffortEnabled() {
		t.Error("'off' must disable")
	}
}
