package workfeed

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreAppendListAck(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))

	if _, err := store.Append(Item{
		ID:          "old",
		Source:      SourceProactive,
		Title:       "Old",
		Body:        "old body",
		SessionKey:  "client:main",
		CreatedAtMs: 1_000,
	}); err != nil {
		t.Fatalf("append old: %v", err)
	}
	if _, err := store.Append(Item{
		ID:          "new",
		Source:      SourceCaptureImage,
		Title:       "New",
		Summary:     "new summary",
		SessionKey:  "client:main",
		CreatedAtMs: 2_000,
	}); err != nil {
		t.Fatalf("append new: %v", err)
	}

	items, total, err := store.List(10, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total/len = %d/%d, want 2/2", total, len(items))
	}
	if items[0].ID != "new" || items[1].ID != "old" {
		t.Fatalf("order = %q, %q; want newest first", items[0].ID, items[1].ID)
	}
	if items[1].Summary != "old body" {
		t.Fatalf("summary fallback = %q, want body preview", items[1].Summary)
	}
	if len(items[0].Actions) == 0 {
		t.Fatalf("expected default actions")
	}

	acked, err := store.Ack("new")
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acked.Status != StatusAcked {
		t.Fatalf("acked status = %q, want %q", acked.Status, StatusAcked)
	}

	items, total, err = store.List(10, false)
	if err != nil {
		t.Fatalf("list unread: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "old" {
		t.Fatalf("unread list = total %d items %+v, want only old", total, items)
	}
}

func TestStoreAckMissing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Ack("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ack missing err = %v, want ErrNotFound", err)
	}
}

func TestStoreRunActionFollowUpReturnsPrompt(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(Item{
		ID:         "audio",
		Source:     SourceCaptureAudio,
		Title:      "Meeting",
		Body:       "discussed launch",
		SessionKey: "client:main",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	result, err := store.RunAction("audio", ActionFollowUp)
	if err != nil {
		t.Fatalf("run followup: %v", err)
	}
	if result.SessionKey != "client:main" {
		t.Fatalf("sessionKey = %q, want client:main", result.SessionKey)
	}
	if result.Prompt == "" {
		t.Fatalf("expected prompt")
	}
	if result.RemoveFromFeed {
		t.Fatalf("followup should not remove item")
	}
}

func TestStoreRunActionSnoozeHidesItem(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(Item{ID: "item", Body: "body"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	result, err := store.RunAction("item", ActionSnooze)
	if err != nil {
		t.Fatalf("run snooze: %v", err)
	}
	if !result.RemoveFromFeed || result.Item.Status != StatusSnoozed {
		t.Fatalf("result = %+v, want snoozed remove", result)
	}
	items, total, err := store.List(10, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("visible items = total %d items %+v, want none", total, items)
	}
}

func TestStoreRunActionMissing(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(Item{ID: "item", Body: "body"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := store.RunAction("item", "missing"); !errors.Is(err, ErrActionNotFound) {
		t.Fatalf("run missing action err = %v, want ErrActionNotFound", err)
	}
}

func TestPreviewTrimsToFirstLine(t *testing.T) {
	got := Preview(" first line \nsecond line", 100)
	if got != "first line" {
		t.Fatalf("Preview = %q, want first line", got)
	}
}
