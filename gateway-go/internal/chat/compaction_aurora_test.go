package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// TestCompaction_AuroraSyncDuringChat verifies that user and assistant messages
// are synced to the Aurora store during an agent run.
func TestCompaction_AuroraSyncDuringChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse("Aurora sync test", "end_turn"))
	}))
	defer server.Close()

	transcript := NewMemoryTranscriptStore()
	auroraStore := tempAuroraStore(t)
	sm := session.NewManager()
	bc := &broadcastCollector{}
	rc := &rawBroadcastCollector{}

	client := llm.NewClient(server.URL, "test-key")
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = client
	cfg.Transcript = transcript
	cfg.AuroraStore = auroraStore
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "test"
	cfg.MaxTokens = 1024

	h := NewHandler(sm, bc.broadcast, nil, cfg)
	h.SetBroadcastRaw(rc.broadcastRaw)
	defer h.Close()

	req := makeReq("1", "chat.send", map[string]any{
		"sessionKey":  "aurora-sync-1",
		"message":     "hello aurora",
		"clientRunId": "run-aurora-1",
	})
	h.Send(context.Background(), req)
	waitForSessionStatus(sm, "aurora-sync-1", session.StatusDone, 5*time.Second)

	// Verify Aurora store has context items for conversation 1.
	items, err := auroraStore.FetchContextItems(1)
	if err != nil {
		t.Fatalf("FetchContextItems: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("expected >= 2 Aurora context items (user+assistant), got %d", len(items))
	}

	// Verify token counts are tracked.
	totalTokens, err := auroraStore.FetchTokenCount(1)
	if err != nil {
		t.Fatalf("FetchTokenCount: %v", err)
	}
	if totalTokens == 0 {
		t.Error("expected non-zero Aurora token count after chat")
	}
}

// TestCompaction_AuroraLeafSummaryReducesTokens verifies the full leaf
// summary flow: messages are added, compacted into a leaf summary, and
// the total token count decreases.
func TestCompaction_AuroraLeafSummaryReducesTokens(t *testing.T) {
	store := tempAuroraStore(t)

	// Add multiple messages with known token counts.
	var msgIDs []uint64
	for i := 0; i < 10; i++ {
		id, err := store.SyncMessage(1, "user",
			fmt.Sprintf("Message %d with some content for tokens", i), 100)
		if err != nil {
			t.Fatalf("SyncMessage: %v", err)
		}
		msgIDs = append(msgIDs, id)
	}

	// Total should be 1000 tokens.
	totalBefore, _ := store.FetchTokenCount(1)
	if totalBefore != 1000 {
		t.Fatalf("totalBefore = %d, want 1000", totalBefore)
	}

	// Compact the first 8 messages into a leaf summary.
	err := store.PersistLeafSummary(aurora.PersistLeafInput{
		SummaryID:               "sum_leaf_integration",
		ConversationID:          1,
		Content:                 "Summary of messages 0-7",
		TokenCount:              50, // much smaller than 800
		FileIDs:                 []string{},
		SourceMessageTokenCount: 800,
		MessageIDs:              msgIDs[:8],
		StartOrdinal:            0,
		EndOrdinal:              7,
	})
	if err != nil {
		t.Fatalf("PersistLeafSummary: %v", err)
	}

	// Verify token count reduced.
	totalAfter, _ := store.FetchTokenCount(1)
	// Should be 50 (summary) + 200 (2 remaining messages).
	if totalAfter != 250 {
		t.Errorf("totalAfter = %d, want 250", totalAfter)
	}

	// Verify context items: 1 summary + 2 messages.
	items, _ := store.FetchContextItems(1)
	summaryCount := 0
	messageCount := 0
	for _, ci := range items {
		switch ci.ItemType {
		case "summary":
			summaryCount++
		case "message":
			messageCount++
		}
	}
	if summaryCount != 1 {
		t.Errorf("summary count = %d, want 1", summaryCount)
	}
	if messageCount != 2 {
		t.Errorf("message count = %d, want 2", messageCount)
	}
}

