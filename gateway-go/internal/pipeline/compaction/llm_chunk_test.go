package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// markerPattern extracts the MARK-<n> tag baked into each big test message so
// the fake summarizer can answer per-chunk deterministically.
var markerPattern = regexp.MustCompile(`MARK-\d+`)

// markerSummarizer echoes the marker found in each conversation as
// "SUM(MARK-n)" and fails for markers listed in failOn. Safe for the parallel
// chunk fan-out.
type markerSummarizer struct {
	mu     sync.Mutex
	calls  []string
	failOn map[string]bool
}

func (m *markerSummarizer) Summarize(_ context.Context, _, conversation string, _ int) (string, error) {
	marker := markerPattern.FindString(conversation)
	m.mu.Lock()
	m.calls = append(m.calls, marker)
	m.mu.Unlock()
	if m.failOn[marker] {
		return "", errors.New("simulated chunk failure")
	}
	return "SUM(" + marker + ")", nil
}

func (m *markerSummarizer) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// bigMarkedMsg builds a user message of ~19.5K estimated tokens carrying a
// MARK-<i> tag, sized so each message lands in its own ≤chunkMaxTokens chunk.
func bigMarkedMsg(i int) llm.Message {
	return llm.NewTextMessage("user", fmt.Sprintf("MARK-%d %s", i, strings.Repeat("내용 ", 13_000)))
}

func TestSummarizeInChunks_BatchBounded(t *testing.T) {
	// 6 one-message chunks; only the oldest maxChunksPerPass (4) may run.
	var old []llm.Message
	for i := 0; i < 6; i++ {
		old = append(old, bigMarkedMsg(i))
	}

	s := &markerSummarizer{}
	summary, covered := summarizeInChunks(context.Background(), old, s, 10_000, "sys", nil)

	if covered != maxChunksPerPass {
		t.Fatalf("covered = %d, want %d (one message per chunk)", covered, maxChunksPerPass)
	}
	if s.callCount() != maxChunksPerPass {
		t.Fatalf("summarizer calls = %d, want %d (batch bound)", s.callCount(), maxChunksPerPass)
	}
	want := "SUM(MARK-0)\n\nSUM(MARK-1)\n\nSUM(MARK-2)\n\nSUM(MARK-3)"
	if summary != want {
		t.Fatalf("summary = %q, want joined in-order %q", summary, want)
	}
	if strings.Contains(summary, "MARK-4") || strings.Contains(summary, "MARK-5") {
		t.Fatal("summary must not cover chunks beyond the batch bound")
	}
}

func TestSummarizeInChunks_PrefixTolerance(t *testing.T) {
	var old []llm.Message
	for i := 0; i < 4; i++ {
		old = append(old, bigMarkedMsg(i))
	}

	// Chunk 2 fails: progress = contiguous successful prefix (chunks 0-1);
	// chunk 3's success must NOT be used (coverage would have a gap).
	s := &markerSummarizer{failOn: map[string]bool{"MARK-2": true}}
	summary, covered := summarizeInChunks(context.Background(), old, s, 10_000, "sys", nil)
	if covered != 2 {
		t.Fatalf("covered = %d, want 2 (prefix before failed chunk)", covered)
	}
	if want := "SUM(MARK-0)\n\nSUM(MARK-1)"; summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}

	// Chunk 0 fails: no usable prefix → the pass yields nothing.
	s = &markerSummarizer{failOn: map[string]bool{"MARK-0": true}}
	summary, covered = summarizeInChunks(context.Background(), old, s, 10_000, "sys", nil)
	if summary != "" || covered != 0 {
		t.Fatalf("got (%q, %d), want empty result when chunk 0 fails", summary, covered)
	}
}

// staticSummarizer returns a fixed result; used to simulate the
// pilot.CollectStream behavior of returning partial text with a nil error
// when the context deadline expires mid-stream.
type staticSummarizer struct{ out string }

func (s *staticSummarizer) Summarize(context.Context, string, string, int) (string, error) {
	return s.out, nil
}

func TestSummarizeOldMessages_ExpiredCtxTreatedAsFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate the shared compaction deadline already spent

	// Single-call path (small input ≥500 tokens).
	small := []llm.Message{
		llm.NewTextMessage("user", strings.Repeat("질문 ", 600)),
		llm.NewTextMessage("assistant", strings.Repeat("응답 ", 600)),
	}
	summary, covered := summarizeOldMessages(ctx, NewConfig(100_000), small, &staticSummarizer{out: "TRUNCATED"}, nil)
	if summary != "" || covered != 0 {
		t.Fatalf("got (%q, %d), want failure for likely-truncated output on expired ctx", summary, covered)
	}

	// Chunked path: same guard per chunk.
	big := []llm.Message{bigMarkedMsg(0), bigMarkedMsg(1)}
	summary, covered = summarizeOldMessages(ctx, NewConfig(100_000), big, &staticSummarizer{out: "TRUNCATED"}, nil)
	if summary != "" || covered != 0 {
		t.Fatalf("got (%q, %d), want failure for chunked path on expired ctx", summary, covered)
	}
}

