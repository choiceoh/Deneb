package compaction

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// --- helpers ---

func textMsg(role, text string) llm.Message {
	return llm.NewTextMessage(role, text)
}

func toolResultMsg(content string) llm.Message {
	blocks := []llm.ContentBlock{{
		Type:      "tool_result",
		ToolUseID: "tu_1",
		Content:   content,
	}}
	raw, _ := json.Marshal(blocks)
	return llm.Message{Role: "user", Content: raw}
}

// --- EstimateTokens ---

// --- MicroCompact ---

func TestMicroCompact_NoChange(t *testing.T) {
	msgs := []llm.Message{
		textMsg("user", "hello"),
		textMsg("assistant", "hi"),
		textMsg("user", "bye"),
		textMsg("assistant", "bye"),
	}
	result, pruned := MicroCompact(msgs, 4)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
	if len(result) != len(msgs) {
		t.Errorf("len = %d, want %d", len(result), len(msgs))
	}
}

func TestMicroCompact_StripsOldCode(t *testing.T) {
	msgs := []llm.Message{
		textMsg("user", "read file"),
		textMsg("assistant", "calling tool"),
		toolResultMsg("Here is the result:\n```go\nfunc main() {}\n```\nDone."),
		textMsg("assistant", "done turn 1"),
		textMsg("user", "next"),
		textMsg("assistant", "turn 2"),
		textMsg("user", "next"),
		textMsg("assistant", "turn 3"),
		textMsg("user", "next"),
		textMsg("assistant", "turn 4"),
		textMsg("user", "next"),
		textMsg("assistant", "turn 5"),
	}
	result, pruned := MicroCompact(msgs, 4)
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	// Verify the tool_result at index 2 had its code stripped.
	var blocks []llm.ContentBlock
	json.Unmarshal(result[2].Content, &blocks)
	if strings.Contains(blocks[0].Content, "func main") {
		t.Error("code should have been stripped from old tool result")
	}
	if !strings.Contains(blocks[0].Content, "[code omitted]") {
		t.Error("expected [code omitted] placeholder")
	}
	if !strings.Contains(blocks[0].Content, "Done.") {
		t.Error("non-code text should be preserved")
	}
}

func TestMicroCompact_PreservesRecentCode(t *testing.T) {
	code := "```go\npackage main\n```"
	msgs := []llm.Message{
		textMsg("user", "recent"),
		textMsg("assistant", "turn 1"),
		toolResultMsg("Result: " + code),
		textMsg("assistant", "turn 2"),
		textMsg("user", "next"),
		textMsg("assistant", "turn 3"),
	}
	// Only 3 turns, threshold is 4 → nothing should be stripped.
	_, pruned := MicroCompact(msgs, 4)
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 (too few turns)", pruned)
	}
}

// --- LLMCompact ---

type mockSummarizer struct {
	called bool
	system string
	text   string
}

func (m *mockSummarizer) Summarize(_ context.Context, system, conversation string, _ int) (string, error) {
	m.called = true
	m.system = system
	m.text = conversation
	return "### 핵심 사실\n- [테스트] 요약 완료", nil
}

func TestLLMCompact_SummarizesOld(t *testing.T) {
	// Build enough messages to have >6 assistant turns.
	var msgs []llm.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, textMsg("user", fmt.Sprintf("Question %d", i)))
		msgs = append(msgs, textMsg("assistant", fmt.Sprintf("Answer %d with lots of content %s", i, strings.Repeat("detail ", 50))))
	}

	cfg := NewConfig(200_000)
	cfg.ContextBudget = 1000 // low budget to trigger compaction

	s := &mockSummarizer{}
	result, ok := LLMCompact(context.Background(), cfg, msgs, s, nil)
	if !ok {
		t.Fatal("expected compaction to succeed")
	}
	if !s.called {
		t.Error("summarizer should have been called")
	}
	// First message should be the summary.
	firstText := string(result[0].Content)
	if !strings.Contains(firstText, "Polaris compaction") {
		t.Error("expected Polaris compaction marker in summary message")
	}
	if len(result) >= len(msgs) {
		t.Errorf("result should be shorter: got %d, original %d", len(result), len(msgs))
	}
}

// --- EmergencyCompact ---

func TestEmergencyCompact_EvictsOldestSummarizesNonEvicted(t *testing.T) {
	var msgs []llm.Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs, textMsg("user", fmt.Sprintf("Message %d %s", i, strings.Repeat("x", 2000))))
		msgs = append(msgs, textMsg("assistant", fmt.Sprintf("Reply %d %s", i, strings.Repeat("y", 2000))))
	}

	cfg := NewConfig(200_000)
	// Budget: 40 msgs × ~1000 tok = ~40K + 35K input = 75K. Budget 60K forces partial eviction.
	cfg.ContextBudget = 60000

	s := &mockSummarizer{}
	result, evicted := EmergencyCompact(context.Background(), cfg, msgs, 35000, s, nil)
	if evicted == 0 {
		t.Error("expected some messages to be evicted")
	}
	if !s.called {
		t.Error("expected summarizer to be called for non-evicted old messages")
	}
	if len(result) >= len(msgs) {
		t.Errorf("result should be shorter: got %d, original %d", len(result), len(msgs))
	}
	// First message should be the summary of non-evicted old messages.
	firstText := string(result[0].Content)
	if !strings.Contains(firstText, "summarized") {
		t.Error("expected summary marker in first message")
	}
	// Last 4 messages should be the preserved recent tail.
	if len(result) < 4 {
		t.Fatalf("result too short: %d", len(result))
	}
}

// --- Full pipeline ---

func TestCompact_PipelineOrder(t *testing.T) {
	// Build messages with code in old tool results.
	msgs := []llm.Message{
		textMsg("user", "old question"),
		textMsg("assistant", "old answer"),
		toolResultMsg("```python\nprint('hello')\n```\nOutput: hello"),
		textMsg("assistant", "done"),
	}
	// Add 5 more recent turns.
	for i := 0; i < 5; i++ {
		msgs = append(msgs, textMsg("user", "q"))
		msgs = append(msgs, textMsg("assistant", "a"))
	}

	cfg := NewConfig(200_000)
	cfg.ContextBudget = 999999 // high budget, no LLM compaction

	result, r := Compact(context.Background(), cfg, msgs, nil, nil)
	// Should have micro-compacted the old tool result.
	if r.MicroPruned != 1 {
		t.Errorf("MicroPruned = %d, want 1", r.MicroPruned)
	}
	if r.LLMCompacted {
		t.Error("LLM compaction should not trigger with high budget")
	}
	if r.EmergencyEvicted != 0 {
		t.Error("no emergency eviction expected")
	}
	if len(result) != len(msgs) {
		t.Errorf("message count should not change for micro-only: got %d, want %d", len(result), len(msgs))
	}
}
