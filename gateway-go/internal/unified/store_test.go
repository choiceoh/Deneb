package unified

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestUnifiedStore_FactTriggersKeepSearchInSync(t *testing.T) {
	dir := t.TempDir()
	store, err := New(Config{
		DatabasePath: filepath.Join(dir, "deneb.db"),
	}, nil)
	if err != nil {
		t.Fatalf("new unified store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := store.DB().Exec(
		`INSERT INTO facts (content, category, importance, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"search trigger coverage for unified fact recall",
		"context",
		0.92,
		"manual",
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert fact: %v", err)
	}
	factID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}

	results, err := store.Search(ctx, "trigger coverage", SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("search after insert: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected search results after insert")
	}
	wantID := strconv.FormatInt(factID, 10)
	found := false
	for _, r := range results {
		if r.ItemType == "fact" && r.ItemID == wantID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected fact result %s, got %#v", wantID, results)
	}

	if _, err := store.DB().Exec(
		`UPDATE facts SET active = 0, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339),
		factID,
	); err != nil {
		t.Fatalf("deactivate fact: %v", err)
	}

	results, err = store.Search(ctx, "trigger coverage", SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("search after deactivate: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected fact to disappear after deactivate, got %#v", results)
	}
}

func TestUnifiedStore_RepairBackfillsMissingIndexRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "deneb.db")

	store, err := New(Config{DatabasePath: dbPath}, nil)
	if err != nil {
		t.Fatalf("new unified store: %v", err)
	}

	_, err = store.DB().Exec(
		`INSERT INTO messages (message_id, conversation_id, seq, role, content, token_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 1, 1, "assistant", "historical note about memory index repair", 10, time.Now().Add(-3*time.Hour).UnixMilli(),
	)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	if _, err := store.DB().Exec(`DELETE FROM memory_index`); err != nil {
		t.Fatalf("clear memory_index: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := New(Config{DatabasePath: dbPath}, nil)
	if err != nil {
		t.Fatalf("reopen unified store: %v", err)
	}
	defer reopened.Close()

	results, err := reopened.Search(context.Background(), "memory index repair", SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("search after repair: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected repaired index to restore search results")
	}
	if results[0].ItemType != "message" {
		t.Fatalf("expected message result after repair, got %#v", results)
	}
}
