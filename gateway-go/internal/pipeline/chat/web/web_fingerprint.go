// web_fingerprint.go — "what changed since last time" for a re-fetched page.
//
// Re-fetching a page the model already read spends its full token cost again to
// learn, usually, that nothing moved. Keeping a per-section hash of what was
// served lets the next fetch say so: unchanged, or changed in these sections.
// Only titles and hashes are kept — bounded and tiny next to the content cache,
// so a page can be recognised long after its content entry has aged out.
package web

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// fingerprintMemoryMax bounds the store. Sections are ~40 bytes each here, so
// this stays far below the content cache while covering many more pages.
const fingerprintMemoryMax = 512

const fingerprintFileName = "web-page-fingerprints.json"

type sectionFingerprint struct {
	Title string
	Hash  uint64
}

// pageRecord is one page's shape plus the budget it was extracted under.
//
// The budget is part of the identity, not decoration: maxChars caps the bytes
// downloaded, so the same URL fetched with different budgets yields different
// section SETS. Comparing across budgets reported every section as new — a
// false "changed" is worse than saying nothing, so a differing budget is
// treated as a first visit.
type pageRecord struct {
	Budget   int64                `json:"budget"`
	Sections []sectionFingerprint `json:"sections"`
}

type fingerprintMemory struct {
	mu    sync.Mutex
	pages map[string]pageRecord
	order []string
	path  string
}

var pageFingerprints = newFingerprintMemory(fingerprintStatePath())

func fingerprintStatePath() string {
	dir := strings.TrimSpace(os.Getenv("DENEB_STATE_DIR"))
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fingerprintFileName)
}

// newFingerprintMemory loads the persisted map, exactly as the stealth tier
// memory beside it does.
//
// Persisted rather than process-local because of when this feature pays off: a
// re-fetch minutes later is served from the content cache and has nothing to
// compare, so the comparison that matters happens hours or days later — across
// the gateway restarts that every deploy causes. An in-memory-only store would
// be silently empty at exactly the moment it was supposed to answer.
func newFingerprintMemory(path string) *fingerprintMemory {
	m := &fingerprintMemory{pages: map[string]pageRecord{}, path: path}
	if path == "" {
		return m
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	var stored struct {
		Order []string              `json:"order"`
		Pages map[string]pageRecord `json:"pages"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		slog.Warn("web page fingerprints unreadable, starting empty", "path", path, "error", err)
		return m
	}
	for _, url := range stored.Order {
		if rec, ok := stored.Pages[url]; ok {
			m.pages[url] = rec
			m.order = append(m.order, url)
		}
	}
	return m
}

// saveLocked persists under the caller's lock. Best-effort: losing the file only
// costs one "changed since last time" answer.
func (m *fingerprintMemory) saveLocked() {
	if m.path == "" {
		return
	}
	data, err := json.Marshal(struct {
		Order []string              `json:"order"`
		Pages map[string]pageRecord `json:"pages"`
	}{Order: m.order, Pages: m.pages})
	if err != nil {
		return
	}
	if err := atomicfile.WriteFile(m.path, data, nil); err != nil {
		slog.Debug("web page fingerprint save failed", "path", m.path, "error", err)
	}
}

// compare records the current shape of a page and reports how it differs from
// the last one recorded. first is true when this URL has not been seen, in which
// case there is nothing to report.
func (m *fingerprintMemory) compare(url, content string, budget int64) (changed []string, unchanged int, first bool) {
	record := pageRecord{Budget: budget, Sections: fingerprintSections(content)}
	m.mu.Lock()
	defer m.mu.Unlock()

	prior, seen := m.pages[url]
	m.store(url, record)
	m.saveLocked()
	// A different budget saw a different slice of the page; there is nothing
	// comparable to report.
	if !seen || prior.Budget != budget {
		return nil, 0, true
	}

	before := make(map[string]uint64, len(prior.Sections))
	for _, s := range prior.Sections {
		before[s.Title] = s.Hash
	}
	for _, s := range record.Sections {
		prior, existed := before[s.Title]
		switch {
		case !existed:
			changed = append(changed, s.Title+" (new)")
		case prior != s.Hash:
			changed = append(changed, s.Title)
		default:
			unchanged++
		}
		delete(before, s.Title)
	}
	// Whatever is left was in the old page and is gone from this one.
	for title := range before {
		changed = append(changed, title+" (removed)")
	}
	return changed, unchanged, false
}

// store inserts under the lock, evicting the oldest URL when full.
func (m *fingerprintMemory) store(url string, record pageRecord) {
	if _, exists := m.pages[url]; !exists {
		m.order = append(m.order, url)
		for len(m.order) > fingerprintMemoryMax {
			delete(m.pages, m.order[0])
			m.order = m.order[1:]
		}
	}
	m.pages[url] = record
}

// fingerprintSections hashes each section body, keyed by its heading. Headings
// are the stable identity: a page whose sections are reordered has not changed,
// while one whose section text moved has.
func fingerprintSections(content string) []sectionFingerprint {
	sections := splitMarkdownSections(content)
	out := make([]sectionFingerprint, 0, len(sections))
	for _, sec := range sections {
		title := sec.title
		if title == "" {
			title = "(preamble)"
		}
		out = append(out, sectionFingerprint{Title: title, Hash: hashBody(sec.body)})
	}
	return out
}

// hashBody ignores whitespace-only differences, which are reflow noise rather
// than an edit the reader would care about.
func hashBody(body string) uint64 {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(body), " ")))
	return binary.BigEndian.Uint64(sum[:8])
}

// changeSummary is the metadata line for a re-fetch, or "" on a first visit or
// when the page has no recognisable sections.
func changeSummary(url, content string, budget int64) string {
	changed, unchanged, first := pageFingerprints.compare(url, content, budget)
	if first || (len(changed) == 0 && unchanged == 0) {
		return ""
	}
	if len(changed) == 0 {
		return "Changed: none since last fetch"
	}
	shown := changed
	if len(shown) > 5 {
		shown = shown[:5]
	}
	summary := "Changed: " + strings.Join(shown, ", ")
	if len(changed) > len(shown) {
		summary += ", …"
	}
	return summary
}
