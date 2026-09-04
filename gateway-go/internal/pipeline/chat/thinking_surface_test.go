package chat

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

func TestTranslateThinkingForDisplayTranslatesEnglish(t *testing.T) {
	deps := runDeps{translateThinking: func(_ context.Context, text string) (string, bool) {
		if !strings.Contains(text, "AttachmentID") {
			t.Fatalf("translator got unexpected text: %q", text)
		}
		return "번역됨", true
	}}
	got := translateThinkingForDisplay(deps, "the code sets AttachmentID to empty")
	if got != "번역됨" {
		t.Fatalf("english thinking was not translated: %q", got)
	}
}

func TestTranslateThinkingForDisplayFailsOpen(t *testing.T) {
	const original = "the code sets AttachmentID to empty"
	cases := map[string]func(context.Context, string) (string, bool){
		"translator reports failure": func(context.Context, string) (string, bool) { return "", false },
		"translator returns blank":   func(context.Context, string) (string, bool) { return "   ", true },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			// Losing the reply because a display nicety failed would be a far
			// worse trade than reading English.
			if got := translateThinkingForDisplay(runDeps{translateThinking: fn}, original); got != original {
				t.Fatalf("expected the original on failure, got %q", got)
			}
		})
	}
}

func TestTranslateThinkingForDisplayWithoutTranslator(t *testing.T) {
	const original = "the code sets AttachmentID to empty"
	// DeepL unconfigured: the feature is simply off, not broken.
	if got := translateThinkingForDisplay(runDeps{}, original); got != original {
		t.Fatalf("nil translator must pass the text through, got %q", got)
	}
}

func TestFormatRunReplyTextTranslatesTheBlockquote(t *testing.T) {
	// End to end through the render site: the 🧠 prefix must carry the
	// translated text, and the reply itself must be untouched.
	deps := runDeps{translateThinking: func(context.Context, string) (string, bool) {
		return "첨부 경로를 확인한다", true
	}}
	got := formatThinkingForChannel("native",
		translateThinkingForDisplay(deps, "check the attachment path"))
	if !strings.HasPrefix(got, "> 🧠 ") {
		t.Fatalf("blockquote prefix lost: %q", got)
	}
	if !strings.Contains(got, "첨부 경로를 확인한다") {
		t.Fatalf("translated text missing from the blockquote: %q", got)
	}
}

func TestDeliverDirectiveReplyTextSkipsFormattingWithNoChannelPush(t *testing.T) {
	// A native streaming turn already delivered its reply over SSE, so the 🧠
	// blockquote this path would build is discarded. Formatting it anyway spends
	// a whole-block translation — a network round trip on the turn's completion
	// path — for text nobody reads.
	calls := 0
	deps := runDeps{translateThinking: func(context.Context, string) (string, bool) {
		calls++
		return "번역됨", true
	}}
	params := RunParams{
		SessionKey: "client:t",
		Delivery:   &chatport.DeliveryContext{Channel: chatport.NativeClientChannel},
	}
	result := &agent.AgentResult{Text: "답", Thinking: "english reasoning about the code"}
	directives := chatport.ReplyDirectives{Text: "답"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	cancel := deliverDirectiveReplyText(params, deps, result, directives, true, logger)
	cancel()
	if calls != 0 {
		t.Fatalf("translator called %d times for a reply that is never pushed", calls)
	}

	// The same run WITHOUT an SSE delivery still needs its reply formatted —
	// that is a real (undelivered) reply, not a discarded one.
	cancel = deliverDirectiveReplyText(params, deps, result, directives, false, logger)
	cancel()
	if calls != 1 {
		t.Fatalf("translator called %d times, want 1 when the reply still needs a channel push", calls)
	}
}
