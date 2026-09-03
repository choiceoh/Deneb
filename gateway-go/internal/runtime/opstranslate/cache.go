package opstranslate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// The cache is keyed by sha256 of the SOURCE text and stores only the Korean
// rendering — the English original never lands in this file. Entries do not
// expire: the ledgers they describe are append-only and immutable, so a
// translation of row N is as valid a year later as the day it was made.
const (
	cacheFileName    = "ops_translate_cache.json"
	cacheFormat      = 1
	cacheMaxEntries  = 20000
	cacheFlushEvery  = 5 * time.Second
	cacheFileMode    = 0o600
	cacheDirFileMode = 0o700
)

type cacheFile struct {
	Version int               `json:"v"`
	Entries map[string]string `json:"entries"`
}

var store = &diskCache{}

type diskCache struct {
	mu      sync.Mutex
	entries map[string]string
	// order preserves insertion so the cap evicts oldest-first — an append-only
	// ledger's oldest rows are also its least-read.
	order   []string
	loaded  bool
	dirty   bool
	flushAt time.Time
	// pathOverride lets tests point at a temp dir without touching the real one.
	pathOverride string
}

func cacheKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func cacheGet(text string) (string, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loadLocked()
	v, ok := store.entries[cacheKey(text)]
	return v, ok && v != ""
}

func cachePut(text, translated string) {
	if text == "" || translated == "" {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loadLocked()
	key := cacheKey(text)
	if _, exists := store.entries[key]; !exists {
		store.order = append(store.order, key)
	}
	store.entries[key] = translated
	store.dirty = true
	for len(store.order) > cacheMaxEntries {
		delete(store.entries, store.order[0])
		store.order = store.order[1:]
	}
	// Throttled so a burst of misses writes the file once, not once per string.
	if time.Since(store.flushAt) >= cacheFlushEvery {
		store.flushLocked()
	}
}

func (c *diskCache) path() string {
	if c.pathOverride != "" {
		return c.pathOverride
	}
	return filepath.Join(config.ResolveStateDir(), "data", cacheFileName)
}

func (c *diskCache) loadLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.entries = make(map[string]string)
	raw, err := os.ReadFile(c.path())
	if err != nil {
		return // absent or unreadable: an empty cache is correct, just cold
	}
	var f cacheFile
	if json.Unmarshal(raw, &f) != nil || f.Version != cacheFormat {
		return // a corrupt or future-format file is discarded, never half-read
	}
	for k, v := range f.Entries {
		if k == "" || v == "" {
			continue
		}
		c.entries[k] = v
		c.order = append(c.order, k)
	}
}

// flushLocked writes the whole map through a temp file + rename, so a crash
// mid-write leaves the previous cache intact rather than a truncated one.
func (c *diskCache) flushLocked() {
	if !c.dirty {
		return
	}
	c.flushAt = time.Now()
	path := c.path()
	if err := os.MkdirAll(filepath.Dir(path), cacheDirFileMode); err != nil {
		return
	}
	blob, err := json.Marshal(cacheFile{Version: cacheFormat, Entries: c.entries})
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, blob, cacheFileMode) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
		return
	}
	c.dirty = false
}

// Flush writes any pending entries now. The throttle means a quiet gateway can
// hold the last few translations in memory; call this from a shutdown hook.
func Flush() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.flushLocked()
}
