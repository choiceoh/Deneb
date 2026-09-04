package translateops

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// The in-memory LRU is a hot set, not the archive. Two things made that a
// problem once chat reasoning started streaming through here:
//
//   - It holds 4k entries shared with page translation and the operator-screen
//     translator, so a single translated web page evicts the reasoning lines a
//     conversation just paid for.
//   - It is emptied by every restart, and deploys are frequent. A cold cache is
//     precisely when the persisted reasoning display copy misses its one-second
//     budget — and that copy has no second chance, so those turns read English
//     forever.
//
// So the LRU keeps a durable layer behind it: every translation ever paid for,
// on disk, consulted on an LRU miss.
const (
	// Short lines, one JSON file. 50k of them is a few MB — cheap next to
	// re-buying a translation at €20 per million characters.
	translateDiskMaxEntries = 50000
	// Writes are batched: the file is rewritten once per this many new entries
	// rather than per translation. A crash loses at most this many lookups,
	// which costs a re-translation, not correctness.
	translateDiskFlushEvery = 50
	// …and never sits unwritten longer than this. One chat turn is well under
	// the entry threshold, so without a clock the durable layer would keep
	// losing exactly the turns a restart is about to need.
	translateDiskFlushAfter = 30 * time.Second
	translateDiskFileName   = "translate-cache.json"
)

type translateDiskEntry struct {
	Text string `json:"t"`
	// Seconds. Used to keep the newest entries when the file is trimmed.
	Seen int64 `json:"s"`
}

// translateDayUsage is what the deployment actually spends. DeepL's /v2/usage
// reports only document counts on this plan, so characters billed are otherwise
// invisible — this is the only place the number exists.
type translateDayUsage struct {
	Chars    int `json:"chars"`
	Requests int `json:"requests"`
}

type translateDiskFile struct {
	Entries map[string]translateDiskEntry `json:"entries"`
	Usage   map[string]translateDayUsage  `json:"usage"`
	// LoggedDay is the last day already reported to the operator log, so a
	// restart does not repeat a day's summary.
	LoggedDay string `json:"loggedDay,omitempty"`
}

var translateDisk = &translateDiskCache{}

type translateDiskCache struct {
	mu        sync.Mutex
	loaded    bool
	file      translateDiskFile
	dirty     int
	lastFlush time.Time
	// pathOverride lets tests keep their writes out of the state dir.
	pathOverride string
}

func (c *translateDiskCache) path() string {
	if c.pathOverride != "" {
		return c.pathOverride
	}
	return filepath.Join(config.ResolveStateDir(), translateDiskFileName)
}

// loadLocked reads the file once. A missing or corrupt file is not an error:
// the cache simply starts empty, which is what it did before it had a disk.
func (c *translateDiskCache) loadLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.file = translateDiskFile{
		Entries: map[string]translateDiskEntry{},
		Usage:   map[string]translateDayUsage{},
	}
	raw, err := os.ReadFile(c.path())
	if err != nil {
		return
	}
	var loaded translateDiskFile
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return
	}
	if loaded.Entries != nil {
		c.file.Entries = loaded.Entries
	}
	if loaded.Usage != nil {
		c.file.Usage = loaded.Usage
	}
	c.file.LoggedDay = loaded.LoggedDay
}

func (c *translateDiskCache) get(key [32]byte) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	e, ok := c.file.Entries[hex.EncodeToString(key[:])]
	if !ok {
		return "", false
	}
	return e.Text, true
}

func (c *translateDiskCache) put(key [32]byte, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	id := hex.EncodeToString(key[:])
	if _, exists := c.file.Entries[id]; exists {
		return
	}
	c.file.Entries[id] = translateDiskEntry{Text: text, Seen: time.Now().Unix()}
	c.dirty++
	c.maybeFlushLocked()
}

// recordUsage tallies what was actually sent to the provider — cache hits cost
// nothing and are deliberately not counted.
func (c *translateDiskCache) recordUsage(chars, requests int) {
	if chars <= 0 && requests <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	day := time.Now().Format("2006-01-02")
	u := c.file.Usage[day]
	u.Chars += chars
	u.Requests += requests
	c.file.Usage[day] = u
	c.dirty++
	c.reportPreviousDayLocked(day)
	c.maybeFlushLocked()
}

// reportPreviousDayLocked logs yesterday's total once, the first time a new day
// spends anything. One line a day is the whole instrument.
func (c *translateDiskCache) reportPreviousDayLocked(today string) {
	if c.file.LoggedDay == today {
		return
	}
	prev := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if u, ok := c.file.Usage[prev]; ok && u.Chars > 0 && c.file.LoggedDay != today {
		slog.Default().Info("deepl translation usage",
			"day", prev, "chars", u.Chars, "requests", u.Requests)
	}
	c.file.LoggedDay = today
	c.dirty++
}

func (c *translateDiskCache) maybeFlushLocked() {
	if c.dirty == 0 {
		return
	}
	if c.dirty >= translateDiskFlushEvery || time.Since(c.lastFlush) >= translateDiskFlushAfter {
		c.flushLocked()
	}
}

// flushLocked rewrites the file, trimmed to the newest entries and the last two
// months of usage. Written to a temp file and renamed so a crash mid-write
// cannot leave a half-parsed cache behind.
func (c *translateDiskCache) flushLocked() {
	c.dirty = 0
	c.lastFlush = time.Now()
	c.trimLocked()
	raw, err := json.Marshal(c.file)
	if err != nil {
		return
	}
	path := c.path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func (c *translateDiskCache) trimLocked() {
	if over := len(c.file.Entries) - translateDiskMaxEntries; over > 0 {
		// Drop the oldest by last-seen. A linear pass is fine at this size and
		// runs once per flush, not per lookup.
		type aged struct {
			id   string
			seen int64
		}
		all := make([]aged, 0, len(c.file.Entries))
		for id, e := range c.file.Entries {
			all = append(all, aged{id, e.Seen})
		}
		for i := 1; i < len(all); i++ {
			for j := i; j > 0 && all[j].seen < all[j-1].seen; j-- {
				all[j], all[j-1] = all[j-1], all[j]
			}
		}
		for i := 0; i < over && i < len(all); i++ {
			delete(c.file.Entries, all[i].id)
		}
	}
	cutoff := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	for day := range c.file.Usage {
		if day < cutoff {
			delete(c.file.Usage, day)
		}
	}
}

// flush writes pending state out. Tests call it; production reaches it through
// the write threshold.
func (c *translateDiskCache) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		return
	}
	c.flushLocked()
}
