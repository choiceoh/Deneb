package workfeed

import (
	"errors"
	"path/filepath"
	"strings"
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

func TestStoreCorrect(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))

	if _, err := store.Append(Item{
		ID:          "c1",
		Source:      SourceMailReport,
		Title:       "거래처 분석",
		Body:        "담당자는 김 부장입니다.",
		SessionKey:  "client:main",
		CreatedAtMs: 1_000,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	out, err := store.Correct("c1", "담당자는 이서연 차장입니다.")
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	// The correction is appended (the original analysis is kept, not replaced) and
	// carries the erratum marker; UpdatedAtMs is bumped.
	if !strings.Contains(out.Body, "이서연 차장") {
		t.Fatalf("corrected body missing note: %q", out.Body)
	}
	if !strings.Contains(out.Body, "사용자 정정") {
		t.Fatalf("corrected body missing marker: %q", out.Body)
	}
	if !strings.Contains(out.Body, "담당자는 김 부장입니다.") {
		t.Fatalf("corrected body dropped the original analysis: %q", out.Body)
	}
	if out.UpdatedAtMs == 0 {
		t.Fatalf("UpdatedAtMs not bumped")
	}

	// The correction persists across a reload.
	items, _, err := store.List(10, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || !strings.Contains(items[0].Body, "이서연 차장") {
		t.Fatalf("persisted body = %+v, want the correction", items)
	}

	// A blank note finds the card but makes no body change (handler guards blank
	// feedback anyway; this just must not error or duplicate the block).
	if _, err := store.Correct("c1", "   "); err != nil {
		t.Fatalf("blank-note correct: %v", err)
	}

	// Correcting a missing card reports not-found.
	if _, err := store.Correct("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("correct missing = %v, want ErrNotFound", err)
	}
}

func TestStoreListRangeFiltersBeforeLimit(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	for i := 0; i < 5; i++ {
		if _, err := store.Append(Item{
			ID:          "urgent-old-" + string(rune('a'+i)),
			Source:      SourceProactive,
			Title:       "old urgent",
			Body:        "긴급 오래된 카드",
			Priority:    PriorityUrgent,
			Status:      StatusUnread,
			CreatedAtMs: int64(1_000 + i),
		}); err != nil {
			t.Fatalf("append old urgent: %v", err)
		}
	}
	if _, err := store.Append(Item{
		ID:          "today-normal",
		Source:      SourceMailReport,
		Title:       "today mail",
		Body:        "보통 오늘 메일",
		Priority:    PriorityNormal,
		Status:      StatusUnread,
		CreatedAtMs: 10_000,
	}); err != nil {
		t.Fatalf("append today: %v", err)
	}

	items, total, err := store.ListRange(1, false, 9_000, 11_000)
	if err != nil {
		t.Fatalf("list range: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "today-normal" {
		t.Fatalf("range list = total %d items %+v, want today-normal only", total, items)
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

func TestStoreRunActionOpenReturnsContextPrompt(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(Item{
		ID:         "report",
		Source:     SourceProactive,
		Title:      "Daily Report",
		Summary:    "launch summary",
		Body:       "blocker: design review",
		SessionKey: "client:main",
		RefType:    "mail",
		RefID:      "msg_1",
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	result, err := store.RunAction("report", ActionOpen)
	if err != nil {
		t.Fatalf("run open: %v", err)
	}
	if result.SessionKey != "client:main" {
		t.Fatalf("sessionKey = %q, want client:main", result.SessionKey)
	}
	if result.Prompt == "" {
		t.Fatalf("expected prompt")
	}
	for _, want := range []string{"Daily Report", "mail / msg_1", "blocker: design review"} {
		if !strings.Contains(result.Prompt, want) {
			t.Fatalf("prompt = %q, want %q", result.Prompt, want)
		}
	}
	if result.RemoveFromFeed {
		t.Fatalf("open should not remove item")
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

func TestStoreRunActionAckSettlesDuplicateIDs(t *testing.T) {
	// Legacy feeds (old restart-resetting id counter) could hold several items
	// under one id. Acking via RunAction must settle every twin, not just the
	// first match, or the survivors stay unread and the card re-surfaces on the
	// next List — the "zombie" work-feed item the native app hit.
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	for _, body := range []string{"first twin", "second twin", "third twin"} {
		if _, err := store.Append(Item{
			ID:         "wf_0004",
			Source:     SourceProactive,
			Body:       body,
			SessionKey: "client:main",
		}); err != nil {
			t.Fatalf("append %q: %v", body, err)
		}
	}
	if _, total, err := store.List(10, false); err != nil || total != 3 {
		t.Fatalf("pre-ack visible = total %d err %v, want 3", total, err)
	}
	result, err := store.RunAction("wf_0004", ActionAck)
	if err != nil {
		t.Fatalf("run ack: %v", err)
	}
	if !result.RemoveFromFeed || result.Item.Status != StatusAcked {
		t.Fatalf("result = %+v, want acked remove", result)
	}
	items, total, err := store.List(10, false)
	if err != nil {
		t.Fatalf("post-ack list: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("after ack, visible = total %d items %+v, want none (no zombie)", total, items)
	}
}

func TestStoreRunActionTrashDeletes(t *testing.T) {
	// 휴지통 permanently removes a card. It is a universal action (not in the item's
	// action list), settles every id twin, and the item never re-surfaces — even
	// with includeAcked=true, since it's deleted, not acked.
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(Item{ID: "wf_keep", Source: SourceProactive, Body: "keep me", SessionKey: "client:main"}); err != nil {
		t.Fatalf("append keep: %v", err)
	}
	for _, body := range []string{"twin a", "twin b"} {
		if _, err := store.Append(Item{ID: "wf_trash", Source: SourceProactive, Body: body, SessionKey: "client:main"}); err != nil {
			t.Fatalf("append %q: %v", body, err)
		}
	}
	result, err := store.RunAction("wf_trash", ActionTrash)
	if err != nil {
		t.Fatalf("run trash: %v", err)
	}
	if !result.RemoveFromFeed || result.Message != "deleted" {
		t.Fatalf("result = %+v, want deleted+remove", result)
	}
	items, total, err := store.List(10, true) // includeAcked: a deleted item must not reappear
	if err != nil {
		t.Fatalf("post-trash list: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "wf_keep" {
		t.Fatalf("after trash, items = %+v (total %d), want only wf_keep", items, total)
	}
}

func TestStoreRunActionSnoozeSettlesDuplicateIDs(t *testing.T) {
	// Snooze, like ack, is id-scoped and must hide every twin sharing the id.
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	for _, body := range []string{"twin a", "twin b"} {
		if _, err := store.Append(Item{ID: "wf_0007", Body: body}); err != nil {
			t.Fatalf("append %q: %v", body, err)
		}
	}
	if _, err := store.RunAction("wf_0007", ActionSnooze); err != nil {
		t.Fatalf("run snooze: %v", err)
	}
	if _, total, err := store.List(10, false); err != nil || total != 0 {
		t.Fatalf("after snooze, visible = total %d err %v, want none", total, err)
	}
}

func TestPreviewTrimsToFirstLine(t *testing.T) {
	got := Preview(" first line \nsecond line", 100)
	if got != "first line" {
		t.Fatalf("Preview = %q, want first line", got)
	}
}

func mustAppendIfNew(t *testing.T, s *Store, it Item) bool {
	t.Helper()
	_, created, err := s.AppendIfNew(it)
	if err != nil {
		t.Fatalf("AppendIfNew: %v", err)
	}
	return created
}

func TestStoreDedupsConsecutiveIdentical(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	item := Item{Source: SourceProactive, Body: "동일한 분석 본문"}

	if created := mustAppendIfNew(t, store, item); !created {
		t.Fatal("first append created=false, want true")
	}
	if created := mustAppendIfNew(t, store, item); created {
		t.Error("second identical append created=true, want false (dedup)")
	}
	_, total, err := store.List(10, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 (duplicate suppressed)", total)
	}
}

func TestStoreDistinctBodiesNotDeduped(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	mustAppendIfNew(t, store, Item{Source: SourceProactive, Body: "본문 A"})
	mustAppendIfNew(t, store, Item{Source: SourceProactive, Body: "본문 B"})
	if _, total, _ := store.List(10, false); total != 2 {
		t.Errorf("total = %d, want 2 (distinct bodies)", total)
	}
}

func TestStoreDifferentSourceNotDeduped(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	mustAppendIfNew(t, store, Item{Source: SourceProactive, Body: "같은 본문"})
	if created := mustAppendIfNew(t, store, Item{Source: SourceCaptureImage, Body: "같은 본문"}); !created {
		t.Error("different source with identical body deduped, want created=true")
	}
}

func TestStoreEmptyBodyNotDeduped(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	mustAppendIfNew(t, store, Item{Source: SourceCaptureImage, Title: "공유 이미지 A"})
	if created := mustAppendIfNew(t, store, Item{Source: SourceCaptureImage, Title: "공유 이미지 B"}); !created {
		t.Error("empty-body cards deduped, want created=true (distinct cards must not collapse)")
	}
}

func TestStoreProactiveEmptyTitleFallsBack(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	if _, err := store.Append(Item{Source: SourceProactive, Title: "", Body: "본문"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	items, _, err := store.List(10, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 || items[0].Title != "업무 리포트" {
		t.Fatalf("title = %q, want 업무 리포트", items[0].Title)
	}
}
