package textsearch

import "strings"

// Hangul matching used to scan the whole inverted vocabulary per query token
// (compound substring + particle trim). That is O(V) per call and was invoked
// inside the per-candidate BM25 loop — catastrophic on a few-thousand-doc
// Korean corpus (mailstore search measured at ~100s). Index-time expansion
// turns those lookups into posting-list hits; query-time hangulTokenMatches
// remains only for per-document term-frequency counting against cached tokens.

const (
	// maxHangulExpandRunes caps how far a single indexed token is exploded into
	// substring keys. Korean tokens are almost always shorter; longer runs are
	// still indexed exactly plus a prefix/suffix window so recall survives.
	maxHangulExpandRunes = 24
	// minHangulExpandRunes is the shortest substring/prefix key we emit. One
	// syllable floods a Korean corpus (rejected by the historical matcher too).
	minHangulExpandRunes = 2
)

var hangulParticles = []string{
	"으로부터", "에게서", "이라도", "이든지", "이라면", "이랑", "으로", "에서", "에게", "부터", "까지", "처럼", "보다", "마다", "조차", "마저",
	"은", "는", "이", "가", "을", "를", "에", "의", "로", "와", "과", "도", "만", "랑",
}

func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

func hangulTokenMatches(indexToken, queryToken string) bool {
	if indexToken == queryToken {
		return true
	}
	queryBase := trimHangulParticle(queryToken)
	for _, query := range []string{queryToken, queryBase} {
		if query == "" {
			continue
		}
		// Preserve the original Hangul-prefix behavior ("레시" -> "레시피")
		// and add safe compound-token recall ("행위허가" -> "개발행위허가").
		if strings.HasPrefix(indexToken, query) {
			return true
		}
		if len([]rune(query)) >= minHangulExpandRunes && strings.Contains(indexToken, query) {
			return true
		}
	}
	return false
}

// maxHangulParticleTrims bounds the stacked-particle peel at TWO — Korean stacks
// at most a case particle plus a topic/additive one ("…에서-는", "…랑-은",
// "…까지-도"). A third round starts eating real nouns: "비금도에서는" correctly
// peels to "비금도" in two, but a third strips the 도 of the place name itself
// and leaves "비금".
const maxHangulParticleTrims = 2

// trimHangulParticle strips trailing particles, REPEATEDLY. Korean particles
// stack ("아르고에너지랑은", "비금도에서는", "계약까지도"), and a single pass left
// "아르고에너지랑은" at "아르고에너지랑" — LONGER than the indexed "아르고에너지",
// so neither the exact key nor the prefix/contains fallbacks matched and the
// query returned ZERO candidates (measured 2026-07-25: the same question without
// the particle ranked the right page #1, with it scored nothing at all).
//
// Over-stripping is bounded by minHangulExpandRunes: a peel that would leave
// fewer than 2 syllables is refused, which is what keeps ordinary nouns whose
// tail happens to be a particle syllable ("결과", "지도", "회의") intact.
func trimHangulParticle(token string) string {
	for i := 0; i < maxHangulParticleTrims; i++ {
		next := trimHangulParticleOnce(token)
		if next == token {
			break
		}
		token = next
	}
	return token
}

func trimHangulParticleOnce(token string) string {
	for _, suffix := range hangulParticles {
		if !strings.HasSuffix(token, suffix) {
			continue
		}
		base := strings.TrimSuffix(token, suffix)
		if len([]rune(base)) >= minHangulExpandRunes {
			return base
		}
	}
	return token
}

// expandIndexKeys returns every inverted-index key a surface token should be
// posted under. Latin/numeric tokens stay exact. Hangul tokens also post under
// their particle-stripped base and every contiguous substring of length ≥ 2
// (capped), so query-time matching is a map lookup instead of a vocabulary scan.
func expandIndexKeys(token string) []string {
	if token == "" {
		return nil
	}
	if !containsHangul(token) {
		return []string{token}
	}
	runes := []rune(token)
	seen := make(map[string]struct{}, 8)
	add := func(s string) {
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}
	add(token)
	if base := trimHangulParticle(token); base != token {
		add(base)
	}
	n := len(runes)
	if n > maxHangulExpandRunes {
		n = maxHangulExpandRunes
		runes = runes[:n]
	}
	for i := 0; i < n; i++ {
		for j := i + minHangulExpandRunes; j <= n; j++ {
			add(string(runes[i:j]))
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
