// web_fetch_tier.go — per-domain stealth-tier memory.
//
// The stealth ladder always started at stage 0, so domains that ALWAYS need a
// higher tier (bot-walled portals, SPA-shell link shorteners) re-paid the
// failing lower stages on every fetch — extra latency AND repeated
// probe-then-fail traffic that raises bot suspicion. This memory records the
// stage that last worked per domain and starts the next fetch there (wigolo's
// per-domain tier self-tuning idea, reviewed 2026-07-21).
//
// Decay contract: entries expire after tierTTL without a success; once per
// tierProbeInterval a fetch starts one stage LOWER than learned, so a domain
// that became easy again drifts back down instead of paying the expensive
// tier forever. A stage-0 success deletes the entry.
package web

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const (
	// tierTTL drops a learned tier that hasn't been confirmed by a success.
	tierTTL = 7 * 24 * time.Hour
	// tierProbeInterval paces the decay probe (one stage lower than learned).
	tierProbeInterval = 24 * time.Hour
	// tierMaxEntries bounds the map; oldest-success entries evict first.
	tierMaxEntries = 256
	// tierFileName lives under DENEB_STATE_DIR (memory-only when unset).
	tierFileName = "web-stealth-tiers.json"
)

// tierEntry is one learned domain tier. Exported fields for JSON persistence.
type tierEntry struct {
	Stage         int   `json:"stage"`
	LastSuccessMs int64 `json:"lastSuccessMs"`
	LastProbeMs   int64 `json:"lastProbeMs"`
	Hits          int   `json:"hits"`
}

// tierMemory is a mutex-guarded domain→tier map with JSON persistence.
type tierMemory struct {
	mu      sync.Mutex
	entries map[string]*tierEntry
	path    string // "" = in-memory only
}

// stealthTiers is the process-wide tier memory (package-level state like
// sharedTransport — the web package has no constructor-injected deps).
var stealthTiers = newTierMemory(tierStatePath())

func tierStatePath() string {
	dir := strings.TrimSpace(os.Getenv("DENEB_STATE_DIR"))
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, tierFileName)
}

// newTierMemory loads the persisted map (missing/corrupt file = start empty).
func newTierMemory(path string) *tierMemory {
	tm := &tierMemory{entries: map[string]*tierEntry{}, path: path}
	if path == "" {
		return tm
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tm
	}
	var stored map[string]*tierEntry
	if err := json.Unmarshal(data, &stored); err != nil {
		slog.Warn("stealth tier memory unreadable, starting empty", "path", path, "error", err)
		return tm
	}
	for host, e := range stored {
		if e != nil && e.Stage > 0 {
			tm.entries[host] = e
		}
	}
	return tm
}

// startStage returns the ladder index to start from for host, applying decay:
// expired entries reset to 0, and once per probe interval the fetch starts one
// stage lower than learned so recovered domains drift back down.
func (tm *tierMemory) startStage(host string, now time.Time) int {
	if host == "" {
		return 0
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	e := tm.entries[host]
	if e == nil {
		return 0
	}
	nowMs := now.UnixMilli()
	if nowMs-e.LastSuccessMs > tierTTL.Milliseconds() {
		delete(tm.entries, host)
		tm.saveLocked()
		return 0
	}
	if nowMs-e.LastProbeMs > tierProbeInterval.Milliseconds() {
		e.LastProbeMs = nowMs
		tm.saveLocked()
		if e.Stage > 0 {
			return e.Stage - 1
		}
	}
	return e.Stage
}

// recordSuccess stores the stage that worked. Stage 0 means the plain profile
// worked — the domain no longer needs escalation, so its entry is dropped.
func (tm *tierMemory) recordSuccess(host string, stage int, now time.Time) {
	if host == "" {
		return
	}
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if stage <= 0 {
		if _, ok := tm.entries[host]; ok {
			delete(tm.entries, host)
			tm.saveLocked()
		}
		return
	}
	nowMs := now.UnixMilli()
	e := tm.entries[host]
	isNew := e == nil
	if isNew {
		e = &tierEntry{}
		tm.entries[host] = e
	}
	if e.Stage != stage {
		// Fresh tier: reset the probe clock so the very next fetch doesn't
		// immediately re-pay the stage we just watched fail.
		e.LastProbeMs = nowMs
	}
	e.Stage = stage
	e.LastSuccessMs = nowMs
	e.Hits++
	if isNew {
		// Evict only after the new entry carries its real timestamp, so the
		// cap check never mistakes the newcomer for the oldest entry.
		tm.evictLocked()
	}
	tm.saveLocked()
}

// evictLocked drops the oldest-success entry once the cap is exceeded.
func (tm *tierMemory) evictLocked() {
	if len(tm.entries) <= tierMaxEntries {
		return
	}
	oldestHost, oldestMs := "", int64(1)<<62
	for host, e := range tm.entries {
		if e.LastSuccessMs < oldestMs {
			oldestMs, oldestHost = e.LastSuccessMs, host
		}
	}
	if oldestHost != "" {
		delete(tm.entries, oldestHost)
	}
}

// saveLocked persists the map; failures degrade to memory-only (Debug level:
// the next save retries, and a lost tier map only costs re-learning).
func (tm *tierMemory) saveLocked() {
	if tm.path == "" {
		return
	}
	data, err := json.Marshal(tm.entries)
	if err != nil {
		return
	}
	if err := atomicfile.WriteFile(tm.path, data, nil); err != nil {
		slog.Debug("stealth tier memory save failed", "path", tm.path, "error", err)
	}
}
