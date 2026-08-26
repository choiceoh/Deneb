// engine_llm_unblock_test.go — regression tests for the client:main compaction
// wedge (2026-06-10): once a single DAG summary existed, the injected fence
// flipped SkipLLMCompaction on for every later turn, so the uncovered raw tail
// could never be summarized again (474 raw messages / 318K tokens, with the
// blunt safety trim doing all the cutting). These tests pin the unblocked
// behavior: fences are protected, the raw remainder still reaches the LLM
// tier, chunked passes persist the JOINED summary, and the safety trim keeps
// fences while reporting accurate post-trim token counts.
package polaris

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// seqSummarizer returns a structured "SUM-<n>" summary per call, recording
// inputs. The mandated section heading matters: the compaction layer drops an
// answer that carries none of them (a non-conforming answer would otherwise be
// stored as the range's summary and feed the next recompaction). Parallel-safe.
type seqSummarizer struct {
	mu    sync.Mutex
	calls int
}

func (s *seqSummarizer) Summarize(_ context.Context, _, _ string, _ int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return fmt.Sprintf("### 핵심 사실 (Facts)\n- [확실] SUM-%d", s.calls), nil
}

func (s *seqSummarizer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func msgText(t *testing.T, m llm.Message) string {
	t.Helper()
	var text string
	if json.Unmarshal(m.Content.Bytes(), &text) != nil {
		return ""
	}
	return text
}

// dagFenceMsg builds the summary message exactly as assembleContextFull
// injects it.
func dagFenceMsg(start, end int, content string) llm.Message {
	return llm.NewTextMessage("user", compact.FormatContextFence(
		"polaris-dag-summary",
		"conversation-summary",
		fmt.Sprintf("이전 대화 요약 (메시지 %d-%d)", start, end),
		content,
	))
}

// TestCompactAndPersistFiresLLMTierAndPreservesExistingFence is THE regression
// test for the production wedge: a DAG summary already exists and is injected
// as a fence, and the uncovered raw tail exceeds the LLM threshold. The LLM
// tier must still fire on the raw remainder, persist a NEW leaf node starting
// right after the existing coverage, and keep the injected fence intact.
func TestCompactAndPersistFiresLLMTierAndPreservesExistingFence(t *testing.T) {
	e, s := testEngine(t)
	sess := "s1"

	// Transcript: messages 0..39. 0..19 are covered by an existing node.
	for i := 0; i < 40; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		s.AppendMessage(sess, toolport.ChatMessage{
			Role:      role,
			Content:   marshalStr(fmt.Sprintf("m%d %s", i, makeString(2200))),
			Timestamp: int64(i * 1000),
		})
	}
	testutil.Must(s.InsertSummary(SummaryNode{
		SessionKey: sess, Level: 1, Content: "기존 요약", TokenEst: 10,
		CreatedAt: 1, MsgStart: 0, MsgEnd: 19,
	}))

	// Assembled context: fence + uncovered raw 20..39 (as assembleContextFull
	// would produce).
	msgs := []llm.Message{dagFenceMsg(0, 19, "기존 요약")}
	for i := 20; i < 40; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, llmMsg(role, fmt.Sprintf("m%d %s", i, makeString(2200))))
	}

	// Raw tail ≈ 20 × 1.1K = ~22K tokens; budget 24K → threshold ≈ 21K → fires.
	summarizer := &seqSummarizer{}
	compacted, result := e.CompactAndPersist(context.Background(), sess, msgs, summarizer, 24_000)

	if !result.LLMCompacted {
		t.Fatal("LLM tier must fire on the uncovered raw tail even when a DAG summary fence is present")
	}
	if summarizer.callCount() == 0 {
		t.Fatal("summarizer was never called")
	}

	// The injected fence survives, un-resummarized, at the head.
	if first := msgText(t, compacted[0]); !strings.Contains(first, "기존 요약") {
		t.Fatalf("first message must remain the injected DAG fence, got %q", truncateStr(first))
	}
	// Followed by the freshly produced compaction fence.
	if second := msgText(t, compacted[1]); !strings.Contains(second, "Polaris compaction") {
		t.Fatalf("second message must be the new summary fence, got %q", truncateStr(second))
	}

	// A NEW leaf node must be persisted, starting right after prior coverage.
	nodes := testutil.Must(s.LoadSummaries(sess, 1))
	if len(nodes) != 2 {
		t.Fatalf("got %d summary nodes, want 2 (existing + new)", len(nodes))
	}
	var newNode *SummaryNode
	for i := range nodes {
		if nodes[i].MsgStart == 20 {
			newNode = &nodes[i]
		}
	}
	if newNode == nil {
		t.Fatalf("no new node starting at msg 20; nodes=%+v", nodes)
	}
	if newNode.MsgEnd <= newNode.MsgStart || newNode.MsgEnd >= 40 {
		t.Fatalf("new node range [%d,%d] out of bounds", newNode.MsgStart, newNode.MsgEnd)
	}
	if !strings.Contains(newNode.Content, "SUM-") {
		t.Fatalf("new node content = %q, want summarizer output", truncateStr(newNode.Content))
	}

	// persistSummary count math: preserved messages = inner compacted minus the
	// new fence; everything before them is covered.
	preserved := len(compacted) - 2 // minus DAG fence and new summary fence
	wantEnd := 39 - preserved
	if newNode.MsgEnd != wantEnd {
		t.Fatalf("new node MsgEnd = %d, want %d (39 - %d preserved)", newNode.MsgEnd, wantEnd, preserved)
	}
}

