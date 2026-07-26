package chat

import (
	"context"
	"strings"
	"testing"
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
