package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestIsInterimNarration(t *testing.T) {
	atThreshold := strings.Repeat("가", deliverableNarrationMaxRunes)
	underThreshold := strings.Repeat("나", deliverableNarrationMaxRunes-1)
	cases := []struct {
		name      string
		text      string
		toolCalls int
		want      bool
	}{
		{"short narration alongside tools", "위키 맥락 확보 완료. 이제 메일 읽을게요.", 6, true},
		{"empty text alongside tools", "", 3, true},
		{"terminal turn (no tools) is answer", "위키에 기록했습니다.", 0, false},
		{"long content with tools is answer (report saved to wiki)", atThreshold, 2, false},
		{"long content no tools is answer", atThreshold, 0, false},
		{"one rune under threshold with tools is narration", underThreshold, 1, true},
		{"CJK counted by rune not byte", "가나다라마바사", 1, true}, // 7 runes, 21 bytes
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isInterimNarration(c.text, c.toolCalls); got != c.want {
				t.Fatalf("isInterimNarration(%d runes, %d tools) = %v, want %v",
					utf8.RuneCountInString(c.text), c.toolCalls, got, c.want)
			}
		})
	}
}

func TestStripNarrationHead(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{
			// The 2026-06-10 prod leak: cron deliverable opened with the model's
			// self-talk before the --- divider and the report heading.
			name: "prod leak: narration + rule + report",
			text: "이제 분석 보고를 정리해.\n\n---\n\n## 📧 메일 분석: [해밀고흥솔라팜] 견적 요청\n\n본문입니다.",
			want: "## 📧 메일 분석: [해밀고흥솔라팜] 견적 요청\n\n본문입니다.",
		},
		{
			name: "narration directly before heading (no rule)",
			text: "이제 정리할게요.\n\n## 보고\n내용",
			want: "## 보고\n내용",
		},
		{
			name: "two narration paragraphs before rule",
			text: "좋아, 메일 확인 완료.\n\n이제 분석 보고를 정리해.\n\n---\n\n## 본문 제목\n내용",
			want: "## 본문 제목\n내용",
		},
		{
			name: "clean report starting with heading is untouched",
			text: "## LG전자 모듈 공급 협의\n\n- 항목 1\n- 항목 2",
			want: "## LG전자 모듈 공급 협의\n\n- 항목 1\n- 항목 2",
		},
		{
			name: "clean report starting with rule is untouched",
			text: "---\n\n## 제목\n내용",
			want: "---\n\n## 제목\n내용",
		},
		{
			name: "head with digits is content (counts are facts)",
			text: "신규 메일 1건을 분석했습니다.\n\n---\n\n## 상세",
			want: "신규 메일 1건을 분석했습니다.\n\n---\n\n## 상세",
		},
		{
			name: "head with colon label is content",
			text: "임박: 미팅 준비가 필요합니다.\n\n## 상세",
			want: "임박: 미팅 준비가 필요합니다.\n\n## 상세",
		},
		{
			name: "emoji-led head is a content marker",
			text: "📬 새 메일이 도착했습니다.\n\n---\n\n## 분석",
			want: "📬 새 메일이 도착했습니다.\n\n---\n\n## 분석",
		},
		{
			name: "head over the rune cap is content",
			text: strings.Repeat("가", deliverableNarrationHeadMaxRunes+1) + ".\n\n---\n\n## 제목",
			want: strings.Repeat("가", deliverableNarrationHeadMaxRunes+1) + ".\n\n---\n\n## 제목",
		},
		{
			name: "head without sentence punctuation is kept",
			text: "해밀고흥솔라팜 견적 분석\n\n---\n\n## 상세",
			want: "해밀고흥솔라팜 견적 분석\n\n---\n\n## 상세",
		},
		{
			name: "rule with empty body keeps everything",
			text: "이제 정리해.\n\n---",
			want: "이제 정리해.\n\n---",
		},
		{
			name: "plain prose answer without boundary is untouched",
			text: "오늘은 특이사항이 없습니다. 내일 일정은 그대로 유지됩니다.",
			want: "오늘은 특이사항이 없습니다. 내일 일정은 그대로 유지됩니다.",
		},
		{
			name: "narration then non-heading content paragraph is kept",
			text: "이제 정리해.\n\n- 항목 하나\n- 항목 둘",
			want: "이제 정리해.\n\n- 항목 하나\n- 항목 둘",
		},
		{
			name: "asterisk rule divider",
			text: "분석을 마쳤습니다.\n\n***\n\n## 결과\n내용",
			want: "## 결과\n내용",
		},
		{
			name: "empty text",
			text: "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripNarrationHead(c.text); got != c.want {
				t.Fatalf("stripNarrationHead(%q)\n got: %q\nwant: %q", c.text, got, c.want)
			}
		})
	}
}

