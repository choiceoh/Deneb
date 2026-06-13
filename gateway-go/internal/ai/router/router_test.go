package router

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// enabled returns the default profile with a toggle, the shape the resolver
// produces for a dual-mode model.
func enabled() Profile {
	p := DefaultProfile()
	p.Enabled = true
	p.ToggleKwarg = "thinking"
	return p
}

// TestDecide exercises the false-easy-averse heuristic: only obviously-simple
// short conversational messages route to non-thinking.
func TestDecide(t *testing.T) {
	p := enabled()
	cases := []struct {
		req     Request
		wantOff bool
		name    string
	}{
		{Request{Message: "안녕"}, true, "greeting"},
		{Request{Message: "고마워!"}, true, "thanks"},
		{Request{Message: "오늘 일정 뭐야?"}, true, "short tool-ish query"},
		{Request{Message: "응 그걸로 해줘"}, true, "short ack"},
		{Request{Message: ""}, false, "empty"},
		{Request{Message: "안녕", IsAutomation: true}, false, "automation keeps thinking"},
		{Request{Message: "이거 봐줘", HasAttachments: true}, false, "attachment caption keeps thinking"},
		{Request{Message: "이 코드 분석해줘"}, false, "hard signal: 분석"},
		{Request{Message: "내일 회의 계획 세워줘"}, false, "hard signal: 계획"},
		{Request{Message: "왜 이렇게 됐지?"}, false, "hard signal: 왜"},
		{Request{Message: "3 곱하기 47 계산해줘"}, false, "hard signal: 계산"},
		{Request{Message: "buggy code fix please"}, false, "hard signal: english"},
		{Request{Message: "어제 회의록이랑 오늘 메일 내용 종합해서 핵심 흐름이 어떻게 이어지는지 보고서 형태로 만들어줘 그리고 빠진 부분 짚어줘"}, false, "long"},
		{Request{Message: "```\nprint(1)\n```"}, false, "code fence"},
	}
	for _, c := range cases {
		got := Decide(p, c.req)
		if got.ThinkingOff != c.wantOff {
			t.Errorf("%s: Decide(%q) off=%v (reason=%s), want %v", c.name, c.req.Message, got.ThinkingOff, got.Reason, c.wantOff)
		}
	}
}

// TestDecideReasonTags pins the stable reason strings the agentlog scorecard
// parses — renaming one silently breaks calibration aggregation.
func TestDecideReasonTags(t *testing.T) {
	p := enabled()
	for _, c := range []struct {
		req    Request
		reason string
	}{
		{Request{Message: "안녕"}, "short-conversational"},
		{Request{Message: ""}, "empty"},
		{Request{Message: "안녕", IsAutomation: true}, "automation"},
		{Request{Message: "x", HasAttachments: true}, "attachments"},
		{Request{Message: strings.Repeat("가", 200)}, "long"},
		{Request{Message: "a\nb\nc"}, "structured"},
		{Request{Message: "이 코드 분석해줘"}, "hard-signal:분석"},
	} {
		if got := Decide(p, c.req); got.Reason != c.reason {
			t.Errorf("Decide(%q).Reason = %q, want %q", c.req.Message, got.Reason, c.reason)
		}
	}
}

// TestContextHeavyRouting: a short follow-up in a heavy thread keeps thinking
// (Ares #3 — router sees h_t), pure acks stay routable.
func TestContextHeavyRouting(t *testing.T) {
	p := enabled()
	heavy := []llm.Message{
		llm.NewTextMessage("user", "이 코드 분석해줘"),
		{Role: "assistant", Content: []byte(`[{"type":"tool_use","id":"t1","name":"read","input":{}}]`)},
		{Role: "user", Content: []byte(`[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]`)},
		llm.NewTextMessage("assistant", "분석 결과입니다..."),
		llm.NewTextMessage("user", "그거 마저 해줘"),
	}
	if got := Decide(p, Request{Message: "그거 마저 해줘", History: heavy}); got.ThinkingOff || got.Reason != "context-heavy" {
		t.Errorf("short steer in heavy thread must keep thinking, got off=%v reason=%s", got.ThinkingOff, got.Reason)
	}
	if got := Decide(p, Request{Message: "고마워!", History: heavy}); !got.ThinkingOff {
		t.Error("pure ack stays routable even in a heavy thread")
	}
	for _, neg := range []string{"안되네", "여전히 안되네요", "안 좋아", "반응이 없네"} {
		if got := Decide(p, Request{Message: neg, History: heavy}); got.ThinkingOff {
			t.Errorf("negative report %q must NOT be an ack (reason=%s)", neg, got.Reason)
		}
	}
	light := []llm.Message{llm.NewTextMessage("user", "안녕"), llm.NewTextMessage("assistant", "안녕하세요!"), llm.NewTextMessage("user", "잘 지냈어?")}
	if got := Decide(p, Request{Message: "잘 지냈어?", History: light}); !got.ThinkingOff {
		t.Error("light history must not block routing")
	}
	// Plain-STRING assistant content (proactive cards, legacy persists) must
	// also count as heavy — ContentToBlocks normalizes it to a text block.
	card := []llm.Message{
		llm.NewTextMessage("assistant", strings.Repeat("메일 분석 ", 400)),
		llm.NewTextMessage("user", "그거 처리해줘"),
	}
	if got := Decide(p, Request{Message: "그거 처리해줘", History: card}); got.ThinkingOff {
		t.Errorf("string-persisted long assistant card must mark context heavy (reason=%s)", got.Reason)
	}
}

// TestDefaultProfileConstants pins the shipped tuning values so an accidental
// edit to DefaultProfile (which the active model resolves to verbatim) fails
// loudly rather than silently shifting production routing.
func TestDefaultProfileConstants(t *testing.T) {
	p := DefaultProfile()
	if p.Enabled || p.ToggleKwarg != "" {
		t.Error("DefaultProfile must be inert until the resolver fills the toggle")
	}
	if p.MaxSimpleRunes != 140 || p.StepCeilingTurn != 3 || p.ObservationRunes != 2000 ||
		p.CumulativeRunes != 8000 || p.HeavyHistoryRunes != 1500 {
		t.Errorf("DefaultProfile thresholds drifted: %+v", p)
	}
	if len(p.HardSignals) == 0 || len(p.PureAcks) == 0 {
		t.Error("DefaultProfile must carry the hard-signal and ack vocabularies")
	}
}
