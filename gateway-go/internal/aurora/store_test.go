package aurora

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	cfg := StoreConfig{DatabasePath: filepath.Join(dir, "aurora.json")}
	s, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSyncMessage(t *testing.T) {
	s := tempStore(t)

	id1, err := s.SyncMessage(1, "user", "hello", 2)
	if err != nil {
		t.Fatalf("SyncMessage: %v", err)
	}

	id2, err := s.SyncMessage(1, "assistant", "hi there", 3)
	if err != nil {
		t.Fatalf("SyncMessage: %v", err)
	}

	if id1 == id2 {
		t.Error("expected different message IDs")
	}

	// Verify context items.
	items, err := s.FetchContextItems(1)
	if err != nil {
		t.Fatalf("FetchContextItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 context items, got %d", len(items))
	}
	if items[0].ItemType != "message" || items[1].ItemType != "message" {
		t.Error("expected message item types")
	}
	if items[0].Ordinal >= items[1].Ordinal {
		t.Error("expected ascending ordinals")
	}
}

func TestFetchMessages(t *testing.T) {
	s := tempStore(t)

	id1, _ := s.SyncMessage(1, "user", "hello world", 3)
	id2, _ := s.SyncMessage(1, "assistant", "hi!", 2)

	msgs, err := s.FetchMessages([]uint64{id1, id2, 999})
	if err != nil {
		t.Fatalf("FetchMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[id1].Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", msgs[id1].Content)
	}
	if msgs[id2].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", msgs[id2].Role)
	}
}

func TestFetchTokenCount(t *testing.T) {
	s := tempStore(t)

	s.SyncMessage(1, "user", "hello", 10)
	s.SyncMessage(1, "assistant", "world", 20)
	s.SyncMessage(2, "user", "other conv", 100) // different conversation

	total, err := s.FetchTokenCount(1)
	if err != nil {
		t.Fatalf("FetchTokenCount: %v", err)
	}
	if total != 30 {
		t.Errorf("expected 30 tokens, got %d", total)
	}
}

func TestPersistLeafSummary(t *testing.T) {
	s := tempStore(t)

	// Create messages.
	id1, _ := s.SyncMessage(1, "user", "msg1", 10)
	id2, _ := s.SyncMessage(1, "user", "msg2", 10)
	s.SyncMessage(1, "assistant", "msg3 (kept)", 10) // ordinal 2, kept

	items, _ := s.FetchContextItems(1)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Persist leaf summary replacing first two messages.
	err := s.PersistLeafSummary(PersistLeafInput{
		SummaryID:               "sum_leaf_001",
		ConversationID:          1,
		Content:                 "summary of msg1+msg2",
		TokenCount:              5,
		FileIDs:                 []string{},
		SourceMessageTokenCount: 20,
		MessageIDs:              []uint64{id1, id2},
		StartOrdinal:            0,
		EndOrdinal:              1,
	})
	if err != nil {
		t.Fatalf("PersistLeafSummary: %v", err)
	}

	// Verify context items: should have 1 summary + 1 message.
	items, _ = s.FetchContextItems(1)
	if len(items) != 2 {
		t.Fatalf("expected 2 items after compaction, got %d", len(items))
	}
	if items[0].ItemType != "summary" {
		t.Errorf("expected summary at position 0, got %s", items[0].ItemType)
	}
	if items[1].ItemType != "message" {
		t.Errorf("expected message at position 1, got %s", items[1].ItemType)
	}

	// Verify summary record.
	sums, _ := s.FetchSummaries([]string{"sum_leaf_001"})
	if len(sums) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(sums))
	}
	if sums["sum_leaf_001"].Kind != "leaf" {
		t.Errorf("expected leaf kind, got %s", sums["sum_leaf_001"].Kind)
	}
	if sums["sum_leaf_001"].TokenCount != 5 {
		t.Errorf("expected 5 tokens, got %d", sums["sum_leaf_001"].TokenCount)
	}

	// Token count should be reduced.
	total, _ := s.FetchTokenCount(1)
	if total != 15 { // 5 (summary) + 10 (msg3)
		t.Errorf("expected 15 tokens after compaction, got %d", total)
	}
}

