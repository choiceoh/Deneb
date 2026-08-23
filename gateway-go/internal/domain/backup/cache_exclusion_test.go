package backup

import "testing"

// TestIsRegenerableCache_MatchesVectorCachesOnly: the vector caches are derived
// from pages + embedder and rebuild on the first warm after a restore, so
// shipping them daily costs transfer for bytes a restore does not need (the
// wiki tarred to 117MB, 69MB of it one cache, against 1.6MB of actual pages).
// Real memory — pages, ledgers, git history — must never match.
func TestIsRegenerableCache_MatchesVectorCachesOnly(t *testing.T) {
	cases := map[string]bool{
		"wiki/.semantic-cache.json":               true,
		"memory/diary/.diary-semantic-cache.json": true,
		"mailstore/semantic-index.json":           true,
		"workfeed.semantic.json":                  true,
		"wiki/프로젝트/pl2-kia-epc-001/대표.md":         false,
		"wiki/.deals.jsonl":                       false,
		"wiki/.git/objects/ab/cdef":               false,
		"wiki/index.md":                           false,
	}
	for name, want := range cases {
		if got := isRegenerableCache(name); got != want {
			t.Errorf("isRegenerableCache(%q) = %v, want %v", name, got, want)
		}
	}
}
