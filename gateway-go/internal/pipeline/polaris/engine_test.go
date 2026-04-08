package polaris

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func testEngine(t *testing.T) (*Engine, *Store) {
	t.Helper()
	dir := t.TempDir()
	s := testutil.Must(NewStore(filepath.Join(dir, "test.db")))
	t.Cleanup(func() { s.Close() })
	logger := slog.Default()
	e := NewEngine(s, logger, DefaultConfig())
	return e, s
}

func llmMsg(role, text string) llm.Message {
	return llm.NewTextMessage(role, text)
}

// mockSummarizer returns a fixed summary for testing.
type mockSummarizer struct {
	summary string
	called  bool
}

func (m *mockSummarizer) Summarize(_ context.Context, _, _ string, _ int) (string, error) {
	m.called = true
	return m.summary, nil
}

func TestCompactAndPersist_NoLLMCompaction(t *testing.T) {
	e, s := testEngine(t)

	// Seed a few messages in the LCM store so DAG tracking works.
	for i := 0; i < 5; i++ {
		s.AppendMessage("s1", textMsg("user", "hello", int64(i*1000)))
		s.AppendMessage("s1", textMsg("assistant", "hi", int64(i*1000+500)))
	}

	// Small context — Polaris should not trigger LLM compaction.
	msgs := []llm.Message{
		llmMsg("user", "hello"),
		llmMsg("assistant", "hi"),
	}

	compacted, result := e.CompactAndPersist(context.Background(), "s1", msgs, nil, 170_000)
	if result.LLMCompacted {
		t.Fatal("expected no LLM compaction for small context")
	}
	if len(compacted) != 2 {
		t.Fatalf("got %d, want 2 messages", len(compacted))
	}

	// No summary nodes should be created.
	nodes := testutil.Must(s.LoadSummaries("s1", 0))
	if len(nodes) != 0 {
		t.Fatalf("got %d, want 0 summary nodes", len(nodes))
	}
}

func TestCompactAndPersist_WithLLMCompaction(t *testing.T) {
	e, s := testEngine(t)

	// Seed enough messages to trigger LLM compaction.
	// Polaris triggers at 80% of 170K = 136K tokens.
	// We need total tokens > 136K. Each message ~5K tokens.
	// 30 messages × 5K = 150K tokens > 136K threshold.
	bigText := makeString(10000) // ~5000 tokens (runes/2)
	for i := 0; i < 30; i++ {
		s.AppendMessage("s1", toolctx.ChatMessage{
			Role:      "user",
			Content:   marshalStr(bigText),
			Timestamp: int64(i * 1000),
		})
	}

	// Build the llm.Message list matching the LCM store.
	var msgs []llm.Message
	for i := 0; i < 30; i++ {
		if i%2 == 0 {
			msgs = append(msgs, llmMsg("user", bigText))
		} else {
			msgs = append(msgs, llmMsg("assistant", bigText))
		}
	}

	summarizer := &mockSummarizer{summary: "### 핵심 사실\n- [테스트] 요약된 내용"}

	compacted, result := e.CompactAndPersist(context.Background(), "s1", msgs, summarizer, 170_000)

	if !summarizer.called {
		t.Fatal("summarizer was not called")
	}
	if !result.LLMCompacted {
		t.Fatal("expected LLM compaction to fire")
	}
	if len(compacted) >= len(msgs) {
		t.Fatalf("got %d (was %d), want fewer messages after compaction", len(compacted), len(msgs))
	}

	// A summary node should be persisted in the DAG.
	nodes := testutil.Must(s.LoadSummaries("s1", 1)) // level 1 = leaf
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 summary node after LLM compaction")
	}
	if nodes[0].Content == "" {
		t.Fatal("summary node has empty content")
	}
	if nodes[0].MsgStart > nodes[0].MsgEnd {
		t.Fatalf("invalid summary range: start=%d end=%d", nodes[0].MsgStart, nodes[0].MsgEnd)
	}
}

func TestCapturingSummarizer(t *testing.T) {
	var captured string
	inner := &mockSummarizer{summary: "captured text"}
	cs := &capturingSummarizer{inner: inner, captured: &captured}

	result := testutil.Must(cs.Summarize(context.Background(), "sys", "conv", 100))
	if result != "captured text" {
		t.Fatalf("unexpected result: %s", result)
	}
	if captured != "captured text" {
		t.Fatalf("capture failed: got %q", captured)
	}
}

func TestShouldCompact(t *testing.T) {
	e, _ := testEngine(t)

	tests := []struct {
		current, budget int
		want            CompactUrgency
	}{
		{50_000, 150_000, CompactNone},  // 33%
		{112_000, 150_000, CompactNone}, // 74.7% < 75%
		{113_000, 150_000, CompactSoft}, // 75.3%
		{135_000, 150_000, CompactHard}, // 90%
		{150_000, 150_000, CompactHard}, // 100%
		{0, 0, CompactNone},             // zero budget
	}

	for _, tt := range tests {
		got := e.ShouldCompact("s1", tt.current, tt.budget)
		if got != tt.want {
			t.Errorf("ShouldCompact(%d, %d) = %d, want %d", tt.current, tt.budget, got, tt.want)
		}
	}
}

// makeString creates a string of n runes (for token estimation: n/2 tokens).
func makeString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func marshalStr(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// Verify that compact.Summarizer is satisfied by capturingSummarizer.
var _ compact.Summarizer = (*capturingSummarizer)(nil)
