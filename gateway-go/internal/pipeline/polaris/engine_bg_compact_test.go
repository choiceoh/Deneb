// engine_bg_compact_test.go — tests for CompactInBackground, the off-critical-
// path (deferred) compaction that removes the synchronous stop-the-world (STW)
// summarization from interactive turns. The contract: a session's uncovered
// tail is summarized in a background goroutine and persisted to the DAG so a
// LATER turn assembles a compacted context, while the CURRENT turn runs raw.
//
// What these pin:
//   - the summary is persisted and the next assembly shrinks (it works),
//   - coverage is PINNED at launch, so messages appended while the pass runs are
//     never wrongly marked covered (no silent data loss — the core race risk),
//   - it is single-flighted per session (no redundant concurrent passes),
//   - the covered boundary never splits a tool_use↔tool_result pair (no orphan
//     → no Anthropic 400 on the next raw turn),
//   - the call returns off the critical path while the slow work runs in the
//     background (the latency win this change exists for).
package polaris

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// gatedSummarizer can block (gate) and/or delay (sleep) each Summarize call so a
// test can hold a background pass mid-flight or measure how long the caller is
// blocked. Parallel-safe (summarizeInChunks fans out goroutines).
type gatedSummarizer struct {
	mu    sync.Mutex
	calls int
	gate  chan struct{} // when non-nil, Summarize blocks until it is closed
	sleep time.Duration // when >0, Summarize delays this long
}

