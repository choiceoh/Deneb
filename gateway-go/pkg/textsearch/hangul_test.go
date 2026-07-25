package textsearch

import "testing"

// Korean particles stack ("…랑-은", "…에서-는", "…까지-도"). A single peel left
// "아르고에너지랑은" at "아르고에너지랑" — longer than the indexed "아르고에너지",
// so neither the exact key nor the prefix/contains fallback matched and the query
// returned ZERO candidates (measured 2026-07-25 against the live wiki).
func TestTrimHangulParticleStripsStackedParticles(t *testing.T) {
	stacked := map[string]string{
		"아르고에너지랑은": "아르고에너지",
		"비금도에서는":   "비금도", // two rounds only — a third would eat the 도 of the place name
		"계약까지도":    "계약",
		"썬탑이랑은":    "썬탑",
		"학교와는":     "학교",
	}
	for in, want := range stacked {
		if got := trimHangulParticle(in); got != want {
			t.Errorf("trimHangulParticle(%q) = %q, want %q", in, got, want)
		}
	}

	// Single particles keep working, unchanged.
	for in, want := range map[string]string{"가격은": "가격", "생산라인은": "생산라인", "당진솔라빌리지에서": "당진솔라빌리지"} {
		if got := trimHangulParticle(in); got != want {
			t.Errorf("single particle: trimHangulParticle(%q) = %q, want %q", in, got, want)
		}
	}

	// Ordinary nouns whose tail is a particle syllable must survive: the
	// minimum-base guard is what protects them from the repeated peel.
	for _, keep := range []string{"결과", "지도", "회의", "사과"} {
		if got := trimHangulParticle(keep); got != keep {
			t.Errorf("trimHangulParticle(%q) = %q, want it unchanged", keep, got)
		}
	}
}
