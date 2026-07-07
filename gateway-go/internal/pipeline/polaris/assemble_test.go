package polaris

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func testAssembleStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := testutil.Must(NewStore(filepath.Join(dir, "test.db")))
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAssembleContextFull_EmptyStore(t *testing.T) {
	store := testAssembleStore(t)

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 48, slog.Default()))
	if len(result.Messages) != 0 {
		t.Fatalf("got %d, want 0 messages for empty store", len(result.Messages))
	}
}

func TestAssembleContextFull_RecentOnly(t *testing.T) {
	store := testAssembleStore(t)

	// Store has messages but no summaries.
	for i := 0; i < 10; i++ {
		store.AppendMessage("s1", textMsg("user", "message", int64(i*1000)))
	}

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 48, slog.Default()))
	if len(result.Messages) != 10 {
		t.Fatalf("got %d, want 10 messages", len(result.Messages))
	}
	if result.WasCompacted {
		t.Fatal("should not be compacted without summaries")
	}
	if result.TotalMessages != 10 {
		t.Fatalf("got %d, want total 10", result.TotalMessages)
	}
}

func TestAssembleContextFull_WithSummaries(t *testing.T) {
	store := testAssembleStore(t)

	// Seed 20 messages.
	for i := 0; i < 20; i++ {
		store.AppendMessage("s1", textMsg("user", "message content here", int64(i*1000)))
	}

	// Insert a summary covering messages 0-9.
	store.InsertSummary(SummaryNode{
		SessionKey: "s1",
		Level:      1,
		Content:    "### 핵심 사실\n- [테스트] 첫 10개 메시지 요약",
		TokenEst:   50,
		CreatedAt:  5000,
		MsgStart:   0,
		MsgEnd:     9,
	})

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 48, slog.Default()))
	if !result.WasCompacted {
		t.Fatal("expected WasCompacted=true with summaries")
	}
	// Should have 1 summary message + 10 recent messages (index 10-19).
	if len(result.Messages) != 11 {
		t.Fatalf("got %d, want 11 messages (1 summary + 10 recent)", len(result.Messages))
	}
}

func TestAssembleContextFull_MultiLevelSummaries(t *testing.T) {
	store := testAssembleStore(t)

	// Seed 30 messages.
	for i := 0; i < 30; i++ {
		store.AppendMessage("s1", textMsg("user", "msg", int64(i*1000)))
	}

	// Two leaf summaries.
	store.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: "leaf 1",
		TokenEst: 30, CreatedAt: 1000, MsgStart: 0, MsgEnd: 9,
	})
	store.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: "leaf 2",
		TokenEst: 30, CreatedAt: 2000, MsgStart: 10, MsgEnd: 19,
	})
	// One condensed summary covering both leaves.
	store.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 2, Content: "condensed summary of 0-19",
		TokenEst: 40, CreatedAt: 3000, MsgStart: 0, MsgEnd: 19,
	})

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 48, slog.Default()))
	if !result.WasCompacted {
		t.Fatal("expected WasCompacted=true")
	}
	// Should prefer the level-2 condensed summary (1 msg) + 10 recent (index 20-29).
	if len(result.Messages) != 11 {
		t.Fatalf("got %d, want 11 messages (1 condensed + 10 recent)", len(result.Messages))
	}
}

func TestAssembleContextFull_DoesNotDropUncoveredRecentAfterSummary(t *testing.T) {
	store := testAssembleStore(t)

	const total = 60
	for i := range total {
		store.AppendMessage("s1", textMsg("user", "m"+strconv.Itoa(i), int64(i*1000)))
	}

	// Existing summary covers only 0-9. Messages 10-59 are not summarized yet,
	// so assembly must keep them raw even though freshTailCount is much smaller.
	store.InsertSummary(SummaryNode{
		SessionKey: "s1",
		Level:      1,
		Content:    "summary 0-9",
		TokenEst:   30,
		CreatedAt:  1000,
		MsgStart:   0,
		MsgEnd:     9,
	})

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 24, slog.Default()))
	if len(result.Messages) != 51 {
		t.Fatalf("got %d, want 51 messages (1 summary + all 50 uncovered recent)", len(result.Messages))
	}

	seen := make(map[string]bool, total)
	for _, msg := range result.Messages {
		var text string
		if json.Unmarshal(msg.Content, &text) == nil {
			seen[text] = true
		}
	}
	for i := 10; i < total; i++ {
		key := "m" + strconv.Itoa(i)
		if !seen[key] {
			t.Fatalf("uncovered message %s was dropped from assembled context", key)
		}
	}
}

