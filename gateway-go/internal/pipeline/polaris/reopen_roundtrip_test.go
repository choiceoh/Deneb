// reopen_roundtrip_test.go — the session-reopen round trip
// (docs/research/improvement-ideas.md §3.1): a compacted session, reopened by
// a FRESH store over the same directory (= process restart), must restore the
// original messages raw — not their summaries. Restarts are routine here
// (deploy hot-swap, OOM, manual), and a silent break in this path surfaces to
// the operator as "왜 갑자기 내 이름을 까먹지".
//
// What this pins:
//   - the first user turn's body survives compaction + restart byte-identical
//     (prompt-cache rule: compaction summarizes the MIDDLE; head stays raw),
//   - summary coverage persists across restart (compaction is not re-done and
//     not lost),
//   - MsgIndex stays monotonic across reopen (appending after restart
//     continues, never overwrites),
//   - a post-restart assembly still yields summary fences + the raw tail.
package polaris

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestSessionReopenRoundTrip_CompactedSessionRestoresRawMessages(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	const sess = "s1"
	firstUserBody := "첫 턴: 내 이름은 최선호이고 곡성 프로젝트 납기는 8월 20일이야. " + makeString(400)

	s1 := testutil.Must(NewStore(dbPath))
	testutil.NoError(t, s1.AppendMessage(sess, textMsg("user", firstUserBody, 0)))
	testutil.NoError(t, s1.AppendMessage(sess, textMsg("assistant", "기억했습니다. "+makeString(400), 1)))
	for i := 2; i < 30; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		testutil.NoError(t, s1.AppendMessage(sess, toolport.ChatMessage{
			Role:      role,
			Content:   marshalStr(fmt.Sprintf("m%d %s", i, makeString(2200))),
			Timestamp: int64(i * 1000),
		}))
	}

	// Tier-1 compaction: ~30 × ~1.1K-token messages against an 8K budget
	// crosses the LLM threshold (0.9), so the summarizer fires and the
	// summary node is persisted to the DAG.
	engine := NewEngine(s1, logger, Config{})
	assembled := testutil.Must(assembleContextFull(s1, sess, 1<<20, 0, logger))
	_, result := engine.CompactAndPersist(context.Background(), sess, assembled.Messages, &gatedSummarizer{}, 8000)
	if !result.LLMCompacted {
		t.Fatalf("fixture did not LLM-compact: result=%+v", result)
	}
	coverageBefore := testutil.Must(s1.LatestSummaryCoverage(sess))
	if coverageBefore < 0 {
		t.Fatalf("summary not persisted before restart (coverage=%d)", coverageBefore)
	}
	maxBefore := testutil.Must(s1.MaxMsgIndex(sess))

	// "Process restart": a fresh store over the same directory — first access
	// replays messages/summaries from disk.
	s2 := testutil.Must(NewStore(dbPath))

	// The original first user turn restores raw, byte-identical — not as a
	// summary (compaction may only ever summarize; the persisted originals
	// are immutable).
	restored := testutil.Must(s2.LoadMessages(sess, 0, 0))
	if len(restored) != 1 || restored[0].Role != "user" {
		t.Fatalf("first message not restored: %+v", restored)
	}
	if got := restored[0].TextContent(); got != firstUserBody {
		t.Fatalf("first user body mutated across restart:\n got: %.120q\nwant: %.120q", got, firstUserBody)
	}

	// Compaction state survives: same coverage, same max index.
	if got := testutil.Must(s2.LatestSummaryCoverage(sess)); got != coverageBefore {
		t.Fatalf("summary coverage after restart = %d, want %d", got, coverageBefore)
	}
	if got := testutil.Must(s2.MaxMsgIndex(sess)); got != maxBefore {
		t.Fatalf("max msg index after restart = %d, want %d", got, maxBefore)
	}

	// Context consistency on the next turn: assembly over the reopened store
	// yields a non-empty context whose uncovered tail is raw.
	reassembled := testutil.Must(assembleContextFull(s2, sess, 1<<20, 0, logger))
	if len(reassembled.Messages) == 0 {
		t.Fatal("post-restart assembly is empty")
	}
	if coverageBefore < maxBefore && !msgsContain(reassembled.Messages, fmt.Sprintf("m%d ", maxBefore)) {
		t.Fatalf("post-restart assembly lost the raw uncovered tail (want m%d)", maxBefore)
	}

	// MsgIndex monotonicity across reopen: appending continues after the
	// restored maximum instead of overwriting.
	testutil.NoError(t, s2.AppendMessage(sess, textMsg("user", "재시작 후 새 턴", 99_000)))
	if got := testutil.Must(s2.MaxMsgIndex(sess)); got != maxBefore+1 {
		t.Fatalf("append after reopen: max index = %d, want %d", got, maxBefore+1)
	}
}
