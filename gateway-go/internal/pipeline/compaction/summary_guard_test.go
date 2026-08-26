package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

type fixedSummarizer struct{ out string }

func (f fixedSummarizer) Summarize(_ context.Context, _, _ string, _ int) (string, error) {
	return f.out, nil
}

func guardOldMessages() []llm.Message {
	return []llm.Message{
		llm.NewTextMessage("user", strings.Repeat("질문 ", 200)),
		llm.NewTextMessage("assistant", strings.Repeat("답변 ", 200)),
	}
}

// A non-conforming answer is a summary only by position. Accepting it replaces
// the range with noise AND feeds the next recompaction — the compaction prompt
// itself warns that one bad line compounds across passes.
func TestNonConformingSummaryIsRejected(t *testing.T) {
	for _, junk := range []string{
		"죄송합니다. 요청을 이해하지 못했습니다.",
		"OK",
		"   ",
	} {
		got, covered := summarizeOldMessages(
			context.Background(), NewConfig(100000), guardOldMessages(), fixedSummarizer{out: junk}, nil,
		)
		if got != "" || covered != 0 {
			t.Fatalf("junk %q was accepted as a summary (%q, %d)", junk, got, covered)
		}
	}
}

func TestStructuredSummaryIsAccepted(t *testing.T) {
	summary := "### 핵심 사실 (Facts)\n- [확실] 톤: 간결\n\n### 열린 루프 (Open Loops)\n- 없음"

	got, covered := summarizeOldMessages(
		context.Background(), NewConfig(100000), guardOldMessages(), fixedSummarizer{out: summary}, nil,
	)

	if got != summary || covered != len(guardOldMessages()) {
		t.Fatalf("a conforming summary must pass through: %q %d", got, covered)
	}
}

// A truncated answer keeps only its leading sections; one heading is enough.
func TestTruncatedSummaryWithOneHeadingIsAccepted(t *testing.T) {
	if !looksLikeStructuredSummary("### 핵심 사실 (Facts)\n- [확실] 값") {
		t.Fatal("a single mandated heading must be enough")
	}
}