func TestExtractThinkingText_Basic(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."},
		{Type: "tool_use", Name: "read"},
	}

	got := extractThinkingText(blocks)
	want := "사용자가 설정 파일을 수정하고 싶어합니다.\n먼저 파일을 읽어봐야겠습니다."
	if got != want {
		t.Errorf("unexpected text: %q", got)
	}
}

func TestExtractThinkingText_MultipleBlocks(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "thinking", Thinking: "first thinking block"},
		{Type: "text", Text: "some text"},
		{Type: "thinking", Thinking: "second thinking block — closer to tools"},
		{Type: "tool_use", Name: "exec"},
	}

	got := extractThinkingText(blocks)
	if got != "second thinking block — closer to tools" {
		t.Errorf("expected last thinking block, got: %q", got)
	}
}

// --- Stream idle watchdog tests ---

// makeStreamEvent creates a minimal SSE event for testing.
func makeStreamEvent(typ string) llm.StreamEvent {
	return llm.StreamEvent{Type: typ, Payload: json.RawMessage(`{}`)}
}

func TestConsumeStreamInto_IdleTimeout(t *testing.T) {
	// Channel that never sends — should trigger idle timeout.
	events := make(chan llm.StreamEvent)
	ctx := context.Background()
	result := &turnResult{}

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, 50*time.Millisecond, nil)
	if !errors.Is(err, ErrStreamIdle) {
		t.Fatalf("expected ErrStreamIdle, got: %v", err)
	}
}

func TestConsumeStreamInto_IdleResetOnEvent(t *testing.T) {
	// Events arrive just before the idle timeout, then stream closes.
	events := make(chan llm.StreamEvent, 3)
	ctx := context.Background()
	result := &turnResult{}

	// Send message_start, then close after a short delay.
	go func() {
		events <- makeStreamEvent("message_start")
		time.Sleep(30 * time.Millisecond)
		events <- makeStreamEvent("message_stop")
	}()

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, 100*time.Millisecond, nil)
	testutil.NoError(t, err)
}

func TestConsumeStreamInto_MalformedEventsSkipped(t *testing.T) {
	// Malformed events should be logged but not crash; valid events still processed.
	events := make(chan llm.StreamEvent, 10)
	ctx := context.Background()
	result := &turnResult{}

	go func() {
		// Valid message_start.
		startPayload, _ := json.Marshal(llm.MessageStart{})
		events <- llm.StreamEvent{Type: "message_start", Payload: startPayload}

		// Malformed content_block_start (bad JSON).
		events <- llm.StreamEvent{Type: "content_block_start", Payload: json.RawMessage(`{bad`)}

		// Malformed content_block_delta.
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: json.RawMessage(`not json`)}

		// Malformed message_delta.
		events <- llm.StreamEvent{Type: "message_delta", Payload: json.RawMessage(`///`)}

		// Valid message_stop.
		events <- llm.StreamEvent{Type: "message_stop", Payload: json.RawMessage(`{}`)}
	}()

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, -1, nil)
	testutil.NoError(t, err)
}

func TestConsumeStreamInto_DeltaIndexMismatch(t *testing.T) {
	// Delta with mismatched index should be dropped (not applied to current block).
	events := make(chan llm.StreamEvent, 10)
	ctx := context.Background()
	result := &turnResult{}

	go func() {
		startPayload, _ := json.Marshal(llm.MessageStart{})
		events <- llm.StreamEvent{Type: "message_start", Payload: startPayload}

		// Open block at index 0.
		cbsPayload, _ := json.Marshal(llm.ContentBlockStart{
			Index:        0,
			ContentBlock: llm.ContentBlock{Type: "text"},
		})
		events <- llm.StreamEvent{Type: "content_block_start", Payload: cbsPayload}

		// Valid delta for index 0.
		var cbd0Val llm.ContentBlockDelta
		cbd0Val.Index = 0
		cbd0Val.Delta.Type = "text_delta"
		cbd0Val.Delta.Text = "hello"
		cbd0, _ := json.Marshal(cbd0Val)
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: cbd0}

		// Mismatched delta for index 5 — should be dropped.
		var cbd5Val llm.ContentBlockDelta
		cbd5Val.Index = 5
		cbd5Val.Delta.Type = "text_delta"
		cbd5Val.Delta.Text = " SHOULD NOT APPEAR"
		cbd5, _ := json.Marshal(cbd5Val)
		events <- llm.StreamEvent{Type: "content_block_delta", Payload: cbd5}

		// Close block 0.
		cbStop, _ := json.Marshal(llm.ContentBlockStop{Index: 0})
		events <- llm.StreamEvent{Type: "content_block_stop", Payload: cbStop}

		events <- llm.StreamEvent{Type: "message_stop", Payload: json.RawMessage(`{}`)}
	}()

	err := consumeStreamInto(ctx, events, StreamHooks{}, result, -1, nil)
	testutil.NoError(t, err)

	if result.text != "hello" {
		t.Errorf("text = %q, want %q (mismatched delta should be dropped)", result.text, "hello")
	}
}