// TestCompactAndPersistSavesJoinedChunkSummary pins that a chunked pass
// persists the JOINED summary, not whichever single chunk call finished last
// (the capturingSummarizer race that silently dropped chunks' facts).
func TestCompactAndPersistSavesJoinedChunkSummary(t *testing.T) {
	e, s := testEngine(t)
	sess := "s1"

	// Two big single-chunk messages + a 12-message recent tail (6 assistant
	// turns). Store mirrors the in-memory context so bootstrap is skipped.
	big := func(i int) string { return fmt.Sprintf("BIG-%d %s", i, makeString(78_000)) } // ~39K tokens
	var msgs []llm.Message
	for i := 0; i < 2; i++ {
		s.AppendMessage(sess, toolport.ChatMessage{Role: "user", Content: marshalStr(big(i)), Timestamp: int64(i)})
		msgs = append(msgs, llmMsg("user", big(i)))
	}
	for i := 0; i < 6; i++ {
		q, a := fmt.Sprintf("q%d", i), fmt.Sprintf("a%d", i)
		s.AppendMessage(sess, toolport.ChatMessage{Role: "user", Content: marshalStr(q), Timestamp: int64(100 + i)})
		s.AppendMessage(sess, toolport.ChatMessage{Role: "assistant", Content: marshalStr(a), Timestamp: int64(100 + i)})
		msgs = append(msgs, llmMsg("user", q), llmMsg("assistant", a))
	}

	// ~78K raw tokens; budget 80K → threshold 72K → fires; old = 2 big + 1
	// small = 2-3 chunks, all within one batch.
	summarizer := &seqSummarizer{}
	_, result := e.CompactAndPersist(context.Background(), sess, msgs, summarizer, 80_000)

	if !result.LLMCompacted {
		t.Fatal("expected LLM compaction to fire")
	}
	if summarizer.callCount() < 2 {
		t.Fatalf("expected ≥2 chunk calls, got %d", summarizer.callCount())
	}

	nodes := testutil.Must(s.LoadSummaries(sess, 1))
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	// The node must contain EVERY chunk's output joined, not a single one.
	for n := 1; n <= summarizer.callCount(); n++ {
		if !strings.Contains(nodes[0].Content, fmt.Sprintf("SUM-%d", n)) {
			t.Fatalf("node content %q missing chunk output SUM-%d (joined-summary regression)",
				truncateStr(nodes[0].Content), n)
		}
	}
}