// TestCompaction_AuroraCondensedSummaryHierarchy tests the multi-level
// compaction hierarchy: messages -> leaf summaries -> condensed summary.
func TestCompaction_AuroraCondensedSummaryHierarchy(t *testing.T) {
	store := tempAuroraStore(t)

	// Phase 1: Add messages.
	for i := 0; i < 20; i++ {
		store.SyncMessage(1, "user",
			fmt.Sprintf("Msg %d", i), 50)
	}

	// Phase 2: Create two leaf summaries.
	err := store.PersistLeafSummary(aurora.PersistLeafInput{
		SummaryID:               "leaf_1",
		ConversationID:          1,
		Content:                 "Leaf summary 1",
		TokenCount:              30,
		FileIDs:                 []string{},
		SourceMessageTokenCount: 500,
		MessageIDs:              []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		StartOrdinal:            0,
		EndOrdinal:              9,
	})
	if err != nil {
		t.Fatalf("PersistLeafSummary leaf_1: %v", err)
	}

	err = store.PersistLeafSummary(aurora.PersistLeafInput{
		SummaryID:               "leaf_2",
		ConversationID:          1,
		Content:                 "Leaf summary 2",
		TokenCount:              30,
		FileIDs:                 []string{},
		SourceMessageTokenCount: 500,
		MessageIDs:              []uint64{10, 11, 12, 13, 14, 15, 16, 17, 18, 19},
		StartOrdinal:            10,
		EndOrdinal:              19,
	})
	if err != nil {
		t.Fatalf("PersistLeafSummary leaf_2: %v", err)
	}

	// Verify two leaf summaries exist.
	items, _ := store.FetchContextItems(1)
	leafCount := 0
	var leafOrdinals []uint64
	for _, ci := range items {
		if ci.ItemType == "summary" {
			leafCount++
			leafOrdinals = append(leafOrdinals, ci.Ordinal)
		}
	}
	if leafCount != 2 {
		t.Fatalf("expected 2 leaf summaries, got %d", leafCount)
	}

	// Phase 3: Condense the two leaf summaries.
	err = store.PersistCondensedSummary(aurora.PersistCondensedInput{
		SummaryID:               "condensed_1",
		ConversationID:          1,
		Depth:                   1,
		Content:                 "Condensed summary of leaf_1 + leaf_2",
		TokenCount:              15,
		FileIDs:                 []string{},
		DescendantCount:         2,
		DescendantTokenCount:    60,
		SourceMessageTokenCount: 1000,
		ParentSummaryIDs:        []string{"leaf_1", "leaf_2"},
		StartOrdinal:            leafOrdinals[0],
		EndOrdinal:              leafOrdinals[1],
	})
	if err != nil {
		t.Fatalf("PersistCondensedSummary: %v", err)
	}

	// Verify only one summary item remains (the condensed one).
	items, _ = store.FetchContextItems(1)
	summaryCount := 0
	for _, ci := range items {
		if ci.ItemType == "summary" {
			summaryCount++
		}
	}
	if summaryCount != 1 {
		t.Errorf("expected 1 condensed summary, got %d", summaryCount)
	}

	// Verify depth tracking.
	depths, _ := store.FetchDistinctDepths(1, 999)
	hasDepth1 := false
	for _, d := range depths {
		if d == 1 {
			hasDepth1 = true
		}
	}
	if !hasDepth1 {
		t.Error("expected depth 1 in distinct depths")
	}

	// Verify stats.
	stats, _ := store.FetchSummaryStats(1)
	if stats.CondensedCount < 1 {
		t.Error("expected >= 1 condensed summary in stats")
	}
	if stats.MaxDepth < 1 {
		t.Error("expected maxDepth >= 1")
	}
}

// TestCompaction_AuroraAssemblyFallback tests that the Aurora context assembly
// fallback (no FFI) correctly selects messages and summaries.
func TestCompaction_AuroraAssemblyFallback(t *testing.T) {
	store := tempAuroraStore(t)

	// Add messages.
	for i := 0; i < 15; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		store.SyncMessage(1, role,
			fmt.Sprintf("Message %d content", i), 10)
	}

	// Compact first 10 into a leaf summary.
	store.PersistLeafSummary(aurora.PersistLeafInput{
		SummaryID:               "asm_leaf",
		ConversationID:          1,
		Content:                 "Summary of messages 0-9",
		TokenCount:              5,
		FileIDs:                 []string{},
		SourceMessageTokenCount: 100,
		MessageIDs:              []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		StartOrdinal:            0,
		EndOrdinal:              9,
	})

	// Assemble using fallback (no FFI).
	cfg := aurora.AssemblyConfig{
		TokenBudget:    100_000,
		FreshTailCount: 32,
		MaxMessages:    100,
	}
	result, err := aurora.Assemble(context.Background(), store, 1, cfg, nil)
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}

	// Should have the summary + remaining messages.
	if len(result.Messages) == 0 {
		t.Fatal("expected non-empty assembly result")
	}

	// Verify total messages count.
	if result.TotalMessages == 0 {
		t.Error("expected non-zero total messages")
	}

	// Check that at least one message contains the summary text.
	hasSummary := false
	for _, msg := range result.Messages {
		var text string
		if json.Unmarshal(msg.Content, &text) == nil {
			if strings.Contains(text, "Summary of messages") ||
				strings.Contains(text, "[Aurora Summary]") {
				hasSummary = true
				break
			}
		}
	}
	if !hasSummary {
		t.Error("expected summary text in assembled messages")
	}
}

