package streaming

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestTruncateForBroadcast(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"under limit", "hello", 10, "hello"},
		{"at limit", "hello", 5, "hello"},
		{"over limit", "hello world", 5, "hello… (이하 생략)"},
		{"empty string", "", 5, ""},
		// Korean "안녕하세요" is 3 bytes per rune (UTF-8).
		// maxLen=7 lands inside the 3rd rune; must retreat to byte 6
		// to avoid leaving a mid-rune byte in the output.
		{"utf8 mid-rune cut", "안녕하세요", 7, "안녕… (이하 생략)"},
		{"utf8 rune boundary", "안녕하세요", 6, "안녕… (이하 생략)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForBroadcast(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForBroadcast(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestStreamBroadcasterEmitThinkingThrottles(t *testing.T) {
	var events []string
	var payloads []string
	sb := NewBroadcaster(func(event string, data []byte) int {
		events = append(events, event)
		payloads = append(payloads, string(data))
		return 1
	}, "s1", "r1")

	// A burst of reasoning deltas inside one throttle window → one frame.
	for range 10 {
		sb.EmitThinking("스텝 ")
	}
	if len(events) != 1 || events[0] != EventThinking {
		t.Fatalf("events = %v, want exactly one %q", events, EventThinking)
	}

	// Aging the last-emit timestamp past the window re-arms the throttle, and
	// the next frame condenses every accumulated delta (throttled ones
	// included) into a whitespace-collapsed preview.
	sb.lastThinkingNs.Store(sb.lastThinkingNs.Load() - int64(thinkingThrottle) - 1)
	sb.EmitThinking("메일 발신인\n이력 대조")
	if len(events) != 2 {
		t.Fatalf("after window: events = %v, want 2 frames", events)
	}
	if !strings.Contains(payloads[1], `"preview":"스텝 스텝 스텝 스텝 스텝 스텝 스텝 스텝 스텝 스텝 메일 발신인 이력 대조"`) {
		t.Fatalf("second frame should carry the collapsed preview, got %s", payloads[1])
	}
}

