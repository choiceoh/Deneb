package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// Batch idempotency: the native client retries miniapp.capture.batch on a
// transient failure, and users re-share the same files — which without dedup
// re-extracts every file, runs a SECOND agent turn, and files a duplicate
// work-feed card. A short-TTL fingerprint→response cache replays the first
// result for an identical resend instead. (In-flight duplicates — a retry
// arriving before the first turn finishes — are NOT deduped; retries in practice
// follow a completion or a timeout, and single-flighting a live agent turn is a
// bigger hammer than this pain warrants.)
const (
	batchDedupTTL = 2 * time.Minute
	batchDedupMax = 64
	// batchFingerprintSample bounds how many head/tail bytes of each (base64)
	// file feed the fingerprint, so a 25MB attachment doesn't cost a full hash.
	// An identical resend still matches exactly (same name/mime/len/head/tail);
	// two files differing only in a sampled-over middle would collide, which for
	// accidental-resend dedup is acceptable.
	batchFingerprintSample = 16 * 1024
)

// captureFileFingerprint is the per-file input to the dedup key — cheap string
// headers copied from the decoded request (no file-byte copy).
type captureFileFingerprint struct {
	Filename, MimeType, Data string
}

// batchRequestFingerprint keys the dedup cache: session + caption + each file's
// name/mime/size and sampled head+tail bytes.
func batchRequestFingerprint(sessionKey, caption string, files []captureFileFingerprint) string {
	h := sha256.New()
	h.Write([]byte(sessionKey))
	h.Write([]byte{0})
	h.Write([]byte(caption))
	for _, f := range files {
		h.Write([]byte{0})
		fmt.Fprintf(h, "%s|%s|%d|", f.Filename, f.MimeType, len(f.Data))
		if len(f.Data) <= 2*batchFingerprintSample {
			h.Write([]byte(f.Data))
			continue
		}
		h.Write([]byte(f.Data[:batchFingerprintSample]))
		h.Write([]byte(f.Data[len(f.Data)-batchFingerprintSample:]))
	}
	return hex.EncodeToString(h.Sum(nil))
}

type batchDedupEntry struct {
	payload   map[string]any
	expiresAt time.Time
}

// batchDedupCache is a bounded, TTL'd fingerprint→response cache, safe for
// concurrent use.
type batchDedupCache struct {
	mu  sync.Mutex
	ttl time.Duration
	max int
	m   map[string]batchDedupEntry
}

func newBatchDedupCache(ttl time.Duration, max int) *batchDedupCache {
	return &batchDedupCache{ttl: ttl, max: max, m: make(map[string]batchDedupEntry)}
}

// get returns the cached response for key when present and unexpired.
func (c *batchDedupCache) get(key string, now time.Time) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return nil, false
	}
	if now.After(e.expiresAt) {
		delete(c.m, key)
		return nil, false
	}
	return e.payload, true
}

// put stores a response under key, evicting expired (then the soonest-expiring)
// entries when at capacity.
func (c *batchDedupCache) put(key string, payload map[string]any, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.m[key]; !exists && len(c.m) >= c.max {
		var soonestKey string
		var soonestExp time.Time
		for k, e := range c.m {
			if now.After(e.expiresAt) {
				delete(c.m, k)
				continue
			}
			if soonestExp.IsZero() || e.expiresAt.Before(soonestExp) {
				soonestKey, soonestExp = k, e.expiresAt
			}
		}
		if len(c.m) >= c.max && soonestKey != "" {
			delete(c.m, soonestKey)
		}
	}
	c.m[key] = batchDedupEntry{payload: payload, expiresAt: now.Add(c.ttl)}
}