func (g *gatedSummarizer) Summarize(ctx context.Context, _, _ string, _ int) (string, error) {
	g.mu.Lock()
	g.calls++
	n := g.calls
	g.mu.Unlock()
	if g.gate != nil {
		select {
		case <-g.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if g.sleep > 0 {
		select {
		case <-time.After(g.sleep):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return fmt.Sprintf("BG-SUM-%d", n), nil
}

func (g *gatedSummarizer) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// seedSession appends `total` ~1.1K-token messages (alternating roles) and an
// existing summary node covering [0, coverageEnd]. Returns the store max index.
func seedSession(t *testing.T, s *Store, sess string, total, coverageEnd int) int {
	t.Helper()
	for i := 0; i < total; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		testutil.NoError(t, s.AppendMessage(sess, toolctx.ChatMessage{
			Role:      role,
			Content:   marshalStr(fmt.Sprintf("m%d %s", i, makeString(2200))),
			Timestamp: int64(i * 1000),
		}))
	}
	if coverageEnd >= 0 {
		testutil.Must(s.InsertSummary(SummaryNode{
			SessionKey: sess, Level: 1, Content: "기존 요약", TokenEst: 10,
			CreatedAt: 1, MsgStart: 0, MsgEnd: coverageEnd,
		}))
	}
	return testutil.Must(s.MaxMsgIndex(sess))
}

func waitNotInFlight(t *testing.T, e *Engine, sess string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !e.CompactionInFlight(sess) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("background compaction still in flight after %s", timeout)
}

func msgsContain(msgs []llm.Message, want string) bool {
	for _, m := range msgs {
		var text string
		if json.Unmarshal(m.Content, &text) == nil && len(text) >= len(want) {
			for i := 0; i+len(want) <= len(text); i++ {
				if text[i:i+len(want)] == want {
					return true
				}
			}
		}
	}
	return false
}

// TestCompactInBackground_PersistsAndShrinksNextAssembly: the happy path. An
// uncovered tail over the threshold is summarized in the background; a new leaf
// node lands right after prior coverage and the next assembly is smaller.
func TestCompactInBackground_PersistsAndShrinksNextAssembly(t *testing.T) {
	e, s := testEngine(t)
	sess := "bg-happy"
	seedSession(t, s, sess, 40, 9) // messages 0..39; existing summary covers 0..9

	before := testutil.Must(assembleContextFull(s, sess, 24_000, 24, slog.Default()))

	sum := &gatedSummarizer{}
	e.CompactInBackground(context.Background(), sess, sum, 24_000, nil, nil, nil)
	waitNotInFlight(t, e, sess, 10*time.Second)

	if sum.callCount() == 0 {
		t.Fatal("background summarizer was never called")
	}

	cov := testutil.Must(s.LatestSummaryCoverage(sess))
	if cov <= 9 {
		t.Fatalf("coverage did not advance past the existing summary: got %d, want >9", cov)
	}
	if cov >= 39 {
		t.Fatalf("coverage %d reached into the protected recent tail (should keep recent raw)", cov)
	}

	nodes := testutil.Must(s.LoadSummaries(sess, 1))
	var newNode *SummaryNode
	for i := range nodes {
		if nodes[i].MsgStart == 10 {
			newNode = &nodes[i]
		}
	}
	if newNode == nil {
		t.Fatalf("no new leaf node starting at msg 10; nodes=%+v", nodes)
	}
	if newNode.MsgEnd < newNode.MsgStart || newNode.MsgEnd >= 40 {
		t.Fatalf("new node range [%d,%d] out of bounds", newNode.MsgStart, newNode.MsgEnd)
	}

	after := testutil.Must(assembleContextFull(s, sess, 24_000, 24, slog.Default()))
	if !after.WasCompacted {
		t.Fatal("next assembly should be marked compacted")
	}
	if after.EstimatedTokens >= before.EstimatedTokens {
		t.Fatalf("next assembly did not shrink: before=%d after=%d", before.EstimatedTokens, after.EstimatedTokens)
	}
	t.Logf("assembly tokens before=%d after=%d (background compaction)", before.EstimatedTokens, after.EstimatedTokens)
}

// TestCompactInBackground_PinnedCoverageExcludesConcurrentAppends is the
// race-safety test: messages appended WHILE the background pass runs must never
// be marked covered (which would silently drop them from future assembly). The
// pass pins the store's max index at launch, so coverage can never exceed it.
func TestCompactInBackground_PinnedCoverageExcludesConcurrentAppends(t *testing.T) {
	e, s := testEngine(t)
	sess := "bg-race"
	pinnedMax := seedSession(t, s, sess, 40, 9) // 0..39

	gate := make(chan struct{})
	sum := &gatedSummarizer{gate: gate}
	e.CompactInBackground(context.Background(), sess, sum, 24_000, nil, nil, nil)

	// The pass is now blocked inside Summarize. Append new messages concurrently
	// — these must end up uncovered (indices past the pin).
	for i := 40; i < 45; i++ {
		testutil.NoError(t, s.AppendMessage(sess, toolctx.ChatMessage{
			Role: "user", Content: marshalStr(fmt.Sprintf("APPENDED-%d", i)), Timestamp: int64(i * 1000),
		}))
	}
	close(gate)
	waitNotInFlight(t, e, sess, 10*time.Second)

	cov := testutil.Must(s.LatestSummaryCoverage(sess))
	if cov > pinnedMax {
		t.Fatalf("coverage %d exceeds the pinned max %d — concurrently appended messages were wrongly covered (data loss)", cov, pinnedMax)
	}

	// The appended messages must still be present as raw recent in assembly.
	res := testutil.Must(assembleContextFull(s, sess, 1_000_000, 24, slog.Default()))
	if !msgsContain(res.Messages, "APPENDED-44") {
		t.Fatal("a message appended during the background pass was lost from assembly")
	}
}

// TestCompactInBackground_SingleFlight: a second launch while a pass is in
// flight is a no-op (no redundant LLM work, no duplicate leaf node).
func TestCompactInBackground_SingleFlight(t *testing.T) {
	e, s := testEngine(t)
	sess := "bg-single"
	seedSession(t, s, sess, 40, 9)

	gate := make(chan struct{})
	sum := &gatedSummarizer{gate: gate}

	e.CompactInBackground(context.Background(), sess, sum, 24_000, nil, nil, nil)
	if !e.CompactionInFlight(sess) {
		t.Fatal("expected a compaction in flight after the first launch")
	}
	// Second launch while the first is blocked: must not start a second pass.
	e.CompactInBackground(context.Background(), sess, sum, 24_000, nil, nil, nil)

	close(gate)
	waitNotInFlight(t, e, sess, 10*time.Second)

	nodes := testutil.Must(s.LoadSummaries(sess, 1))
	newLeaves := 0
	for _, n := range nodes {
		if n.MsgStart == 10 {
			newLeaves++
		}
	}
	if newLeaves != 1 {
		t.Fatalf("single-flight violated: %d new leaves at MsgStart=10, want exactly 1", newLeaves)
	}
}

// TestCompactInBackground_BelowThresholdNoOp: an uncovered tail under the LLM
// threshold is not worth a background pass — no goroutine, no summary.
func TestCompactInBackground_BelowThresholdNoOp(t *testing.T) {
	e, s := testEngine(t)
	sess := "bg-small"
	for i := 0; i < 6; i++ {
		testutil.NoError(t, s.AppendMessage(sess, toolctx.ChatMessage{
			Role: "user", Content: marshalStr(fmt.Sprintf("tiny%d", i)), Timestamp: int64(i),
		}))
	}

	e.CompactInBackground(context.Background(), sess, &gatedSummarizer{}, 100_000, nil, nil, nil)

	if e.CompactionInFlight(sess) {
		t.Fatal("no pass should be in flight: tail is far under the threshold")
	}
	if nodes := testutil.Must(s.LoadSummaries(sess, 0)); len(nodes) != 0 {
		t.Fatalf("expected no summary persisted, got %d nodes", len(nodes))
	}
}

// TestCompactInBackground_NilSummarizerNoOp: defensive — a nil summarizer must
// not acquire the single-flight or spawn anything.
func TestCompactInBackground_NilSummarizerNoOp(t *testing.T) {
	e, s := testEngine(t)
	sess := "bg-nilsum"
	seedSession(t, s, sess, 40, 9)

	e.CompactInBackground(context.Background(), sess, nil, 24_000, nil, nil, nil)
	if e.CompactionInFlight(sess) {
		t.Fatal("nil summarizer must not start or hold a compaction")
	}
}

// TestCompactInBackground_OffCriticalPath measures the win: the caller returns
// long before the (deliberately slow) summarization finishes, whereas the
// synchronous CompactAndPersist blocks the caller for the whole summarizer time.
func TestCompactInBackground_OffCriticalPath(t *testing.T) {
	const summarizerDelay = 300 * time.Millisecond

	// Synchronous baseline on its own session: blocks for the summarizer.
	eSync, sSync := testEngine(t)
	seedSession(t, sSync, "bg-sync", 40, 9)
	syncMsgs := []llm.Message{dagFenceMsg(0, 9, "기존 요약")}
	for i := 10; i < 40; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		syncMsgs = append(syncMsgs, llmMsg(role, fmt.Sprintf("m%d %s", i, makeString(2200))))
	}
	syncStart := time.Now()
	eSync.CompactAndPersist(context.Background(), "bg-sync", syncMsgs, &gatedSummarizer{sleep: summarizerDelay}, 24_000)
	syncBlocked := time.Since(syncStart)

	// Background path on an identical session: returns immediately.
	eBg, sBg := testEngine(t)
	seedSession(t, sBg, "bg-async", 40, 9)
	bgStart := time.Now()
	eBg.CompactInBackground(context.Background(), "bg-async", &gatedSummarizer{sleep: summarizerDelay}, 24_000, nil, nil, nil)
	bgReturned := time.Since(bgStart)
	waitNotInFlight(t, eBg, "bg-async", 10*time.Second)

	t.Logf("STW: synchronous CompactAndPersist blocked the caller %v; CompactInBackground returned in %v (summarizer delay %v ran off the critical path)",
		syncBlocked, bgReturned, summarizerDelay)

	if syncBlocked < summarizerDelay {
		t.Fatalf("synchronous path returned in %v, expected to block ~%v", syncBlocked, summarizerDelay)
	}
	if bgReturned > summarizerDelay/2 {
		t.Fatalf("CompactInBackground blocked the caller %v (~ summarizer delay) — not off the critical path", bgReturned)
	}
}

// TestSafeCoverageCount: the covered boundary is snapped back past any trailing
// tool_result so the next assembly's recent window never starts with an orphan.
func TestSafeCoverageCount(t *testing.T) {
	toolUse := llm.NewBlockMessage("assistant", []llm.ContentBlock{{Type: "tool_use", ID: "t1", Name: "exec"}})
	toolRes := llm.NewBlockMessage("user", []llm.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Content: "r"}})
	u, a := llmMsg("user", "hi"), llmMsg("assistant", "yo")
	raw := []llm.Message{u, a, toolUse, toolRes, u, a} // tool pair at indices 2,3

	cases := []struct{ in, want int }{
		{3, 2}, // raw[3] is the tool_result → back off so both halves stay uncovered
		{2, 2}, // raw[2] is the tool_use (not a tool_result) → unchanged
		{4, 4}, // raw[4] is a plain message → unchanged
		{6, 6}, // full coverage → unchanged
		{0, 0}, // nothing covered → unchanged
		{9, 6}, // over-long → clamped to len(raw)
	}
	for _, c := range cases {
		if got := safeCoverageCount(raw, c.in); got != c.want {
			t.Errorf("safeCoverageCount(_, %d) = %d, want %d", c.in, got, c.want)
		}
	}
}
