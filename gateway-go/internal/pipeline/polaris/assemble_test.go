package polaris

import (
	"log/slog"
	"path/filepath"
	"testing"

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
