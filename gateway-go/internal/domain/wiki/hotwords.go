package wiki

import (
	"sort"
	"strings"
)

// HotwordHints builds a comma-separated proper-noun bias list for speech
// recognition (VibeVoice-ASR's `hotwords`) from the wiki index: each page's
// title plus its tags — the company names, people, places, and domain terms the
// user actually works with, which are exactly what bare ASR mis-hears
// (탑솔라→팝솔라, 에코프로, 석문호, 케이원일렉트릭, …). Named entities
// (Type=="entity") and high-importance pages rank first, so the cap keeps the
// most useful names once the wiki grows large. Returns "" for an empty wiki.
func (s *Store) HotwordHints(maxTerms int) string {
	if maxTerms <= 0 {
		maxTerms = 200
	}
	s.mu.RLock()
	entries := make([]IndexEntry, 0, len(s.index.Entries))
	for _, e := range s.index.Entries {
		entries = append(entries, e)
	}
	s.mu.RUnlock()

	// Rank: named entities first, then importance, then recency (Updated date).
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if ae, be := a.Type == "entity", b.Type == "entity"; ae != be {
			return ae
		}
		if a.Importance != b.Importance {
			return a.Importance > b.Importance
		}
		return a.Updated > b.Updated
	})

	const maxChars = 2500
	seen := make(map[string]bool)
	terms := make([]string, 0, maxTerms)
	chars := 0
	// add appends a unique, non-blank term; returns false once a cap is hit.
	add := func(raw string) bool {
		t := strings.TrimSpace(raw)
		if t == "" {
			return true
		}
		key := strings.ToLower(t)
		if seen[key] {
			return true
		}
		if len(terms) >= maxTerms || chars+len(t) > maxChars {
			return false
		}
		seen[key] = true
		terms = append(terms, t)
		chars += len(t) + 2
		return true
	}
	for _, e := range entries {
		if !add(e.Title) {
			break
		}
		capped := false
		for _, tag := range e.Tags {
			if !add(tag) {
				capped = true
				break
			}
		}
		if capped {
			break
		}
	}
	return strings.Join(terms, ", ")
}
