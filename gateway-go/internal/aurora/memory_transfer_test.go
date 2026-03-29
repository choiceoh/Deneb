package aurora

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestShouldTransfer(t *testing.T) {
	cfg := DefaultMemoryTransferConfig()

	tests := []struct {
		name     string
		summary  SummaryRecord
		expected bool
	}{
		{
			name: "condensed summary meets criteria",
			summary: SummaryRecord{
				Depth:      1,
				TokenCount: 200,
			},
			expected: true,
		},
		{
			name: "leaf summary rejected (depth 0)",
			summary: SummaryRecord{
				Depth:      0,
				TokenCount: 500,
			},
			expected: false,
		},
		{
			name: "too few tokens rejected",
			summary: SummaryRecord{
				Depth:      2,
				TokenCount: 50,
			},
			expected: false,
		},
		{
			name: "deep condensed summary accepted",
			summary: SummaryRecord{
				Depth:      3,
				TokenCount: 1000,
			},
			expected: true,
		},
		{
			name: "exactly at threshold",
			summary: SummaryRecord{
				Depth:      1,
				TokenCount: 100,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldTransfer(tt.summary, cfg)
			if got != tt.expected {
				t.Errorf("ShouldTransfer() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTransferredSummaryTracking(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "aurora.json")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, err := NewStore(StoreConfig{DatabasePath: storePath}, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Initially not transferred.
	if store.IsTransferred("summary-1") {
		t.Error("expected summary-1 to NOT be transferred initially")
	}

	// Mark as transferred.
	if err := store.MarkTransferred("summary-1"); err != nil {
		t.Fatalf("MarkTransferred: %v", err)
	}

	if !store.IsTransferred("summary-1") {
		t.Error("expected summary-1 to be transferred after marking")
	}

	// Other summaries still not transferred.
	if store.IsTransferred("summary-2") {
		t.Error("expected summary-2 to NOT be transferred")
	}

	// Flush debounced writes before reloading from disk.
	if err := store.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify persistence: reload store from disk.
	store2, err := NewStore(StoreConfig{DatabasePath: storePath}, logger)
	if err != nil {
		t.Fatalf("NewStore reload: %v", err)
	}
	defer store2.Close()

	if !store2.IsTransferred("summary-1") {
		t.Error("expected summary-1 to persist after reload")
	}
}

func TestTransferredCleanupOnCondensation(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "aurora.json")

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	store, err := NewStore(StoreConfig{DatabasePath: storePath}, logger)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Create leaf summaries and mark them as transferred.
	leaf1 := "leaf-1"
	leaf2 := "leaf-2"
	store.MarkTransferred(leaf1)
	store.MarkTransferred(leaf2)

	// Add them as summaries so PersistCondensedSummary can find context items.
	store.mu.Lock()
	store.data.Summaries[leaf1] = SummaryRecord{SummaryID: leaf1, ConversationID: 1, Kind: "leaf", Depth: 0}
	store.data.Summaries[leaf2] = SummaryRecord{SummaryID: leaf2, ConversationID: 1, Kind: "leaf", Depth: 0}
	store.data.ContextItems = append(store.data.ContextItems,
		ContextItem{ConversationID: 1, Ordinal: 0, ItemType: "summary", SummaryID: &leaf1},
		ContextItem{ConversationID: 1, Ordinal: 1, ItemType: "summary", SummaryID: &leaf2},
	)
	store.mu.Unlock()

	if !store.IsTransferred(leaf1) || !store.IsTransferred(leaf2) {
		t.Fatal("expected leaves to be transferred before condensation")
	}

	// Condense the two leaves into a new summary.
	err = store.PersistCondensedSummary(PersistCondensedInput{
		SummaryID:        "condensed-1",
		ConversationID:   1,
		Depth:            1,
		Content:          "condensed summary content",
		TokenCount:       100,
		ParentSummaryIDs: []string{leaf1, leaf2},
		StartOrdinal:     0,
		EndOrdinal:       1,
	})
	if err != nil {
		t.Fatalf("PersistCondensedSummary: %v", err)
	}

	// Parent transferred entries should be cleaned up.
	if store.IsTransferred(leaf1) {
		t.Error("expected leaf-1 transferred entry to be cleaned up after condensation")
	}
	if store.IsTransferred(leaf2) {
		t.Error("expected leaf-2 transferred entry to be cleaned up after condensation")
	}
}

func TestParseTransferResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCount int
		wantOk    bool
	}{
		{
			name:      "standard facts object",
			input:     `{"facts": [{"content": "테스트 팩트", "category": "decision", "importance": 0.9}]}`,
			wantCount: 1,
			wantOk:    true,
		},
		{
			name:      "bare array",
			input:     `[{"content": "팩트1", "category": "preference", "importance": 0.8}]`,
			wantCount: 1,
			wantOk:    true,
		},
		{
			name:      "empty facts",
			input:     `{"facts": []}`,
			wantCount: 0,
			wantOk:    true,
		},
		{
			name:      "invalid json",
			input:     `not json`,
			wantCount: 0,
			wantOk:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			facts, ok := parseTransferResponse(tt.input)
			if ok != tt.wantOk {
				t.Errorf("ok = %v, want %v", ok, tt.wantOk)
			}
			if len(facts) != tt.wantCount {
				t.Errorf("len(facts) = %d, want %d", len(facts), tt.wantCount)
			}
		})
	}
}

func TestIsValidCategory(t *testing.T) {
	valid := []string{"decision", "preference", "solution", "context", "user_model", "mutual"}
	for _, c := range valid {
		if !isValidCategory(c) {
			t.Errorf("expected %q to be valid", c)
		}
	}

	invalid := []string{"", "unknown", "foo"}
	for _, c := range invalid {
		if isValidCategory(c) {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}
