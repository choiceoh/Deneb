// project_anchor.go — resolve project mentions in free text to their 대표페이지.
//
// Two consumers:
//   - recall (chat pipeline): a turn that names a project gets that project's
//     대표페이지 anchored into the recall evidence, so "기아 화성 근황?" surfaces
//     the curated 현재 상태 even when BM25 ranks detail pages first;
//   - mail reclassification: an unlinked mail whose title names exactly one
//     project files into that project's 메일분석 slot.
package wiki

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// minProjectKeyRunes guards containment matching against short generic names
// ("바로" would match everywhere); names shorter than this never match by text.
const minProjectKeyRunes = 3

// MatchProjectsInText returns ACTIVE projects whose name (display title or
// folder name) appears in text, most-specific (longest key) first, capped at
// limit. Matching is normalized (lowercase, letters+digits only) so "기아 화성"
// matches the 기아-화성 folder. Deterministic — no LLM.
func (s *Store) MatchProjectsInText(text string, limit int) []ProjectRef {
	if s == nil || limit <= 0 {
		return nil
	}
	hay := normalizeTitleKey(text)
	if hay == "" {
		return nil
	}
	type scored struct {
		ref ProjectRef
		key string
	}
	var hits []scored
	for _, ref := range s.knownProjects() {
		key := bestProjectKeyIn(hay, ref)
		if key == "" {
			continue
		}
		hits = append(hits, scored{ref: ref, key: key})
	}
	// Specificity = rune count, not byte length: on mixed Hangul/ASCII keys a
	// 3-byte Hangul rune would out-"length" three ASCII runes.
	sort.SliceStable(hits, func(i, j int) bool {
		return utf8.RuneCountInString(hits[i].key) > utf8.RuneCountInString(hits[j].key)
	})
	out := make([]ProjectRef, 0, limit)
	for _, h := range hits {
		out = append(out, h.ref)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// UniqueProjectInText resolves text to a single ACTIVE project when the most
// specific identity match is unambiguous: the project whose matched key is
// strictly the longest (rune count) wins, and a specificity TIE across
// distinct projects returns ok=false. This is the primitive for exactly-one
// consumers (meeting harvest, mail reclassification): with 거래처 keys in
// play, a bare client mention ("금호타이어 회의") matches every project of
// that client at the same key length — a tie, so no arbitrary pick — while a
// specific title ("금호타이어 곡성 1단계 자재") still resolves because the
// project's own name key outranks its siblings' client-only match.
func (s *Store) UniqueProjectInText(text string) (ProjectRef, bool) {
	if s == nil {
		return ProjectRef{}, false
	}
	return uniqueProjectIn(normalizeTitleKey(text), s.knownProjects())
}

// uniqueProjectIn is UniqueProjectInText over an already-fetched project list
// (the mail reclassifier batches candidates; re-listing per page was a silent
// N× knownProjects scan).
//
// Resolution doctrine (모호하면 잔류):
//   - A hit via the project's OWN identity (name/folder/site) outranks hits
//     that matched only through the shared 거래처 key — an explicit project
//     mention wins over siblings merely implied by their client.
//   - Among own-identity hits, the longest key wins ONLY when every other own
//     key is its substring ("기아 화성" is subsumed by "기아 화성 국유지");
//     two independent project mentions ("기아 화성 + 해남 EPC 비교") stay
//     ambiguous no matter their lengths.
//   - Client-key-only hits resolve only when a single project matched (a
//     single-project 거래처); two siblings at the same client key tie.
func uniqueProjectIn(hay string, projects []ProjectRef) (ProjectRef, bool) {
	if hay == "" {
		return ProjectRef{}, false
	}
	type hit struct {
		ref ProjectRef
		key string
	}
	var ownHits, clientHits []hit
	for _, ref := range projects {
		key := bestProjectKeyIn(hay, ref)
		if key == "" {
			continue
		}
		if key == normalizeTitleKey(ref.Client) {
			// Our own company is never evidence of WHICH project a document
			// belongs to — every internal mail says "탑솔라". One active project
			// carries client: 탑솔라, so the client fallback filed five unrelated
			// "[탑솔라(주)] …" subjects into it (2026-08, 5 of 7 title refiles).
			// The domain path already blocklists topsolar.kr; this is the same
			// rule for the name.
			if isOwnCompanyKey(key) {
				continue
			}
			clientHits = append(clientHits, hit{ref: ref, key: key})
		} else {
			ownHits = append(ownHits, hit{ref: ref, key: key})
		}
	}
	pool := ownHits
	if len(pool) == 0 {
		pool = clientHits
	}
	if len(pool) == 0 {
		return ProjectRef{}, false
	}
	best := pool[0]
	for _, h := range pool[1:] {
		if utf8.RuneCountInString(h.key) > utf8.RuneCountInString(best.key) {
			best = h
		}
	}
	for _, h := range pool {
		if h.ref.Path == best.ref.Path {
			continue
		}
		// A distinct project matching at the same key, or via a key the
		// winner's does not subsume, is independent evidence → ambiguous.
		if h.key == best.key || !strings.Contains(best.key, h.key) {
			return ProjectRef{}, false
		}
	}
	return best.ref, true
}

// MatchProjectSite matches place text against project 현장(sites) ONLY — never
// the project name or client. The location→site-visit recorder needs this
// precision: matching on name/client would falsely "visit" a project just
// because the geocoded place shares a token with its name (a project literally
// named "군산" would match every 군산 location). Returns the project whose site
// key is the longest (most specific) match, the human-readable matched site
// candidate (original spacing/text, e.g. "수산리" — not the normalized key), and
// ok=false when no site matches.
//
// 모호하면 잔류: when two DISTINCT active projects match at the same longest key
// (e.g. both list 수산리, or the geocoded place shares only a trailing 읍/면/리
// unit with both), the location payload carries no evidence to choose between
// them, so it returns ok=false rather than silently logging to whichever was
// scanned first. Deterministic, active projects only.
func (s *Store) MatchProjectSite(place string) (ProjectRef, string, bool) {
	if s == nil {
		return ProjectRef{}, "", false
	}
	hay := normalizeTitleKey(place)
	if hay == "" {
		return ProjectRef{}, "", false
	}
	bestRef := ProjectRef{}
	bestCand := ""     // original candidate string (for display/logging)
	bestKeyLen := 0    // normalized-key rune count (for specificity comparison)
	ambiguous := false // another DISTINCT project matched at bestKeyLen
	for _, ref := range s.knownProjects() {
		for _, site := range ref.Sites {
			for _, cand := range siteMatchCandidates(site) {
				key := normalizeTitleKey(cand)
				keyLen := utf8.RuneCountInString(key)
				if keyLen < minProjectKeyRunes || !strings.Contains(hay, key) {
					continue
				}
				switch {
				case keyLen > bestKeyLen:
					bestKeyLen = keyLen
					bestCand = strings.TrimSpace(cand)
					bestRef = ref
					ambiguous = false
				case keyLen == bestKeyLen && ref.Path != bestRef.Path:
					// A distinct project ties the current best key → no evidence
					// to disambiguate a shared-site visit.
					ambiguous = true
				}
			}
		}
	}
	if bestKeyLen == 0 || ambiguous {
		return ProjectRef{}, "", false
	}
	return bestRef, bestCand, true
}

// siteMatchCandidates returns the match keys for one 현장 value: the full site
// string plus its trailing administrative unit ("전북 군산시 옥구읍 수산리" →
// [full, "수산리"]), the latter being what a geocoded place most reliably
// shares with the stored site.
func siteMatchCandidates(site string) []string {
	out := []string{site}
	if fields := strings.Fields(site); len(fields) > 1 {
		out = append(out, fields[len(fields)-1])
	}
	return out
}

func bestProjectKeyIn(hay string, ref ProjectRef) string {
	best := ""
	name, _ := ProjectNameOf(ref.Path)
	cands := []string{ref.Name, name, ref.Client}
	for _, site := range ref.Sites {
		cands = append(cands, siteMatchCandidates(site)...)
	}
	for _, cand := range cands {
		key := normalizeTitleKey(cand)
		if utf8.RuneCountInString(key) < minProjectKeyRunes {
			continue
		}
		if strings.Contains(hay, key) &&
			utf8.RuneCountInString(key) > utf8.RuneCountInString(best) {
			best = key
		}
	}
	return best
}

// ownCompanyKeys are normalized spellings of OUR company. Kept next to
// mailDomainBlocklist's topsolar.kr entry — same rule, name side.
var ownCompanyKeys = map[string]bool{
	"탑솔라":      true,
	"탑솔라주":     true,
	"주탑솔라":     true,
	"탑솔라그룹":    true,
	"topsolar": true,
}

// isOwnCompanyKey reports whether a normalized match key names our own company.
func isOwnCompanyKey(key string) bool {
	return ownCompanyKeys[key]
}