// TestCompactAndPersistSafetyTrimPreservesFencesWithAccurateTokens pins the
// incremental-digestion regime: a pass covers only part of a huge backlog, the
// leftover raw keeps the turn over budget, and the safety trim must (a) keep
// the summary fences, (b) drop oldest raw only, and (c) report the post-trim
// token count in Result.TokensAfter so the caller's degraded-context warning
// fires only on real failures.
func TestCompactAndPersistSafetyTrimPreservesFencesWithAccurateTokens(t *testing.T) {
	e, s := testEngine(t)
	sess := "s1"
	var logBuf bytes.Buffer
	e.logger = slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// 6 big single-chunk messages (~19.5K tokens each) + 12-message tail.
	big := func(i int) string { return fmt.Sprintf("BIG-%d %s", i, makeString(39_000)) }
	var msgs []llm.Message
	for i := 0; i < 6; i++ {
		s.AppendMessage(sess, toolport.ChatMessage{Role: "user", Content: marshalStr(big(i)), Timestamp: int64(i)})
		msgs = append(msgs, llmMsg("user", big(i)))
	}
	for i := 0; i < 6; i++ {
		q, a := fmt.Sprintf("q%d", i), fmt.Sprintf("a%d", i)
		s.AppendMessage(sess, toolport.ChatMessage{Role: "user", Content: marshalStr(q), Timestamp: int64(100 + i)})
		s.AppendMessage(sess, toolport.ChatMessage{Role: "assistant", Content: marshalStr(a), Timestamp: int64(100 + i)})
		msgs = append(msgs, llmMsg("user", q), llmMsg("assistant", a))
	}

	// ~117K raw tokens, budget 30K: the pass digests maxChunksPerPass chunks,
	// leftover (~2 big msgs) still exceeds the budget → safety trim fires.
	summarizer := &seqSummarizer{}
	compacted, result := e.CompactAndPersist(context.Background(), sess, msgs, summarizer, 30_000)

	if !result.LLMCompacted {
		t.Fatal("expected LLM compaction to fire")
	}

	// (a) the new summary fence survives the trim at the head.
	if first := msgText(t, compacted[0]); !strings.Contains(first, "Polaris compaction") {
		t.Fatalf("summary fence must survive the safety trim, got %q", truncateStr(first))
	}

	// (b)+(c) trimmed under budget, and TokensAfter reflects the real result.
	actual := compact.EstimateMessagesTokens(compacted)
	if actual > 30_000 {
		t.Fatalf("post-trim context %d tokens still over budget 30000", actual)
	}
	if result.TokensAfter != actual {
		t.Fatalf("Result.TokensAfter = %d, want %d (post-trim); stale pre-trim counts re-create the spurious 'failed to reduce below budget' warning",
			result.TokensAfter, actual)
	}
	if logLine := logBuf.String(); !strings.Contains(logLine, `level=INFO msg="polaris: post-compaction safety trim fired"`) {
		t.Fatalf("safety trim must be an informational budget-enforcement event, log=%q", logLine)
	}

	// Progress persisted: a leaf node covering the digested prefix exists.
	nodes := testutil.Must(s.LoadSummaries(sess, 1))
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(nodes))
	}
	if nodes[0].MsgStart != 0 || nodes[0].MsgEnd < 0 {
		t.Fatalf("unexpected node range [%d,%d]", nodes[0].MsgStart, nodes[0].MsgEnd)
	}
}