func TestPersistCondensedSummary(t *testing.T) {
	s := tempStore(t)

	// Set up with leaf summaries already in place.
	s.SyncMessage(1, "user", "early msg", 10)

	// Manually add leaf summary context items.
	sid1 := "sum_leaf_001"
	sid2 := "sum_leaf_002"

	s.PersistLeafSummary(PersistLeafInput{
		SummaryID:      sid1,
		ConversationID: 1,
		Content:        "leaf1",
		TokenCount:     5,
		FileIDs:        []string{},
		MessageIDs:     []uint64{0},
		StartOrdinal:   0,
		EndOrdinal:     0,
	})

	s.SyncMessage(1, "user", "mid msg", 10)
	items, _ := s.FetchContextItems(1)

	s.PersistLeafSummary(PersistLeafInput{
		SummaryID:      sid2,
		ConversationID: 1,
		Content:        "leaf2",
		TokenCount:     5,
		FileIDs:        []string{},
		MessageIDs:     []uint64{},
		StartOrdinal:   items[len(items)-1].Ordinal,
		EndOrdinal:     items[len(items)-1].Ordinal,
	})

	// Condense the two leaf summaries.
	items, _ = s.FetchContextItems(1)
	summaryItems := 0
	var startOrd, endOrd uint64
	for _, ci := range items {
		if ci.ItemType == "summary" {
			summaryItems++
			if summaryItems == 1 {
				startOrd = ci.Ordinal
			}
			endOrd = ci.Ordinal
		}
	}

	err := s.PersistCondensedSummary(PersistCondensedInput{
		SummaryID:        "sum_cond_001",
		ConversationID:   1,
		Depth:            1,
		Content:          "condensed of leaf1+leaf2",
		TokenCount:       3,
		FileIDs:          []string{},
		ParentSummaryIDs: []string{sid1, sid2},
		StartOrdinal:     startOrd,
		EndOrdinal:       endOrd,
	})
	if err != nil {
		t.Fatalf("PersistCondensedSummary: %v", err)
	}

	// Verify depths.
	depths, _ := s.FetchDistinctDepths(1, 999)
	found := false
	for _, d := range depths {
		if d == 1 {
			found = true
		}
	}
	if !found {
		t.Error("expected depth 1 in distinct depths")
	}

	// Verify stats.
	stats, _ := s.FetchSummaryStats(1)
	if stats.CondensedCount < 1 {
		t.Error("expected at least 1 condensed summary")
	}
}

func TestReset(t *testing.T) {
	s := tempStore(t)

	s.SyncMessage(1, "user", "hello", 5)
	s.SyncMessage(1, "assistant", "world", 5)

	err := s.Reset(1)
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}

	items, _ := s.FetchContextItems(1)
	if len(items) != 0 {
		t.Errorf("expected 0 items after reset, got %d", len(items))
	}

	total, _ := s.FetchTokenCount(1)
	if total != 0 {
		t.Errorf("expected 0 tokens after reset, got %d", total)
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aurora.json")
	cfg := StoreConfig{DatabasePath: path}

	// Create store and add data.
	s1, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	s1.SyncMessage(1, "user", "persisted", 5)
	s1.Close()

	// Reopen and verify.
	s2, err := NewStore(cfg, nil)
	if err != nil {
		t.Fatalf("NewStore reopen: %v", err)
	}
	defer s2.Close()

	items, _ := s2.FetchContextItems(1)
	if len(items) != 1 {
		t.Fatalf("expected 1 item after reopen, got %d", len(items))
	}

	msgs, _ := s2.FetchMessages([]uint64{0})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message after reopen, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.Content != "persisted" {
			t.Errorf("expected 'persisted', got %q", m.Content)
		}
	}
}

func TestPersistEvent(t *testing.T) {
	s := tempStore(t)

	err := s.PersistEvent(PersistEventInput{
		ConversationID:   1,
		Pass:             "leaf",
		Level:            "normal",
		TokensBefore:     1000,
		TokensAfter:      500,
		CreatedSummaryID: "sum_001",
	})
	if err != nil {
		t.Fatalf("PersistEvent: %v", err)
	}

	// Force debounced flush to disk before checking.
	if err := s.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify file written.
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		t.Error("expected store file to exist after persist")
	}
}

func TestFetchEmptyCollections(t *testing.T) {
	s := tempStore(t)

	items, err := s.FetchContextItems(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}

	msgs, err := s.FetchMessages(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Errorf("expected empty messages, got %d", len(msgs))
	}

	sums, err := s.FetchSummaries(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 0 {
		t.Errorf("expected empty summaries, got %d", len(sums))
	}

	depths, err := s.FetchDistinctDepths(1, 999)
	if err != nil {
		t.Fatal(err)
	}
	if len(depths) != 0 {
		t.Errorf("expected empty depths, got %d", len(depths))
	}

	total, err := s.FetchTokenCount(1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("expected 0 tokens, got %d", total)
	}
}
