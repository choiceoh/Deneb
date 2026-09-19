package translateops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Production reached the 50k-entry cap. Use synthetic text and timestamps
// shared by small batches, like entries saved during the same provider request.
func benchmarkFullTranslateDiskCache(extra int) *translateDiskCache {
	entries := make(map[string]translateDiskEntry, translateDiskMaxEntries+extra)
	for i := range translateDiskMaxEntries + extra {
		entries[fmt.Sprintf("%064x", i)] = translateDiskEntry{
			Text: strings.Repeat("번역 결과 ", 8),
			Seen: int64(i / 12),
		}
	}
	return &translateDiskCache{
		loaded: true,
		file: translateDiskFile{
			Entries: entries,
			Usage:   map[string]translateDayUsage{},
		},
		lastFlush: time.Now(),
	}
}

func BenchmarkTranslateDiskCacheTrim(b *testing.B) {
	for _, extra := range []int{0, 50} {
		b.Run(fmt.Sprintf("overflow_%d", extra), func(b *testing.B) {
			for b.Loop() {
				b.StopTimer()
				cache := benchmarkFullTranslateDiskCache(extra)
				b.StartTimer()
				cache.trimLocked()
				b.StopTimer()
				if len(cache.file.Entries) != translateDiskMaxEntries {
					b.Fatal("trim did not retain exactly the cache capacity")
				}
				b.StartTimer()
			}
		})
	}
}

// Exercise the browser's entrypoint, including seven provider batches (up to six
// concurrent) and real file writes. Only DeepL HTTP is stubbed, so this measures
// the local delay after translation, not external API or phone/network latency.
func BenchmarkTranslateSegmentsFullDiskCache(b *testing.B) {
	oldDisk, oldClient := translateDisk, deeplHTTPClient
	b.Cleanup(func() {
		translateDisk, deeplHTTPClient = oldDisk, oldClient
		translateTextCache.Clear()
	})
	b.Setenv("DEEPL_API_KEY", "test-key")
	b.Setenv("DEEPL_API_URL", defaultDeepLTranslateURL)
	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		translations := make([]map[string]string, len(r.Form["text"]))
		for i, text := range r.Form["text"] {
			translations[i] = map[string]string{"text": "번역: " + text}
		}
		body, err := json.Marshal(map[string]any{"translations": translations})
		if err != nil {
			return nil, err
		}
		return deepLTestResponse(http.StatusOK, string(body)), nil
	})}
	segments := make([]string, 40)
	for i := range segments {
		segments[i] = fmt.Sprintf("%03d ", i) + strings.Repeat("article text ", 33)
	}
	path := filepath.Join(b.TempDir(), "translate-cache.json")
	for b.Loop() {
		b.StopTimer()
		translateTextCache.Clear()
		translateDisk = benchmarkFullTranslateDiskCache(0)
		translateDisk.pathOverride = path
		b.StartTimer()
		out, err := TranslateSegments(context.Background(), segments, "ko")
		b.StopTimer()
		if err != nil || len(out) != len(segments) {
			b.Fatalf("TranslateSegments: count=%d, err=%v", len(out), err)
		}
		for i, text := range out {
			if text != "번역: "+segments[i] {
				b.Fatalf("segment %d lost or reordered", i)
			}
		}
		b.StartTimer()
	}
}
