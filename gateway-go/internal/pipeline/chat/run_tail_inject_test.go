package chat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func messageText(t *testing.T, msg llm.Message) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return s
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	t.Fatalf("unparseable message content: %s", string(msg.Content))
	return ""
}

func TestInjectTailAdditions_StringContent(t *testing.T) {
	messages := []llm.Message{
		llm.NewTextMessage("user", "이전 질문"),
		llm.NewTextMessage("assistant", "이전 답"),
		llm.NewTextMessage("user", "현재 질문"),
	}
	out, ok := injectTailAdditions(messages, []string{"<recall-context>증거</recall-context>"})
	if !ok {
		t.Fatal("expected injection to succeed")
	}
	got := messageText(t, out[2])
	if got != "현재 질문\n\n<recall-context>증거</recall-context>" {
		t.Fatalf("unexpected injected content: %q", got)
	}
	// History messages untouched; original slice not mutated.
	if messageText(t, out[0]) != "이전 질문" || messageText(t, out[1]) != "이전 답" {
		t.Fatal("history messages must be untouched")
	}
	if messageText(t, messages[2]) != "현재 질문" {
		t.Fatal("input slice must not be mutated (wire-only contract)")
	}
}

func TestInjectTailAdditions_LastUserNotLastMessage(t *testing.T) {
	// Agent-loop shape mid-run: tool results follow the user message.
	messages := []llm.Message{
		llm.NewTextMessage("user", "질문"),
		llm.NewTextMessage("assistant", "도구 호출"),
		llm.NewTextMessage("tool", "도구 결과"),
	}
	out, ok := injectTailAdditions(messages, []string{"A", "B"})
	if !ok {
		t.Fatal("expected injection to succeed")
	}
	if got := messageText(t, out[0]); got != "질문\n\nA\n\nB" {
		t.Fatalf("expected both additions appended in order, got %q", got)
	}
	if messageText(t, out[2]) != "도구 결과" {
		t.Fatal("non-user tail must be untouched")
	}
}

func TestInjectTailAdditions_BlockContent(t *testing.T) {
	blocks := []llm.ContentBlock{
		{Type: "text", Text: "사진 봐줘"},
		{Type: "image", Source: &llm.ImageSource{Type: "base64", MediaType: "image/png", Data: "xxx"}},
	}
	messages := []llm.Message{llm.NewBlockMessage("user", blocks)}
	out, ok := injectTailAdditions(messages, []string{"증거"})
	if !ok {
		t.Fatal("expected injection to succeed")
	}
	var outBlocks []llm.ContentBlock
	if err := json.Unmarshal(out[0].Content, &outBlocks); err != nil {
		t.Fatalf("expected block content: %v", err)
	}
	if len(outBlocks) != 3 {
		t.Fatalf("expected appended text block, got %d blocks", len(outBlocks))
	}
	if outBlocks[1].Type != "image" {
		t.Fatal("image block must keep its position")
	}
	if outBlocks[2].Type != "text" || outBlocks[2].Text != "증거" {
		t.Fatalf("unexpected appended block: %+v", outBlocks[2])
	}
}

func TestInjectTailAdditions_NoUserMessage(t *testing.T) {
	messages := []llm.Message{llm.NewTextMessage("assistant", "고아 메시지")}
	out, ok := injectTailAdditions(messages, []string{"증거"})
	if ok {
		t.Fatal("expected fallback signal when no user message exists")
	}
	if messageText(t, out[0]) != "고아 메시지" {
		t.Fatal("messages must be returned unchanged on fallback")
	}
}

func TestInjectTailAdditions_NothingToAdd(t *testing.T) {
	messages := []llm.Message{llm.NewTextMessage("user", "질문")}
	out, ok := injectTailAdditions(messages, nil)
	if !ok {
		t.Fatal("no additions is trivially done — no fallback")
	}
	if messageText(t, out[0]) != "질문" {
		t.Fatal("messages must be unchanged")
	}
}

func TestBuildTailAdditions(t *testing.T) {
	// Interactive turn with recall: recall first, then the directive.
	adds := buildTailAdditions(RunParams{AutoDeliveredOutput: true}, "recall-블록", "")
	if len(adds) != 2 || adds[0] != "recall-블록" ||
		!strings.Contains(adds[1], "전달 정책") || !strings.Contains(adds[1], "message") {
		t.Fatalf("unexpected additions: %#v", adds)
	}
	// The directive must read true for an interactive chat too: no false
	// "scheduled run" label, no report-only framing.
	if strings.Contains(adds[1], "예약된 자동 실행") || strings.Contains(adds[1], "보고 본문") {
		t.Fatalf("directive still carries cron-only framing: %q", adds[1])
	}
	// Heartbeat shape: no recall (EphemeralUser skips it), no auto-delivery.
	if adds := buildTailAdditions(RunParams{}, "", ""); len(adds) != 0 {
		t.Fatalf("expected no additions, got %#v", adds)
	}
}

func TestBuildTailAdditions_NotebookGrounding(t *testing.T) {
	// A notebook-grounded turn withholds BOTH recall and the 업무 feed digest —
	// the pinned sources are the explicit scope. Only grounding + delivery ride.
	adds := buildTailAdditions(RunParams{AutoDeliveredOutput: true, FeedContext: "feed"}, "recall-블록", "노트북-그라운딩")
	if len(adds) != 2 || adds[0] != "노트북-그라운딩" || !strings.Contains(adds[1], "전달 정책") {
		t.Fatalf("grounded turn should be [grounding, delivery] (no recall/feed), got %#v", adds)
	}
	// Not grounded: recall + feed flow as reference material.
	adds = buildTailAdditions(RunParams{FeedContext: "feed"}, "recall-블록", "")
	if len(adds) != 2 || adds[0] != "recall-블록" || adds[1] != "feed" {
		t.Fatalf("ungrounded turn should be [recall, feed], got %#v", adds)
	}
}

func TestBuildTailAdditions_ChatbotTone(t *testing.T) {
	// 챗봇 workspace (chat: session): the tone directive is injected — between
	// recall and the delivery directive — so the 챗봇 reads as light general chat.
	adds := buildTailAdditions(RunParams{SessionKey: "chat:main", AutoDeliveredOutput: true}, "", "")
	if len(adds) != 2 || adds[0] != chatbotToneDirective || !strings.Contains(adds[1], "전달 정책") {
		t.Fatalf("expected [chatbot-tone, delivery] for chat: session, got %#v", adds)
	}
	// 업무 workspace (client:main): no tone directive — the system prompt's
	// chief-of-staff persona stands.
	for _, key := range []string{"client:main", "client:main:wf-x", "cron:mail:1", ""} {
		adds := buildTailAdditions(RunParams{SessionKey: key, AutoDeliveredOutput: true}, "", "")
		for _, a := range adds {
			if a == chatbotToneDirective {
				t.Fatalf("chatbot tone leaked into non-chat session %q", key)
			}
		}
	}
}

func TestSessionFallbackChannel(t *testing.T) {
	cases := map[string]string{
		"client:main":           "client",
		"client:lt-123":         "client",
		"cron:email-analysis:1": "",
		"system:skill-review:x": "",
		"acp:whatever":          "",
		"":                      "",
	}
	for key, want := range cases {
		if got := sessionFallbackChannel(key); got != want {
			t.Errorf("sessionFallbackChannel(%q) = %q, want %q", key, got, want)
		}
	}
}
