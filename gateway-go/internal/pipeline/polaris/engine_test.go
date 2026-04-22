package polaris

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
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
	// Bootstrap will recover the 8 older messages (raw inject, < 50K threshold).
	msgs := []llm.Message{
		llmMsg("user", "hello"),
		llmMsg("assistant", "hi"),
	}

	compacted, result := e.CompactAndPersist(context.Background(), "s1", msgs, nil, 170_000)
	if result.LLMCompacted {
		t.Fatal("expected no LLM compaction for small context")
	}
	// Bootstrap injects 8 older messages (raw) + 2 fresh = 10 total.
	if len(compacted) != 10 {
		t.Fatalf("got %d, want 10 messages (8 bootstrap + 2 fresh)", len(compacted))
	}

	// No summary nodes — raw inject does not persist to DAG.
	nodes := testutil.Must(s.LoadSummaries("s1", 0))
	if len(nodes) != 0 {
		t.Fatalf("got %d, want 0 summary nodes", len(nodes))
	}
}

// TestCompactAndPersist_BootstrapNoGapAtFreshTailBoundary guards against a
// regression where a synthetic message prepended by the caller (e.g. a
// "context truncated" notice) inflates len(messages), causing
// bootstrapIfNeeded to compute olderEnd one short and orphan the message
// at the fresh-tail boundary. The caller must pass only real transcript
// messages; this test documents and enforces that contract.
func TestCompactAndPersist_BootstrapNoGapAtFreshTailBoundary(t *testing.T) {
	e, s := testEngine(t)
	sess := "s1"
	const total = 30

	// Seed 30 numbered messages in the store (indices 0..29).
	for i := range total {
		s.AppendMessage(sess, textMsg("user", "m"+strconv.Itoa(i), int64(i*1000)))
	}

	// Simulate assembly loading only the last freshTail=24 messages
	// (no synthetic notice prepended — that was the source of the bug).
	const freshTail = 24
	fresh := make([]llm.Message, 0, freshTail)
	for i := total - freshTail; i < total; i++ {
		fresh = append(fresh, llmMsg("user", "m"+strconv.Itoa(i)))
	}

	compacted, _ := e.CompactAndPersist(context.Background(), sess, fresh, nil, 170_000)

	// After bootstrap raw-inject (< 50K tokens), every transcript message
	// 0..29 must appear exactly once in compacted — no gap at the fresh-tail
	// boundary (msg at index total-freshTail-1 = index 5).
	seen := make(map[string]int, total)
	for _, m := range compacted {
		var text string
		if json.Unmarshal(m.Content, &text) == nil {
			seen[text]++
		}
	}
	for i := range total {
		key := "m" + strconv.Itoa(i)
		if seen[key] != 1 {
			t.Fatalf("message %s appears %d times in compacted context (expected exactly 1 — fresh-tail boundary orphan bug)", key, seen[key])
		}
	}
}

// TestCompactAndPersist_BootstrapWithSyntheticPrependOrphans documents the
// pre-fix failure mode: when a caller prepends a synthetic (non-transcript)
// message, bootstrapIfNeeded's olderEnd := maxIdx - len(messages) calculation
// is off by one, and the message at the fresh-tail boundary is permanently
// lost. The fix lives in the caller (run_exec.go no longer injects a context
// notice before compaction); this test pins the contract so a future
// re-introduction of caller-side prepending surfaces as a loud failure.
func TestCompactAndPersist_BootstrapWithSyntheticPrependOrphans(t *testing.T) {
	e, s := testEngine(t)
	sess := "s1"
	const total = 30

	for i := range total {
		s.AppendMessage(sess, textMsg("user", "m"+strconv.Itoa(i), int64(i*1000)))
	}

	// Simulate the pre-fix caller: prepend a synthetic notice in front of
	// the fresh tail so len(messages) = 25 when only 24 real transcript
	// messages are present.
	const freshTail = 24
	msgs := make([]llm.Message, 0, freshTail+1)
	msgs = append(msgs, llmMsg("user", "[context notice]"))
	for i := total - freshTail; i < total; i++ {
		msgs = append(msgs, llmMsg("user", "m"+strconv.Itoa(i)))
	}

	compacted, _ := e.CompactAndPersist(context.Background(), sess, msgs, nil, 170_000)

	// With the synthetic prepend, bootstrap computes olderEnd = 29 - 25 = 4
	// instead of 5, so "m5" is neither in older nor in fresh tail — orphan.
	seen := make(map[string]bool, total)
	for _, m := range compacted {
		var text string
		if json.Unmarshal(m.Content, &text) == nil {
			seen[text] = true
		}
	}
	if seen["m5"] {
		t.Fatal("m5 unexpectedly present — did bootstrap become tolerant of synthetic prepend? update this test")
	}
	if !seen["m4"] || !seen["m6"] {
		t.Fatalf("expected m4 and m6 to be present; got m4=%v m6=%v", seen["m4"], seen["m6"])
	}
}

func TestCompactAndPersist_WithLLMCompaction(t *testing.T) {
	e, s := testEngine(t)

	// Seed enough messages to trigger LLM compaction.
	// Polaris triggers at 90% of 170K = 153K tokens.
	// We need total tokens > 153K. Each message ~5K tokens.
	// 32 messages × 5K = 160K tokens > 153K threshold.
	bigText := makeString(10000) // ~5000 tokens (runes/2)
	for i := 0; i < 32; i++ {
		s.AppendMessage("s1", toolctx.ChatMessage{
			Role:      "user",
			Content:   marshalStr(bigText),
			Timestamp: int64(i * 1000),
		})
	}

	// Build the llm.Message list matching the LCM store.
	var msgs []llm.Message
	for i := 0; i < 32; i++ {
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

	// Defaults are Soft=0.70, Hard=0.85 — chosen to keep headroom between
	// compaction and the provider's hard context limit, so a single large
	// tool result cannot push the turn into the mid-loop emergency path.
	tests := []struct {
		current, budget int
		want            CompactUrgency
	}{
		{50_000, 150_000, CompactNone},  // 33%
		{104_000, 150_000, CompactNone}, // 69.3% < 70%
		{106_000, 150_000, CompactSoft}, // 70.7%
		{127_000, 150_000, CompactSoft}, // 84.7% < 85%
		{128_000, 150_000, CompactHard}, // 85.3%
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
