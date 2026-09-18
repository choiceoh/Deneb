package chatport

import (
	"encoding/json"
	"strings"
	"testing"
)

func excerptToolTurn() ChatMessage {
	return ChatMessage{Role: "assistant", Content: json.RawMessage(
		`[{"type":"thinking","thinking":"먼저 위키를 봐야겠다","signature":"AAAABBBBCCCCDDDD"},` +
			`{"type":"text","text":"확인해 볼게요"},` +
			`{"type":"tool_use","name":"morning_letter","input":{"date":"2026-08-26"}}]`,
	)}
}

// ExcerptText is the model-facing rendering of a past turn: it keeps what the
// turn SAID and WHICH tool it called, and drops the two parts that read to a
// model as a document to keep writing — the assistant's own reasoning and the
// tool call's raw JSON input (incident 2026-09-17, stream_0043).
func TestExcerptTextDropsReasoningAndToolInput(t *testing.T) {
	msg := excerptToolTurn()
	got := msg.ExcerptText()
	if !strings.Contains(got, "확인해 볼게요") {
		t.Errorf("ExcerptText dropped the spoken text: %q", got)
	}
	if !strings.Contains(got, "[도구 morning_letter]") {
		t.Errorf("ExcerptText dropped the tool name: %q", got)
	}
	if strings.Contains(got, "2026-08-26") || strings.Contains(got, "{") {
		t.Errorf("ExcerptText leaked the tool input JSON: %q", got)
	}
	if strings.Contains(got, "위키를 봐야겠다") {
		t.Errorf("ExcerptText fed the assistant's reasoning back: %q", got)
	}
	if strings.Contains(got, "AAAABBBB") {
		t.Errorf("ExcerptText leaked the signature: %q", got)
	}
	// Search MATCHING is unchanged: SearchableText still carries the prose and
	// the argument so "does this message MENTION x" keeps its recall.
	if s := msg.SearchableText(); !strings.Contains(s, "위키를 봐야겟다") && !strings.Contains(s, "위키를 봐야겠다") {
		t.Errorf("SearchableText must keep matching thinking prose: %q", s)
	}
}

// A thinking-only turn renders as nothing, so history renderers skip the row
// instead of printing the reasoning (or a blank line) as a transcript entry.
func TestExcerptTextIsEmptyForThinkingOnlyTurn(t *testing.T) {
	msg := ChatMessage{Role: "assistant", Content: json.RawMessage(
		`[{"type":"thinking","thinking":"조용히 생각만","signature":"SIG"}]`,
	)}
	if got := msg.ExcerptText(); got != "" {
		t.Errorf("ExcerptText = %q, want empty for a thinking-only turn", got)
	}
	if got := msg.SearchableText(); !strings.Contains(got, "조용히 생각만") {
		t.Errorf("SearchableText must still match the reasoning: %q", got)
	}
}

// Plain string content and tool results render identically to SearchableText —
// the two renderers must not disagree about ordinary rows.
func TestExcerptTextMatchesSearchableTextForPlainAndToolResultContent(t *testing.T) {
	plain := NewTextChatMessage("user", "안녕하세요", 1)
	if plain.ExcerptText() != plain.SearchableText() || plain.ExcerptText() != "안녕하세요" {
		t.Errorf("plain content diverged: %q vs %q", plain.ExcerptText(), plain.SearchableText())
	}
	result := ChatMessage{Role: "tool", Content: json.RawMessage(
		`[{"type":"tool_result","tool_use_id":"t1","content":"3건 찾았습니다"}]`,
	)}
	if result.ExcerptText() != result.SearchableText() || !strings.Contains(result.ExcerptText(), "3건 찾았습니다") {
		t.Errorf("tool_result content diverged: %q vs %q", result.ExcerptText(), result.SearchableText())
	}
	var empty ChatMessage
	if got := empty.ExcerptText(); got != "" {
		t.Errorf("empty message rendered %q", got)
	}
}
