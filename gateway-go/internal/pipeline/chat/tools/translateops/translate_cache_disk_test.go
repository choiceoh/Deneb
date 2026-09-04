package translateops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// useTempDiskCache points the durable layer at a temp file for one test.
func useTempDiskCache(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "translate-cache.json")
	prev := translateDisk
	translateDisk = &translateDiskCache{pathOverride: path}
	resetTranslateTextCache()
	t.Cleanup(func() {
		translateDisk = prev
		resetTranslateTextCache()
	})
	return path
}

func TestTranslationSurvivesAnEmptyHotSet(t *testing.T) {
	// The LRU is 4k entries shared with page and operator-screen translation and
	// is emptied by every restart. A translation already paid for must not be
	// bought again just because it fell out.
	useTempDiskCache(t)
	rememberTranslated("KO", "the attachment path", "첨부 경로")
	translateDisk.flush()

	translateTextCache.Clear() // evicted from the hot set, file intact
	got, ok := translateCached("KO", "the attachment path")
	if !ok || got != "첨부 경로" {
		t.Fatalf("translateCached = %q, %v; want the durable copy", got, ok)
	}
	// And it is promoted back into the hot set rather than re-read every time.
	if hit, ok := translateTextCache.Get(translateCacheKey("KO", "the attachment path")); !ok || hit != "첨부 경로" {
		t.Fatal("a disk hit was not promoted into the LRU")
	}
}

func TestDurableCacheSurvivesAProcessRestart(t *testing.T) {
	path := useTempDiskCache(t)
	rememberTranslated("KO", "restart me", "재시작")
	translateDisk.flush()

	// A new process: nothing in memory, only the file. (Clear just the hot set —
	// the full reset deletes the file, which is the opposite of this test.)
	translateDisk = &translateDiskCache{pathOverride: path}
	translateTextCache.Clear()
	if got, ok := translateCached("KO", "restart me"); !ok || got != "재시작" {
		t.Fatalf("after restart translateCached = %q, %v", got, ok)
	}
}

func TestDurableCacheKeepsTheNewestWhenTrimmed(t *testing.T) {
	useTempDiskCache(t)
	translateDisk.mu.Lock()
	translateDisk.loadLocked()
	for i := 0; i < translateDiskMaxEntries+50; i++ {
		id := string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune(i))
		translateDisk.file.Entries[id] = translateDiskEntry{Text: "x", Seen: int64(i)}
	}
	translateDisk.trimLocked()
	n := len(translateDisk.file.Entries)
	_, oldestKept := translateDisk.file.Entries[string(rune('a'))+string(rune('a'))+string(rune(0))]
	translateDisk.mu.Unlock()

	if n > translateDiskMaxEntries {
		t.Fatalf("entries = %d, want <= %d", n, translateDiskMaxEntries)
	}
	if oldestKept {
		t.Fatal("trim kept the oldest entry")
	}
}

func TestUsageCountsOnlyWhatWasSent(t *testing.T) {
	// The instrument the plan does not give us: characters billed. Cache hits
	// must not appear in it or the number is worthless.
	path := useTempDiskCache(t)
	translateDisk.recordUsage(120, 1)
	translateDisk.recordUsage(80, 1)
	translateDisk.flush()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("usage file not written: %v", err)
	}
	var file translateDiskFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("usage file unreadable: %v", err)
	}
	today := file.Usage[time.Now().Format("2006-01-02")]
	if today.Chars != 200 || today.Requests != 2 {
		t.Fatalf("today = %+v, want 200 chars over 2 requests", today)
	}
}

func TestDurableCacheToleratesAUnreadableFile(t *testing.T) {
	// A corrupt file must degrade to "no durable layer", never to a failure:
	// translation still works, it just pays again.
	path := useTempDiskCache(t)
	if err := os.WriteFile(path, []byte("{ this is not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := translateCached("KO", "anything"); ok {
		t.Fatal("a corrupt file produced a hit")
	}
	rememberTranslated("KO", "anything", "무엇이든")
	if got, ok := translateCached("KO", "anything"); !ok || got != "무엇이든" {
		t.Fatalf("cache unusable after a corrupt file: %q, %v", got, ok)
	}
}