func TestAssembleContextFull_TokenBudgetTrimsOldestSummaries(t *testing.T) {
	store := testAssembleStore(t)

	// Seed messages.
	for i := 0; i < 20; i++ {
		store.AppendMessage("s1", textMsg("user", "msg", int64(i*1000)))
	}

	// Insert a summary with huge content.
	bigContent := makeString(60000) // ~30K tokens
	store.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: bigContent,
		TokenEst: 30000, CreatedAt: 1000, MsgStart: 0, MsgEnd: 9,
	})

	// Budget is 1000 tokens — summary should be trimmed.
	result := testutil.Must(assembleContextFull(store, "s1", 1000, 48, slog.Default()))
	// Recent messages should survive even with tight budget.
	if len(result.Messages) == 0 {
		t.Fatal("expected at least some messages")
	}
}

// blockMsg builds a ChatMessage whose Content is a raw content-block array.
func blockMsg(role, blocksJSON string, ts int64) toolctx.ChatMessage {
	return toolctx.ChatMessage{Role: role, Content: json.RawMessage(blocksJSON), Timestamp: ts}
}

func TestAssembleContextFull_RepairsDanglingToolUse(t *testing.T) {
	store := testAssembleStore(t)

	// An interrupted turn: the assistant's tool_use persisted but its
	// tool_result never landed (client abort / hotswap mid-turn). A fresh
	// session (no summaries → the as-is return path) must not ship the orphan —
	// strict OpenAI-compatible providers 400 the whole request on every
	// subsequent turn, wedging the session.
	store.AppendMessage("s1", textMsg("user", "질문", 1000))
	store.AppendMessage("s1", blockMsg("assistant",
		`[{"type":"tool_use","id":"web:0","name":"web","input":{}}]`, 2000))
	store.AppendMessage("s1", textMsg("user", "다시 질문", 3000))

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 48, slog.Default()))
	if len(result.Messages) != 3 {
		t.Fatalf("got %d, want 3 messages", len(result.Messages))
	}
	repaired := string(result.Messages[1].Content)
	if strings.Contains(repaired, "tool_use") {
		t.Fatalf("dangling tool_use survived assembly: %s", repaired)
	}
}

func TestAssembleContextFull_RepairsOrphanToolResultAfterSummary(t *testing.T) {
	store := testAssembleStore(t)

	for i := 0; i < 10; i++ {
		store.AppendMessage("s1", textMsg("user", "old", int64(i*1000)))
	}
	store.InsertSummary(SummaryNode{
		SessionKey: "s1", Level: 1, Content: "summary 0-9",
		TokenEst: 30, CreatedAt: 1000, MsgStart: 0, MsgEnd: 9,
	})
	// The recent window opens on a tool_result whose tool_use was summarized
	// away — the summaries return path must stub it, not ship it.
	store.AppendMessage("s1", blockMsg("user",
		`[{"type":"tool_result","tool_use_id":"web:9","content":"r"}]`, 11_000))
	store.AppendMessage("s1", textMsg("assistant", "답변", 12_000))

	result := testutil.Must(assembleContextFull(store, "s1", 30_000, 48, slog.Default()))
	for _, msg := range result.Messages {
		if strings.Contains(string(msg.Content), "tool_result") {
			t.Fatalf("orphan tool_result survived assembly: %s", string(msg.Content))
		}
	}
}

func TestSelectBestSummaries(t *testing.T) {
	nodes := []SummaryNode{
		{Level: 1, MsgStart: 0, MsgEnd: 9},
		{Level: 1, MsgStart: 10, MsgEnd: 19},
		{Level: 2, MsgStart: 0, MsgEnd: 19},
	}

	selected := selectBestSummaries(nodes, 19)
	if len(selected) != 1 {
		t.Fatalf("got %d, want 1 (condensed)", len(selected))
	}
	if selected[0].Level != 2 {
		t.Fatalf("got %d, want level 2", selected[0].Level)
	}
}
