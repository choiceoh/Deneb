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
	"strings"
	"sync"
)

// fingerprintMemoryMax bounds the store. Sections are ~40 bytes each here, so
// this stays far below the content cache while covering many more pages.
const fingerprintMemoryMax = 512

type sectionFingerprint struct {
	Title string
	Hash  uint64
}

type fingerprintMemory struct {
	mu    sync.Mutex
	pages map[string][]sectionFingerprint
	order []string
}

var pageFingerprints = &fingerprintMemory{pages: map[string][]sectionFingerprint{}}

// compare records the current shape of a page and reports how it differs from
// the last one recorded. first is true when this URL has not been seen, in which
// case there is nothing to report.
func (m *fingerprintMemory) compare(url, content string) (changed []string, unchanged int, first bool) {
	now := fingerprintSections(content)
	m.mu.Lock()
	defer m.mu.Unlock()

	previous, seen := m.pages[url]
	m.store(url, now)
	if !seen {
		return nil, 0, true
	}

	before := make(map[string]uint64, len(previous))
	for _, s := range previous {
		before[s.Title] = s.Hash
	}
	for _, s := range now {
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
func (m *fingerprintMemory) store(url string, sections []sectionFingerprint) {
	if _, exists := m.pages[url]; !exists {
		m.order = append(m.order, url)
		for len(m.order) > fingerprintMemoryMax {
			delete(m.pages, m.order[0])
			m.order = m.order[1:]
		}
	}
	m.pages[url] = sections
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
func changeSummary(url, content string) string {
	changed, unchanged, first := pageFingerprints.compare(url, content)
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
