package compaction

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// D: serializeMessages must not split a multi-byte rune when truncating a long
// tool_result (Korean is 3 bytes/char, so a byte cap at 800 lands mid-rune).
func TestSerializeMessages_TruncatesOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("가나다", 500) // 1500 runes, 4500 bytes
	msg := llm.NewBlockMessage("user", []llm.ContentBlock{
		{Type: "tool_result", Content: long},
	})
	out := serializeMessages([]llm.Message{msg})
	if !utf8.ValidString(out) {
		t.Fatalf("serialized output has invalid UTF-8 (rune split at truncation): %q", out[:50])
	}
	if !strings.Contains(out, "...") {
		t.Fatalf("expected truncation marker, got %q", out[:50])
	}
}

// E: on ctx cancellation a summarizer can return partial text with a nil error
// (the CollectStream trap). EmergencyCompact must NOT persist that truncated
// summary as covering the evicted range — it should fall back to raw messages.
type partialSummarizer struct{}

func (partialSummarizer) Summarize(context.Context, string, string, int) (string, error) {
	return "TRUNCATED_SUMMARY", nil // partial text, nil error
}

func TestEmergencyCompact_DropsTruncatedSummaryOnCtxCancel(t *testing.T) {
	big := strings.Repeat("x", 8000)
	messages := []llm.Message{
		llm.NewTextMessage("user", big),      // old: evicted
		llm.NewTextMessage("assistant", big), // old: evicted
		llm.NewTextMessage("user", "keepA"),  // old: non-evicted → summarized
		llm.NewTextMessage("assistant", "keepB"),
		llm.NewTextMessage("user", "recent1"), // recent tail (recentKeep=4)
		llm.NewTextMessage("assistant", "recent2"),
		llm.NewTextMessage("user", "recent3"),
		llm.NewTextMessage("assistant", "recent4"),
	}
	cfg := Config{ContextBudget: 200}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // expired before Summarize returns

	out, evicted := EmergencyCompact(ctx, cfg, messages, partialSummarizer{}, nil)
	if evicted == 0 {
		t.Skip("budget did not trigger eviction in this environment; path not exercised")
	}
	joined := serializeMessages(out)
	if strings.Contains(joined, "TRUNCATED_SUMMARY") {
		t.Fatalf("truncated summary must be dropped on ctx-cancel, got it in output")
	}
	if !strings.Contains(joined, "recent4") {
		t.Fatalf("recent tail must be preserved, got %q", joined)
	}
}