func TestLLMCompact_LeftoverStaysRaw(t *testing.T) {
	// 6 big single-chunk messages + a recent tail with 6 assistant turns.
	var msgs []llm.Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, bigMarkedMsg(i))
	}
	for i := 0; i < 6; i++ {
		msgs = append(msgs, llm.NewTextMessage("user", fmt.Sprintf("recent q%d", i)))
		msgs = append(msgs, llm.NewTextMessage("assistant", fmt.Sprintf("recent a%d", i)))
	}

	s := &markerSummarizer{}
	compacted, summary, ok := LLMCompact(context.Background(), NewConfig(100_000), msgs, s, nil)
	if !ok {
		t.Fatal("expected LLMCompact to fire")
	}
	// findSplitPoint keeps the last 6 assistant turns → old = 6 big + 1 recent
	// user = 7 messages in 7 chunks (the tiny one rides with the last big one
	// or alone — either way past the batch bound). covered = 4.
	if !strings.Contains(summary, "MARK-3") || strings.Contains(summary, "MARK-4") {
		t.Fatalf("summary should cover through MARK-3 only, got %q", truncateForLog(summary))
	}

	// The uncovered old messages must survive raw, right after the summary fence.
	var rawSeen []string
	for _, m := range compacted[1:] {
		var text string
		if jsonUnmarshalText(m.Content, &text) {
			if marker := markerPattern.FindString(text); marker != "" {
				rawSeen = append(rawSeen, marker)
			}
		}
	}
	if len(rawSeen) != 2 || rawSeen[0] != "MARK-4" || rawSeen[1] != "MARK-5" {
		t.Fatalf("leftover raw markers = %v, want [MARK-4 MARK-5]", rawSeen)
	}

	// Fence first, recent tail last.
	var fenceText string
	if !jsonUnmarshalText(compacted[0].Content, &fenceText) || !strings.Contains(fenceText, "Polaris compaction") {
		t.Fatal("first message must be the compaction summary fence")
	}
	var lastText string
	if !jsonUnmarshalText(compacted[len(compacted)-1].Content, &lastText) || lastText != "recent a5" {
		t.Fatalf("recent tail must be preserved; last = %q", lastText)
	}
}

func TestSummarizeOldMessages_LargeIncrementalFallsBackToFreshChunks(t *testing.T) {
	cfg := NewConfig(100_000)
	cfg.PreviousSummary = "PREV_SUMMARY"

	old := []llm.Message{bigMarkedMsg(0), bigMarkedMsg(1)}

	capt := &recompCapture{out: "CHUNKED"}
	// recompCapture is not parallel-safe; two chunks may race on its fields.
	// Use a locking wrapper to keep -race clean while still seeing prompts.
	wrap := &lockingCapture{inner: capt}
	summary, covered := summarizeOldMessages(context.Background(), cfg, old, wrap, nil)
	if summary == "" || covered != 2 {
		t.Fatalf("got (%q, %d), want chunked fresh summary covering both messages", summary, covered)
	}
	for _, sys := range wrap.systems() {
		if strings.Contains(sys, "갱신") {
			t.Fatal("oversized incremental input must use the fresh prompt, not the update prompt")
		}
	}
	for _, conv := range wrap.convs() {
		if strings.Contains(conv, "PREV_SUMMARY") {
			t.Fatal("oversized incremental input must not inline the previous summary")
		}
	}
}

// lockingCapture is a -race-safe recording summarizer for parallel chunk calls.
type lockingCapture struct {
	mu    sync.Mutex
	inner *recompCapture
	sys   []string
	conv  []string
}

func (l *lockingCapture) Summarize(ctx context.Context, system, conversation string, maxOut int) (string, error) {
	l.mu.Lock()
	l.sys = append(l.sys, system)
	l.conv = append(l.conv, conversation)
	l.mu.Unlock()
	return l.inner.out, nil
}

func (l *lockingCapture) systems() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.sys...)
}

func (l *lockingCapture) convs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.conv...)
}

// jsonUnmarshalText decodes a plain-string message content.
func jsonUnmarshalText(raw []byte, out *string) bool {
	return json.Unmarshal(raw, out) == nil
}

func truncateForLog(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
