package translateops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
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

// A page's lookups and inserts must not wait on the disk. Until 2026-09-19 a
// flush held the cache lock through the trim, the marshal and the write — at
// the 50k cap about a second each, several per page. Hold the write lock as a
// slow disk would and the cache still answers.
func TestLookupsDoNotWaitOnADiskWrite(t *testing.T) {
	useTempDiskCache(t)
	rememberTranslated("KO", "already paid", "이미 낸 번역")
	translateDisk.flush() // lastFlush now: the next few inserts stay under the threshold

	translateDisk.writeMu.Lock() // a write of the whole file is in progress
	done := make(chan struct{})
	go func() {
		defer close(done)
		key := translateCacheKey("KO", "new line")
		translateDisk.put(key, "새 줄")
		if got, ok := translateDisk.get(translateCacheKey("KO", "already paid")); !ok || got != "이미 낸 번역" {
			t.Errorf("get = %q, %v", got, ok)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a lookup waited on the disk write")
	}
	translateDisk.writeMu.Unlock()
}

// Six batches of one page flush at once. Only the newest state is worth a
// write: an older snapshot that reaches the disk after a newer one was taken is
// skipped, and one that would land after a newer write is refused.
func TestAnOlderSnapshotNeverOverwritesANewerOne(t *testing.T) {
	path := useTempDiskCache(t)
	rememberTranslated("KO", "warm", "데움") // the first insert writes at once (no flush yet)
	translateDisk.flush()
	if err := os.Remove(path); err != nil { // watch only the writes below
		t.Fatal(err)
	}

	rememberTranslated("KO", "first", "첫째")
	translateDisk.mu.Lock()
	older := translateDisk.snapshotLocked()
	translateDisk.mu.Unlock()

	rememberTranslated("KO", "second", "둘째")
	translateDisk.mu.Lock()
	newer := translateDisk.snapshotLocked()
	translateDisk.mu.Unlock()

	translateDisk.write(older) // a newer snapshot exists: skipped
	if _, err := os.Stat(path); err == nil {
		t.Fatal("the older snapshot was written while a newer one was pending")
	}
	translateDisk.write(newer)
	translateDisk.write(older) // after the newer landed: refused
	onDisk := &translateDiskCache{pathOverride: path}
	for text, want := range map[string]string{"first": "첫째", "second": "둘째"} {
		if got, ok := onDisk.get(translateCacheKey("KO", text)); !ok || got != want {
			t.Errorf("%q on disk = %q, %v", text, got, ok)
		}
	}
}

// Concurrent batches inserting and flushing leave every translation on disk.
func TestConcurrentFlushesLandEverything(t *testing.T) {
	path := useTempDiskCache(t)
	var wg sync.WaitGroup
	for batch := 0; batch < 6; batch++ {
		wg.Add(1)
		go func(batch int) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				rememberTranslated("KO", fmt.Sprintf("line %d-%d", batch, i), fmt.Sprintf("줄 %d-%d", batch, i))
			}
			translateDisk.flush()
		}(batch)
	}
	wg.Wait()
	translateDisk.flush()

	onDisk := &translateDiskCache{pathOverride: path}
	for batch := 0; batch < 6; batch++ {
		for i := 0; i < 60; i++ {
			if _, ok := onDisk.get(translateCacheKey("KO", fmt.Sprintf("line %d-%d", batch, i))); !ok {
				t.Fatalf("line %d-%d is not on disk", batch, i)
			}
		}
	}
}

// The trim runs on every flush once the file is full; it must stay cheap there.
func BenchmarkTrimAtTheCap(b *testing.B) {
	c := &translateDiskCache{pathOverride: filepath.Join(b.TempDir(), "translate-cache.json")}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	for i := 0; i < translateDiskMaxEntries; i++ {
		c.file.Entries[fmt.Sprintf("%064x", i)] = translateDiskEntry{Text: "x", Seen: int64(i % 5000)}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for k := 0; k < 20; k++ {
			c.file.Entries[fmt.Sprintf("new-%d-%d", n, k)] = translateDiskEntry{Text: "x", Seen: int64(10000 + n)}
		}
		c.trimLocked()
	}
}

// One page's RPC against a full durable cache, with an instant DeepL: what the
// translation costs on our side alone. 40 fresh ~120-char segments is two
// provider batches — the client's per-RPC budget (BrowserTranslationRequest).
func BenchmarkTranslateAPageAtTheCap(b *testing.B) {
	c := &translateDiskCache{pathOverride: filepath.Join(b.TempDir(), "translate-cache.json")}
	c.mu.Lock()
	c.loadLocked()
	for i := 0; i < translateDiskMaxEntries; i++ {
		c.file.Entries[fmt.Sprintf("%064x", i)] = translateDiskEntry{Text: "x", Seen: int64(i % 5000)}
	}
	c.mu.Unlock()
	prev, prevClient := translateDisk, deeplHTTPClient
	translateDisk = c
	b.Cleanup(func() { translateDisk, deeplHTTPClient = prev, prevClient })
	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		_ = r.ParseForm()
		out := make([]map[string]string, 0, len(r.Form["text"]))
		for _, text := range r.Form["text"] {
			out = append(out, map[string]string{"text": "번역 " + text})
		}
		body, _ := json.Marshal(map[string]any{"translations": out})
		return deepLTestResponse(http.StatusOK, string(body)), nil
	})}
	b.Setenv("DEEPL_API_KEY", "test-key")
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		segments := make([]string, 40) // fresh every round: all misses, as on a new page
		for i := range segments {
			segments[i] = fmt.Sprintf("Bench %d-%d: the northern depot received %d crates of solar inverters this morning, and the invoice is due on Friday.", n, i, i+3)
		}
		if _, err := TranslateSegments(context.Background(), segments, "ko"); err != nil {
			b.Fatal(err)
		}
	}
}
