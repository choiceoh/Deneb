package toolport

import (
	"encoding/json"
	"testing"
)

// Gateway-authored notes ride the transcript under the user role so the model
// sees them next turn; the timeline must not show them as bubbles the user
// typed (2026-09-18 audit: 8 production sessions carried "[SYSTEM: 직전
// 어시스턴트 응답은 … 전송이 확인되지 않았습니다]" as apparent user messages).
func TestIsSyntheticSystemNote(t *testing.T) {
	yes := []string{
		"[SYSTEM: 직전 턴이 빈 응답으로 끝났습니다. 사용자에게 아래 안내를 이미 보냈고…]\n다시 요청해 주세요.",
		"  [SYSTEM: 직전 어시스턴트 응답은 phone-event 채널로 **전송이 확인되지 않았습니다**]",
		"**System:** the previous assistant turn was interrupted by the user while executing tools: exec.",
		"**System:** subagent completed. Synthesize the result below into your response for the user.",
		"[System: 턴 예산 정보 — 남은 턴 2/12. 서브에이전트가 작업 중입니다. 추가 작업이 없으면 턴을 종료하세요.]",
	}
	for _, s := range yes {
		if !IsSyntheticSystemNote(s) {
			t.Errorf("not recognised as a gateway note: %q", s)
		}
	}
	no := []string{
		"SYSTEM 프롬프트 좀 보여줘",
		"[관련 스킬] 아까 그거",
		"**중요:** 내일 회의 있어",
		"",
	}
	for _, s := range no {
		if IsSyntheticSystemNote(s) {
			t.Errorf("user text misclassified as a gateway note: %q", s)
		}
	}
}

func TestStripSyntheticSystemNotesForDisplay(t *testing.T) {
	msgs := []ChatMessage{
		NewTextChatMessage("user", "메일 보내줘", 1),
		NewTextChatMessage("assistant", "보냈습니다.", 2),
		NewTextChatMessage("user", "[SYSTEM: 직전 어시스턴트 응답은 client 채널로 **전송이 확인되지 않았습니다**]", 3),
		// Subagent completion notice riding the tool-results user message as a
		// text block: once the tool_result strip removes the raw output, only
		// the notice would remain — as a user bubble.
		{Role: "user", Timestamp: 4, Content: json.RawMessage(
			`[{"type":"tool_result","tool_use_id":"t1","content":"raw"},{"type":"text","text":"**System:** subagent completed. Synthesize the result below."}]`,
		)},
		NewTextChatMessage("user", "고마워", 5),
		// An assistant message that happens to start the same way is prose, not a note.
		NewTextChatMessage("assistant", "[SYSTEM: 이라고 적힌 로그는 게이트웨이 노트입니다.", 6),
	}
	out := StripSyntheticSystemNotesForDisplay(msgs)
	if len(out) != 4 {
		t.Fatalf("kept %d messages, want 4: %+v", len(out), out)
	}
	for _, m := range out {
		if m.Role == "user" && IsSyntheticSystemNote(m.TextContent()) {
			t.Errorf("gateway note survived the strip: %q", m.TextContent())
		}
	}
	if out[0].Timestamp != 1 || out[1].Timestamp != 2 || out[2].Timestamp != 5 || out[3].Timestamp != 6 {
		t.Errorf("order/selection wrong: %+v", out)
	}
	if len(msgs) != 6 {
		t.Fatal("input slice must not be mutated")
	}
}