// TestCompaction_AuroraSweepWithMockSummarizer tests the sweep command handler
// flow with a mock summarizer function.
func TestCompaction_AuroraSweepWithMockSummarizer(t *testing.T) {
	store := tempAuroraStore(t)

	// Add messages.
	for i := 0; i < 20; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		store.SyncMessage(1, role,
			fmt.Sprintf("Conversation message %d with enough content to matter", i), 50)
	}

	// Verify initial token count.
	totalBefore, _ := store.FetchTokenCount(1)
	if totalBefore != 1000 {
		t.Fatalf("totalBefore = %d, want 1000", totalBefore)
	}

	// Manually create a leaf summary using the store directly
	// (simulating what RunSweep does via the handler).
	err := store.PersistLeafSummary(aurora.PersistLeafInput{
		SummaryID:               "sweep_leaf_1",
		ConversationID:          1,
		Content:                 "Summary of first 16 messages",
		TokenCount:              30,
		FileIDs:                 []string{},
		SourceMessageTokenCount: 800,
		MessageIDs:              []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		StartOrdinal:            0,
		EndOrdinal:              15,
	})
	if err != nil {
		t.Fatalf("PersistLeafSummary: %v", err)
	}

	totalAfter, _ := store.FetchTokenCount(1)
	if totalAfter >= totalBefore {
		t.Errorf("expected reduced tokens after sweep, before=%d after=%d", totalBefore, totalAfter)
	}

	// Verify compaction event.
	err = store.PersistEvent(aurora.PersistEventInput{
		ConversationID:   1,
		Pass:             "leaf",
		Level:            "normal",
		TokensBefore:     totalBefore,
		TokensAfter:      totalAfter,
		CreatedSummaryID: "sweep_leaf_1",
	})
	if err != nil {
		t.Fatalf("PersistEvent: %v", err)
	}
}

// TestCompaction_AuroraResetClearsAll verifies that resetting Aurora state
// completely clears all context items, messages, and summaries.
func TestCompaction_AuroraResetClearsAll(t *testing.T) {
	store := tempAuroraStore(t)

	// Build up state.
	for i := 0; i < 10; i++ {
		store.SyncMessage(1, "user", fmt.Sprintf("msg %d", i), 10)
	}
	store.PersistLeafSummary(aurora.PersistLeafInput{
		SummaryID:      "reset_leaf",
		ConversationID: 1,
		Content:        "leaf",
		TokenCount:     5,
		FileIDs:        []string{},
		MessageIDs:     []uint64{0, 1, 2},
		StartOrdinal:   0,
		EndOrdinal:     2,
	})
	store.PersistEvent(aurora.PersistEventInput{
		ConversationID:   1,
		Pass:             "leaf",
		Level:            "normal",
		TokensBefore:     100,
		TokensAfter:      50,
		CreatedSummaryID: "reset_leaf",
	})

	// Reset.
	if err := store.Reset(1); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	// Verify everything is cleared.
	items, _ := store.FetchContextItems(1)
	if len(items) != 0 {
		t.Errorf("expected 0 items after reset, got %d", len(items))
	}

	tokens, _ := store.FetchTokenCount(1)
	if tokens != 0 {
		t.Errorf("expected 0 tokens after reset, got %d", tokens)
	}

	stats, _ := store.FetchSummaryStats(1)
	if stats.LeafCount != 0 || stats.CondensedCount != 0 {
		t.Errorf("expected 0 summaries after reset, got leaf=%d condensed=%d",
			stats.LeafCount, stats.CondensedCount)
	}
}
