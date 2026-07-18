package wiki

import (
	"sort"

	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// HotwordHints builds a comma-separated proper-noun bias list for speech
// recognition (the ASR sidecar's `hotwords`) from the wiki index: each page's
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
	terms := textutil.NewLimitedTerms(maxTerms, maxChars)
	for _, e := range entries {
		if !terms.Add(e.Title) {
			break
		}
		capped := false
		for _, tag := range e.Tags {
			if !terms.Add(tag) {
				capped = true
				break
			}
		}
		if capped {
			break
		}
	}
	return terms.String()
}