// TestTrimWithFenceProtection_BalancesOrphanedToolPairs pins that the safety
// trim cannot emit a leading orphan tool_result (Anthropic 400) and prefers
// dropping raw messages over fences.
func TestTrimWithFenceProtection_BalancesOrphanedToolPairs(t *testing.T) {
	fence := dagFenceMsg(0, 9, makeString(4000)) // ~2K tokens

	toolUse, _ := json.Marshal([]llm.ContentBlock{{
		Type: "tool_use", ID: "tu_1", Name: "exec",
		Input: llm.FlexibleFromRaw([]byte(fmt.Sprintf(`{"cmd":%q}`, makeString(6000)))),
	}})
	toolResult, _ := json.Marshal([]llm.ContentBlock{{
		Type: "tool_result", ToolUseID: "tu_1", Content: makeString(6000),
	}})

	msgs := []llm.Message{
		fence,
		{Role: "assistant", Content: llm.FlexibleFromRaw(toolUse)}, // ~3K tokens — oldest raw, will be trimmed
		{Role: "user", Content: llm.FlexibleFromRaw(toolResult)},   // its result would orphan without balancing
		llmMsg("user", makeString(4000)),                           // ~2K
		llmMsg("assistant", makeString(4000)),                      // ~2K
		llmMsg("user", "recent question"),
		llmMsg("assistant", "recent answer"),
	}

	out := trimWithFenceProtection(msgs, 10_000)

	if first := msgText(t, out[0]); !compact.IsContextFenceText(first) {
		t.Fatalf("fence must be kept by the trim, got %q", truncateStr(first))
	}
	if got := compact.EstimateMessagesTokens(out); got > 10_000 {
		t.Fatalf("trimmed context %d tokens still over budget", got)
	}
	// The tool_use was dropped; its tool_result must have been stubbed, not
	// left as an orphan.
	for _, m := range out {
		var blocks []llm.ContentBlock
		if json.Unmarshal(m.Content.Bytes(), &blocks) != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "tool_result" {
				t.Fatalf("orphan tool_result survived the trim (tool_use was dropped): %+v", b)
			}
		}
	}
}

// TestBootstrapPartialCoverageSavesPartialRange pins that a bootstrap over
// a huge backlog — digested in bounded chunk batches — persists ONLY the range
// the summary actually covers. Persisting the full older range while covering
// a prefix (the pre-fix assumption) would silently lose the uncovered facts:
// coverage would skip past messages no summary ever saw.
func TestBootstrapPartialCoverageSavesPartialRange(t *testing.T) {
	e, s := testEngine(t)
	sess := "s1"

	// Store: 6 big messages (~19.5K tokens each ≈ 117K total ≥ 50K bootstrap
	// LLM threshold) + a 12-message fresh tail.
	big := func(i int) string { return fmt.Sprintf("BIG-%d %s", i, makeString(39_000)) }
	for i := 0; i < 6; i++ {
		s.AppendMessage(sess, toolport.ChatMessage{Role: "user", Content: marshalStr(big(i)), Timestamp: int64(i)})
	}
	var fresh []llm.Message
	for i := 0; i < 6; i++ {
		q, a := fmt.Sprintf("q%d", i), fmt.Sprintf("a%d", i)
		s.AppendMessage(sess, toolport.ChatMessage{Role: "user", Content: marshalStr(q), Timestamp: int64(100 + i)})
		s.AppendMessage(sess, toolport.ChatMessage{Role: "assistant", Content: marshalStr(a), Timestamp: int64(100 + i)})
		fresh = append(fresh, llmMsg("user", q), llmMsg("assistant", a))
	}

	summarizer := &seqSummarizer{}
	compacted, _ := e.CompactAndPersist(context.Background(), sess, fresh, summarizer, 170_000)

	nodes := testutil.Must(s.LoadSummaries(sess, 1))
	if len(nodes) != 1 {
		t.Fatalf("got %d nodes, want 1 bootstrap node", len(nodes))
	}
	// 6 one-message chunks, batch bound 4 → covered = 4 → range [0,3], NOT
	// [0,5] (olderEnd).
	if nodes[0].MsgStart != 0 || nodes[0].MsgEnd != 3 {
		t.Fatalf("bootstrap node range [%d,%d], want [0,3] (covered prefix only)",
			nodes[0].MsgStart, nodes[0].MsgEnd)
	}
	// The injected fence must declare the same partial range.
	if first := msgText(t, compacted[0]); !strings.Contains(first, "메시지 0-3") {
		t.Fatalf("bootstrap fence must reference the covered range, got %q", truncateStr(first))
	}
}

func truncateStr(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}
