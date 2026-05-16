package wiki

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDiarySearch_BasicRoundTrip — indexed entry is reachable by query token.
func TestDiarySearch_BasicRoundTrip(t *testing.T) {
	d := newDiarySearchDB()
	d.upsertEntry("diary-2026-05-16.md", "10:30",
		"오늘 chat pipeline 의 recall cache 를 수정했다.",
		time.Now().UnixMilli())

	hits, err := d.search(context.Background(), "recall", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(hits))
	}
	if hits[0].File != "diary-2026-05-16.md" || hits[0].Header != "10:30" {
		t.Errorf("hit mismatch: %+v", hits[0])
	}
}

// TestDiarySearch_EmptyQueryReturnsNil keeps the API contract loose.
func TestDiarySearch_EmptyQueryReturnsNil(t *testing.T) {
	d := newDiarySearchDB()
	d.upsertEntry("diary-2026-05-16.md", "10:30", "anything", time.Now().UnixMilli())
	hits, err := d.search(context.Background(), "", 5)
	if err != nil {
		t.Errorf("empty query should be error-free, got %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("empty query should return no hits, got %d", len(hits))
	}
}

// TestDiarySearch_RecencyOutranksOld — same BM25 match, fresher entry wins.
func TestDiarySearch_RecencyOutranksOld(t *testing.T) {
	d := newDiarySearchDB()
	now := time.Now().UnixMilli()
	old := now - 60*24*60*60*1000 // 60 days ago

	d.upsertEntry("diary-2026-03-17.md", "10:30",
		"chat pipeline recall 작업 시작", old)
	d.upsertEntry("diary-2026-05-16.md", "10:30",
		"chat pipeline recall 작업 마무리", now)

	hits, err := d.search(context.Background(), "recall", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].File != "diary-2026-05-16.md" {
		t.Errorf("recent entry should rank first, got %q", hits[0].File)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("recent score %.4f should exceed old score %.4f", hits[0].Score, hits[1].Score)
	}
}

// TestDiarySearch_RecentEntriesFallback — when no query terms match, the
// caller can ask for recent entries instead.
func TestDiarySearch_RecentEntriesFallback(t *testing.T) {
	d := newDiarySearchDB()
	now := time.Now().UnixMilli()
	d.upsertEntry("diary-2026-05-14.md", "08:00", "older entry", now-2*24*60*60*1000)
	d.upsertEntry("diary-2026-05-15.md", "08:00", "middle entry", now-1*24*60*60*1000)
	d.upsertEntry("diary-2026-05-16.md", "08:00", "newest entry", now)

	hits := d.recentEntries(2)
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	if hits[0].File != "diary-2026-05-16.md" {
		t.Errorf("first hit should be newest, got %q", hits[0].File)
	}
	if hits[1].File != "diary-2026-05-15.md" {
		t.Errorf("second hit should be middle, got %q", hits[1].File)
	}
}

// TestDiarySearch_RebuildFromDir reads multi-file diary state from disk.
func TestDiarySearch_RebuildFromDir(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"diary-2026-05-15.md": "\n## 09:00\n\nfirst entry on tuesday\n\n## 14:30\n\nsecond tuesday entry, recall topic\n",
		"diary-2026-05-16.md": "\n## 11:00\n\nwednesday entry about recall\n",
		"not-a-diary.md":      "\n## 10:00\n\nshould be ignored\n", // non-diary file
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	d := newDiarySearchDB()
	if err := d.rebuildFromDir(dir); err != nil {
		t.Fatalf("rebuildFromDir: %v", err)
	}

	hits, err := d.search(context.Background(), "recall", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits for 'recall', want 2 (one from each diary file)", len(hits))
	}
}

// TestDiarySearch_KoreanPrefixMatch — textsearch handles Hangul prefix; verify
// it still works end-to-end through diary search.
func TestDiarySearch_KoreanPrefixMatch(t *testing.T) {
	d := newDiarySearchDB()
	d.upsertEntry("diary-2026-05-16.md", "10:30",
		"위키 페이지 회상 파이프라인을 정리했다.", time.Now().UnixMilli())

	// Korean prefix tokens should match "회상" matching the prefix of "회상".
	hits, err := d.search(context.Background(), "회상", 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Errorf("Korean query should match indexed entry")
	}
}

// TestParseDiaryFile_BasicSections splits "## " sections correctly.
func TestParseDiaryFile_BasicSections(t *testing.T) {
	body := "\n## 09:00\n\nfirst body\n\n## 10:30\n\nsecond body\nwith continuation\n"
	entries := parseDiaryFile("diary-2026-05-16.md", body)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Header != "09:00" || entries[0].Content != "first body" {
		t.Errorf("first entry mismatch: %+v", entries[0])
	}
	if entries[1].Header != "10:30" || entries[1].Content != "second body\nwith continuation" {
		t.Errorf("second entry mismatch: %+v", entries[1])
	}
}

// TestDiaryEntryUnixMillis — filename + header → unix millis.
func TestDiaryEntryUnixMillis(t *testing.T) {
	got := diaryEntryUnixMillis("diary-2026-05-16.md", "10:30")
	if got <= 0 {
		t.Fatalf("expected positive timestamp, got %d", got)
	}
	// Unparseable input returns 0.
	if z := diaryEntryUnixMillis("diary-bad.md", "??:??"); z != 0 {
		t.Errorf("unparseable input should yield 0, got %d", z)
	}
}

// TestStore_AppendDiary_UpdatesIndex — Store.AppendDiary persists AND indexes
// so a freshly written entry is searchable immediately (no restart required).
// This is the headline guarantee of routing through Store rather than the
// standalone AppendDiaryTo helper.
func TestStore_AppendDiary_UpdatesIndex(t *testing.T) {
	wikiDir := t.TempDir()
	diaryDir := t.TempDir()
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.AppendDiary("오늘 recall pipeline 작업했다"); err != nil {
		t.Fatalf("AppendDiary: %v", err)
	}

	hits, err := store.SearchDiary(context.Background(), "recall", 5)
	if err != nil {
		t.Fatalf("SearchDiary: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1 — Store.AppendDiary should update the index live", len(hits))
	}
}

// TestStore_AppendDiaryTo_NoLiveIndex documents the known limitation that the
// standalone AppendDiaryTo helper (used by gmailpoll and morning_letter) does
// NOT touch the in-memory index — its entries are only searchable after the
// next gateway restart. Captured here so a future refactor that fixes it can
// flip this assertion intentionally.
func TestStore_AppendDiaryTo_NoLiveIndex(t *testing.T) {
	wikiDir := t.TempDir()
	diaryDir := t.TempDir()
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := AppendDiaryTo(diaryDir, "이건 standalone 으로 쓴 일지"); err != nil {
		t.Fatalf("AppendDiaryTo: %v", err)
	}

	hits, err := store.SearchDiary(context.Background(), "standalone", 5)
	if err != nil {
		t.Fatalf("SearchDiary: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("standalone AppendDiaryTo is expected NOT to update the live index; got %d hits", len(hits))
	}
}
