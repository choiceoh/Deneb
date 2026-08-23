// counterparty_anchor.go — resolve counterparty mentions in free text to their
// 거래 원장 page (프로젝트/거래/<name>.md), mirroring project_anchor.go.
//
// Consumer: recall (chat pipeline). A turn that names a counterparty gets that
// company's deal ledger anchored into the recall evidence, so "대한전선 최근
// 거래 어떻게 됐지?" surfaces the cross-project ledger even when BM25 ranks
// mail-analysis detail pages first. Deterministic — no LLM.
package wiki

import (
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// minCounterpartyKeyRunes mirrors minProjectKeyRunes: names shorter than
	// this never match by text ("주"류 조각이 아무데나 걸리는 것 방지).
	minCounterpartyKeyRunes = 3
	// minCounterpartyASCIIContainRunes guards short all-ASCII ledger names
	// (skb, ztt, hre) against substring hits inside ordinary English words —
	// normalizeTitleKey strips separators, so "threshold" contains "hre".
	// Below this length an ASCII key must match as a standalone token instead.
	minCounterpartyASCIIContainRunes = 5
)

// CounterpartyRef names a deal ledger page under 프로젝트/거래/.
type CounterpartyRef struct {
	Name    string // display name (page Title, else the ledger filename)
	Path    string // ledger path, e.g. "프로젝트/거래/대한전선.md"
	Summary string // page Meta.Summary — one-line description
}

// MatchCounterpartiesInText returns counterparties whose ledger name appears in
// text, most-specific (longest key) first, capped at limit. Matching mirrors
// MatchProjectsInText (normalized containment) with one extra guard: short
// all-ASCII names only match as standalone tokens, optionally with an attached
// non-ASCII tail so Korean particles still hit ("SKB에 회신했나" → skb).
func (s *Store) MatchCounterpartiesInText(text string, limit int) []CounterpartyRef {
	if s == nil || limit <= 0 {
		return nil
	}
	hay := normalizeTitleKey(text)
	if hay == "" {
		return nil
	}
	tokens := lowerAlnumTokens(text)
	type scored struct {
		ref CounterpartyRef
		key string
	}
	var hits []scored
	for _, ref := range s.knownCounterparties() {
		key := bestCounterpartyKeyIn(hay, tokens, ref)
		if key == "" {
			continue
		}
		hits = append(hits, scored{ref: ref, key: key})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return utf8.RuneCountInString(hits[i].key) > utf8.RuneCountInString(hits[j].key)
	})
	out := make([]CounterpartyRef, 0, limit)
	for _, h := range hits {
		out = append(out, h.ref)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// KnownCounterparties lists the active deal ledgers (프로젝트/거래/) — the
// counterparty name set for loose matching surfaces (meeting harvest).
func (s *Store) KnownCounterparties() []CounterpartyRef { return s.knownCounterparties() }

// knownCounterparties lists the deal ledgers under 프로젝트/거래/, skipping
// archived ones. Sorted by name.
func (s *Store) knownCounterparties() []CounterpartyRef {
	paths, err := s.ListPages(projectCategoryPrefix + "/" + dealDir)
	if err != nil {
		return nil
	}
	refs := make([]CounterpartyRef, 0, len(paths))
	for _, p := range paths {
		p = strings.ReplaceAll(p, "\\", "/")
		ref := CounterpartyRef{
			Name: strings.TrimSuffix(path.Base(p), ".md"),
			Path: p,
		}
		if page, perr := s.ReadPage(p); perr == nil && page != nil {
			// Superseded-but-not-yet-archived ledgers (post-merge, pre-verify)
			// must not anchor either — the replacement page is the live one.
			if page.Meta.Archived || IsEffectivelySuperseded(p, page.Meta) {
				continue
			}
			if t := strings.TrimSpace(page.Meta.Title); t != "" {
				ref.Name = t
			}
			ref.Summary = strings.TrimSpace(page.Meta.Summary)
		}
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs
}

// bestCounterpartyKeyIn returns the longest (by rune count) normalized identity
// key of ref (display name or ledger filename) that matches the text, or "".
func bestCounterpartyKeyIn(hay string, tokens []string, ref CounterpartyRef) string {
	base := strings.TrimSuffix(path.Base(ref.Path), ".md")
	best := ""
	for _, cand := range []string{ref.Name, base} {
		key := normalizeTitleKey(cand)
		n := utf8.RuneCountInString(key)
		if n < minCounterpartyKeyRunes {
			continue
		}
		var matched bool
		if isASCIIKey(key) && n < minCounterpartyASCIIContainRunes {
			matched = matchesToken(tokens, key)
		} else {
			matched = strings.Contains(hay, key)
		}
		if matched && n > utf8.RuneCountInString(best) {
			best = key
		}
	}
	return best
}

// lowerAlnumTokens splits text into lowercased letter/digit runs, the token set
// short-ASCII keys are matched against.
func lowerAlnumTokens(text string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

// matchesToken reports whether key appears as a standalone token, or as a token
// prefix whose continuation is non-ASCII — Korean particles attach without a
// separator ("skb에"), and that must still count as the bare name while an
// ASCII continuation ("skbroadband") must not.
func matchesToken(tokens []string, key string) bool {
	for _, t := range tokens {
		if t == key {
			return true
		}
		if strings.HasPrefix(t, key) {
			r, _ := utf8.DecodeRuneInString(t[len(key):])
			if r != utf8.RuneError && r > unicode.MaxASCII {
				return true
			}
		}
	}
	return false
}

// isASCIIKey reports whether key consists solely of ASCII runes.
func isASCIIKey(key string) bool {
	for _, r := range key {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
