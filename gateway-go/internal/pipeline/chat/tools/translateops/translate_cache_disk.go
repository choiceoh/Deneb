package translateops

import (
	"cmp"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
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

// translateDiskCache is the durable layer. Every page translation's lookups and
// inserts go through mu, so nothing slow may run under it: a flush copies the
// file under mu (snapshotLocked, ~25ms at the 50k cap) and marshals and writes
// the copy after releasing it (write). Until 2026-09-19 the whole flush ran
// under mu — at the cap its trim alone took 0.9s (an insertion sort over the
// real 50k-entry file), every paid batch and every 50th insert flushed, and
// every lookup queued behind them: one page's RPC spent 1.95s on our side even
// with an instant provider, 0.06s after (BenchmarkTranslateAPageAtTheCap).
//
// Lock hierarchy: mu and writeMu are never held together. mu guards the
// in-memory file; writeMu orders the disk writes of snapshots.
type translateDiskCache struct {
	mu        sync.Mutex
	loaded    bool
	file      translateDiskFile
	dirty     int
	lastFlush time.Time
	// pathOverride lets tests keep their writes out of the state dir.
	pathOverride string

	writeMu sync.Mutex
	// snapSeq numbers snapshots as they are taken; wroteSeq (under writeMu) is
	// the newest one on disk. A writer whose snapshot is older than the newest
	// taken skips — the newer one's taker writes it — so a burst of flushes
	// costs one write, and the newest state always lands.
	snapSeq  atomic.Uint64
	wroteSeq uint64
}

// translateDiskSnapshot is a copy of the file taken under mu, written after.
type translateDiskSnapshot struct {
	seq  uint64
	file translateDiskFile
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
	c.loadLocked()
	id := hex.EncodeToString(key[:])
	if _, exists := c.file.Entries[id]; exists {
		c.mu.Unlock()
		return
	}
	c.file.Entries[id] = translateDiskEntry{Text: text, Seen: time.Now().Unix()}
	c.dirty++
	snap := c.maybeSnapshotLocked()
	c.mu.Unlock()
	c.write(snap)
}

// recordUsage tallies what was actually sent to the provider — cache hits cost
// nothing and are deliberately not counted.
func (c *translateDiskCache) recordUsage(chars, requests int) {
	if chars <= 0 && requests <= 0 {
		return
	}
	c.mu.Lock()
	c.loadLocked()
	day := time.Now().Format("2006-01-02")
	u := c.file.Usage[day]
	u.Chars += chars
	u.Requests += requests
	c.file.Usage[day] = u
	c.dirty++
	c.reportPreviousDayLocked(day)
	snap := c.maybeSnapshotLocked()
	c.mu.Unlock()
	c.write(snap)
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

// maybeSnapshotLocked is the write threshold: a snapshot to write once enough
// is new or it has waited long enough, nil otherwise.
func (c *translateDiskCache) maybeSnapshotLocked() *translateDiskSnapshot {
	if c.dirty == 0 {
		return nil
	}
	if c.dirty >= translateDiskFlushEvery || time.Since(c.lastFlush) >= translateDiskFlushAfter {
		return c.snapshotLocked()
	}
	return nil
}

// snapshotLocked trims the file to the newest entries and the last two months
// of usage and copies it for write, which runs after mu is released.
func (c *translateDiskCache) snapshotLocked() *translateDiskSnapshot {
	c.dirty = 0
	c.lastFlush = time.Now()
	c.trimLocked()
	entries := make(map[string]translateDiskEntry, len(c.file.Entries))
	for id, e := range c.file.Entries {
		entries[id] = e
	}
	usage := make(map[string]translateDayUsage, len(c.file.Usage))
	for day, u := range c.file.Usage {
		usage[day] = u
	}
	return &translateDiskSnapshot{
		seq:  c.snapSeq.Add(1),
		file: translateDiskFile{Entries: entries, Usage: usage, LoggedDay: c.file.LoggedDay},
	}
}

// write lands a snapshot on disk, never under mu. Written to a temp file and
// renamed so a crash mid-write cannot leave a half-parsed cache behind.
func (c *translateDiskCache) write(snap *translateDiskSnapshot) {
	if snap == nil {
		return
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if snap.seq < c.snapSeq.Load() || snap.seq <= c.wroteSeq {
		return // a newer snapshot exists; its taker writes it
	}
	raw, err := json.Marshal(snap.file)
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
		return
	}
	c.wroteSeq = snap.seq
}

func (c *translateDiskCache) trimLocked() {
	if over := len(c.file.Entries) - translateDiskMaxEntries; over > 0 {
		// Drop the oldest by last-seen. This runs on every flush once the file
		// is full, so it must stay O(n log n): an insertion sort here cost 0.9s
		// per flush at 50k entries (map order is random — its worst case).
		type aged struct {
			id   string
			seen int64
		}
		all := make([]aged, 0, len(c.file.Entries))
		for id, e := range c.file.Entries {
			all = append(all, aged{id, e.Seen})
		}
		slices.SortFunc(all, func(a, b aged) int {
			if c := cmp.Compare(a.seen, b.seen); c != 0 {
				return c
			}
			return cmp.Compare(a.id, b.id)
		})
		for _, a := range all[:over] {
			delete(c.file.Entries, a.id)
		}
	}
	cutoff := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	for day := range c.file.Usage {
		if day < cutoff {
			delete(c.file.Usage, day)
		}
	}
}

// flush writes what is new now instead of at the next threshold: the provider
// path calls it after every paid batch (translateBatchDeepL). Nothing new since
// the last snapshot means nothing to write — that snapshot's taker writes it.
func (c *translateDiskCache) flush() {
	c.mu.Lock()
	if !c.loaded || c.dirty == 0 {
		c.mu.Unlock()
		return
	}
	snap := c.snapshotLocked()
	c.mu.Unlock()
	c.write(snap)
}
