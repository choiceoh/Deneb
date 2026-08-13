package miniapp

import (
	"strings"
	"testing"
	"time"
)

func TestBatchRequestFingerprintDistinguishesInputs(t *testing.T) {
	base := []captureFileFingerprint{{Filename: "a.pdf", MimeType: "application/pdf", Data: "AAAA"}}
	k := batchRequestFingerprint("client:main", "요약", base)

	// Identical inputs → identical key (an exact resend).
	if k2 := batchRequestFingerprint("client:main", "요약", base); k2 != k {
		t.Errorf("identical inputs should match: %q vs %q", k, k2)
	}
	// Each field participates in the key.
	for name, other := range map[string]string{
		"caption": batchRequestFingerprint("client:main", "다른질문", base),
		"session": batchRequestFingerprint("client:other", "요약", base),
	} {
		if other == k {
			t.Errorf("%s change should change the key", name)
		}
	}
	if batchRequestFingerprint("client:main", "요약", []captureFileFingerprint{{Filename: "b.pdf", MimeType: "application/pdf", Data: "AAAA"}}) == k {
		t.Error("filename change should change the key")
	}
	if batchRequestFingerprint("client:main", "요약", []captureFileFingerprint{{Filename: "a.pdf", MimeType: "application/pdf", Data: "BBBB"}}) == k {
		t.Error("content change should change the key")
	}
}

func TestBatchRequestFingerprintSamplesLargeData(t *testing.T) {
	big := strings.Repeat("x", 3*batchFingerprintSample)
	f := func(data string) string {
		return batchRequestFingerprint("s", "c", []captureFileFingerprint{{Filename: "f", MimeType: "m", Data: data}})
	}
	// Same head+tail+len → same key (sampling middle out), so an exact resend of
	// a large file matches.
	if f(big) != f(big) {
		t.Error("identical large data should match")
	}
	// A different tail → different key.
	if f(big) == f(big[:len(big)-1]+"Z") {
		t.Error("changed tail should change the key")
	}
}

func TestBatchDedupCacheTTLAndEviction(t *testing.T) {
	c := newBatchDedupCache(time.Minute, 2)
	now := time.Unix(1_000_000, 0)
	c.put("a", map[string]any{"v": "a"}, now)

	if got, ok := c.get("a", now.Add(30*time.Second)); !ok || got["v"] != "a" {
		t.Errorf("live entry = %v/%v", got, ok)
	}
	if _, ok := c.get("a", now.Add(2*time.Minute)); ok {
		t.Error("entry should expire past TTL")
	}

	// Capacity 2: a third distinct entry evicts one, never exceeding max.
	c.put("a", map[string]any{}, now)
	c.put("b", map[string]any{}, now)
	c.put("cc", map[string]any{}, now)
	c.mu.Lock()
	size := len(c.m)
	c.mu.Unlock()
	if size > 2 {
		t.Errorf("cache exceeded max: size=%d", size)
	}
}