func TestThinkingPreviewTailTruncation(t *testing.T) {
	sb := NewBroadcaster(func(string, []byte) int { return 1 }, "s1", "r1")

	// Empty/whitespace-only accumulation → no preview.
	sb.appendThinking("  \n\t ")
	if got := sb.thinkingPreview(); got != "" {
		t.Fatalf("whitespace-only preview = %q, want empty", got)
	}

	// Below the minimum readable length (the first pulse fires on the very
	// first delta) → still no preview, just the bare liveness signal.
	sb.appendThinking("The")
	if got := sb.thinkingPreview(); got != "" {
		t.Fatalf("sub-minimum preview = %q, want empty", got)
	}

	// Long accumulation without a terminator surfaces the live fragment from its
	// head with only a trailing ellipsis (never a leading one), so the thought
	// starts at its beginning. Korean runes must not split mid-character.
	for range 40 {
		sb.appendThinking("과거 거래 조건을 다시 확인한다 ")
	}
	got := sb.thinkingPreview()
	runes := []rune(got)
	if len(runes) == 0 || runes[0] == '…' {
		t.Fatalf("preview should not lead with an ellipsis, got %q", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("overflowing preview should end with a trailing ellipsis, got %q", got)
	}
	if len(runes) != thinkingPreviewRunes+1 {
		t.Fatalf("preview length = %d runes, want %d", len(runes), thinkingPreviewRunes+1)
	}
	if !strings.Contains(got, "확인한다") {
		t.Fatalf("preview should carry the accumulated reasoning text, got %q", got)
	}
}

// TestCleanThinkingPreviewSentenceExtraction pins the chip-line cleanup against
// a real DeepSeek-V4 reasoning sample: Korean, wrapped in parentheses, verbose
// multi-sentence thoughts. The chip must surface the latest complete sentence
// from its head — no wrapper parens, no leading ellipsis, opener dropped.
func TestCleanThinkingPreviewSentenceExtraction(t *testing.T) {
	// Verbatim shape from the live engine (truncated middle for brevity).
	raw := "(아, 탑솔라의 김 부장님에게서 온 이메일에 대해 문의하시는군요. " +
		"보통 때와 다른 어조에 계좌 변경 요청까지 있다니... 상당히 의심스러운 상황입니다.)\n\n" +
		"(우선 발신자 주소부터 확인해야 합니다. 실제 업무용 도메인과 일치하는지 꼭 살펴보셨으면 좋겠습니다. " +
		"그리고 제목이나 본문에서 급박함을 강조하며 개인정보나 금융 정보를 바로 제공하게 만드는 " +
		"전형적인 사회공학 기법이 사용되지는 않았는지도 중요합니다.)"

	got := cleanThinkingPreview(raw)

	if strings.ContainsAny(got, "()（）[]「」*#`>") {
		t.Errorf("preview should strip wrapper/markdown noise, got %q", got)
	}
	if strings.HasPrefix(got, "…") {
		t.Errorf("preview must not lead with an ellipsis, got %q", got)
	}
	if strings.HasPrefix(got, "그리고") {
		t.Errorf("preview should drop the leading discourse marker, got %q", got)
	}
	// The latest sentence ("그리고 제목이나 본문에서 …") surfaces from its head
	// (with 그리고 stripped), not the earlier 발신자/도메인 ones.
	if !strings.HasPrefix(got, "제목이나 본문에서") {
		t.Errorf("preview should surface the latest sentence head, got %q", got)
	}
	if r := []rune(got); len(r) > thinkingPreviewRunes+1 {
		t.Errorf("preview overflowed the chip cap: %d runes (%q)", len(r), got)
	}
}

func TestCleanThinkingPreviewEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "  \n\t ", ""},
		{"below minimum", "발신자", ""},
		{
			// No terminator yet → show the live fragment from its head, whole.
			name: "in-progress fragment",
			in:   "발신자 도메인을 면밀히 대조하는 중",
			want: "발신자 도메인을 면밀히 대조하는 중",
		},
		{
			// Latest complete sentence wins over a short trailing blip.
			name: "latest complete sentence",
			in:   "발신자 주소를 확인한다. 도메인을 대조한다. 음...",
			want: "도메인을 대조한다",
		},
		{
			// Leading interjection + wrapper parens both stripped.
			name: "opener and parens",
			in:   "(음, 계좌 변경은 전형적인 BEC 수법입니다.)",
			want: "계좌 변경은 전형적인 BEC 수법입니다",
		},
		{
			// English reasoning splits on ". " just the same.
			name: "english sentences",
			in:   "Let me check the sender. The domain looks spoofed.",
			want: "The domain looks spoofed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanThinkingPreview(tt.in); got != tt.want {
				t.Errorf("cleanThinkingPreview(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStreamBroadcasterEmitDelta(t *testing.T) {
	t.Run("skips empty text", func(t *testing.T) {
		called := false
		sb := NewBroadcaster(func(event string, data []byte) int {
			called = true
			return 0
		}, "s1", "r1")
		sb.EmitDelta("")
		if called {
			t.Error("should not broadcast empty delta")
		}
	})

	t.Run("broadcasts non-empty text", func(t *testing.T) {
		var captured struct {
			event string
			data  []byte
		}
		sb := NewBroadcaster(func(event string, data []byte) int {
			captured.event = event
			captured.data = data
			return 1
		}, "s1", "r1")
		sb.EmitDelta("hello")

		if captured.event != EventDelta {
			t.Errorf("event = %q, want %q", captured.event, EventDelta)
		}

		var msg map[string]any
		json.Unmarshal(captured.data, &msg)
		payload := msg["payload"].(map[string]any)
		if payload["delta"] != "hello" {
			t.Errorf("delta = %v, want %q", payload["delta"], "hello")
		}
		if payload["sessionKey"] != "s1" {
			t.Errorf("sessionKey = %v, want %q", payload["sessionKey"], "s1")
		}
		if payload["clientRunId"] != "r1" {
			t.Errorf("clientRunId = %v, want %q", payload["clientRunId"], "r1")
		}
	})
}

func TestStreamBroadcasterEvents(t *testing.T) {
	var mu sync.Mutex
	var events []struct {
		event string
		data  map[string]any
	}

	sb := NewBroadcaster(func(event string, data []byte) int {
		var parsed map[string]any
		json.Unmarshal(data, &parsed)
		mu.Lock()
		events = append(events, struct {
			event string
			data  map[string]any
		}{event, parsed})
		mu.Unlock()
		return 1
	}, "sess", "run")

	sb.EmitStarted()
	sb.EmitDelta("chunk1")
	sb.EmitToolStart("read", "t1", "")
	sb.EmitToolResult("read", "t1", "file content", false)
	sb.EmitDelta("chunk2")
	sb.EmitComplete("final", llm.TokenUsage{InputTokens: 100, OutputTokens: 50})

	if len(events) != 6 {
		t.Fatalf("got %d, want 6 events", len(events))
	}

	// Verify event types.
	wantEvents := []string{EventChat, EventDelta, EventTool, EventTool, EventDelta, EventChat}
	for i, want := range wantEvents {
		if events[i].event != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i].event, want)
		}
	}

	// Verify seq increments.
	for i, ev := range events {
		payload := ev.data["payload"].(map[string]any)
		seq := payload["seq"].(float64)
		if int(seq) != i+1 {
			t.Errorf("event[%d] seq = %v, want %d", i, seq, i+1)
		}
	}
}

func TestStreamBroadcasterToolResult(t *testing.T) {
	var captured map[string]any
	sb := NewBroadcaster(func(event string, data []byte) int {
		json.Unmarshal(data, &captured)
		return 1
	}, "s1", "r1")

	sb.EmitToolResult("exec", "tool-id", "error message", true)

	payload := captured["payload"].(map[string]any)
	if payload["state"] != "completed" {
		t.Errorf("state = %v, want %q", payload["state"], "completed")
	}
	if payload["tool"] != "exec" {
		t.Errorf("tool = %v, want %q", payload["tool"], "exec")
	}
	if payload["toolUseId"] != "tool-id" {
		t.Errorf("toolUseId = %v, want %q", payload["toolUseId"], "tool-id")
	}
	if payload["isError"] != true {
		t.Errorf("isError = %v, want true", payload["isError"])
	}
}

func TestStreamBroadcasterError(t *testing.T) {
	var captured map[string]any
	sb := NewBroadcaster(func(event string, data []byte) int {
		json.Unmarshal(data, &captured)
		return 1
	}, "s1", "r1")

	sb.EmitError("something failed")

	payload := captured["payload"].(map[string]any)
	if payload["state"] != "error" {
		t.Errorf("state = %v, want %q", payload["state"], "error")
	}
	if payload["error"] != "something failed" {
		t.Errorf("error = %v, want %q", payload["error"], "something failed")
	}
}

func TestStreamBroadcasterAborted(t *testing.T) {
	var captured map[string]any
	sb := NewBroadcaster(func(event string, data []byte) int {
		json.Unmarshal(data, &captured)
		return 1
	}, "s1", "r1")

	sb.EmitAborted("partial text")

	payload := captured["payload"].(map[string]any)
	if payload["state"] != "aborted" {
		t.Errorf("state = %v, want %q", payload["state"], "aborted")
	}
	if payload["text"] != "partial text" {
		t.Errorf("text = %v, want %q", payload["text"], "partial text")
	}
}
