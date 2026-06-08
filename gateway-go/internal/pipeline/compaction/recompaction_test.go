package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// recompCapture records what the summarizer was asked, to assert which prompt
// path (fresh vs incremental update) ran.
type recompCapture struct {
	system string
	conv   string
	out    string
}

func (c *recompCapture) Summarize(_ context.Context, system, conversation string, _ int) (string, error) {
	c.system = system
	c.conv = conversation
	return c.out, nil
}

func recompOldMessages() []llm.Message {
	return []llm.Message{
		llm.NewTextMessage("user", strings.Repeat("질문내용 ", 200)),
		llm.NewTextMessage("assistant", strings.Repeat("응답내용 ", 200)),
	}
}

func TestSummarizeOldMessages_IncrementalUpdate(t *testing.T) {
	capt := &recompCapture{out: "UPDATED"}
	cfg := NewConfig(100000)
	cfg.PreviousSummary = "PREV_SUMMARY_MARKER_XYZ"

	got := summarizeOldMessages(context.Background(), cfg, recompOldMessages(), capt, nil)
	if got != "UPDATED" {
		t.Fatalf("summary = %q, want UPDATED", got)
	}
	// Incremental path uses the recompaction (update) system prompt.
	if !strings.Contains(capt.system, "갱신") {
		t.Errorf("expected recompaction prompt (갱신); system prompt lacked it")
	}
	// The prior summary is fed into the conversation input under the update label.
	if !strings.Contains(capt.conv, "PREV_SUMMARY_MARKER_XYZ") {
		t.Errorf("previous summary was not fed to the summarizer")
	}
	if !strings.Contains(capt.conv, "이전 요약") {
		t.Errorf("update label (이전 요약) missing from conversation input")
	}
}

func TestSummarizeOldMessages_FreshNoPrevious(t *testing.T) {
	capt := &recompCapture{out: "FRESH"}
	cfg := NewConfig(100000) // no PreviousSummary

	got := summarizeOldMessages(context.Background(), cfg, recompOldMessages(), capt, nil)
	if got != "FRESH" {
		t.Fatalf("summary = %q, want FRESH", got)
	}
	if strings.Contains(capt.system, "갱신") {
		t.Errorf("fresh path must not use the recompaction (갱신) prompt")
	}
	if strings.Contains(capt.conv, "이전 요약") {
		t.Errorf("fresh path must not carry the update label")
	}
}

// LLMCompact must surface the produced summary so the caller can persist it as
// the next PreviousSummary.
func TestLLMCompact_ReturnsSummaryForRecompaction(t *testing.T) {
	// Need > keepRecentTurns assistant turns so findSplitPoint yields old>1.
	var msgs []llm.Message
	for range keepRecentTurns + 3 {
		msgs = append(msgs, llm.NewTextMessage("user", strings.Repeat("입력 ", 80)))
		msgs = append(msgs, llm.NewTextMessage("assistant", strings.Repeat("출력 ", 80)))
	}
	capt := &recompCapture{out: "THE_SUMMARY"}
	_, summary, ok := LLMCompact(context.Background(), NewConfig(100000), msgs, capt, nil)
	if !ok {
		t.Fatal("expected LLMCompact to fire")
	}
	if summary != "THE_SUMMARY" {
		t.Fatalf("returned summary = %q, want THE_SUMMARY", summary)
	}
}
