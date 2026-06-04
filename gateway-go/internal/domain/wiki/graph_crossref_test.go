// graph_crossref_test.go — experimental body cross-reference detection,
// evaluated (and rejected) by graph_bench_test.go.
//
// Hypothesis: the explicit Related[] graph misses links a page only states in
// prose — "영광 BESS" lists "비금도 154kV 케이블 (ZTT)" in its body but never as a
// Related[] edge — so recovering them as edges should raise recall. crossRefsFrom
// detects a citation by matching several of one page's title tokens (at least
// one distinctive) inside another page's body.
//
// Result (the benchmark's "+ body cross-references" row): it nudges strong-vs-
// weak AUC up but lowers rank correlation, because site names and specs are
// title-rare yet body-common, so no token threshold separates a real citation
// (영광→비금도) from same-site noise (a module page that merely mentions the site
// and a spec). It is therefore NOT wired into the production write path; this
// file keeps the detector and its measurement so the trade-off stays on record
// and re-runnable rather than relitigated from intuition.
package wiki

import "strings"

const (
	// crossRefDistinctiveMax: a token appearing in this many page titles or
	// fewer is distinctive enough to anchor a citation (a project/site/model
	// name, not a generic word like "케이블" or "발주").
	crossRefDistinctiveMax = 3
	// crossRefMinTokens: how many of a title's tokens a body must contain to
	// count as citing that page. Several co-occurring tokens make a real
	// reference; one or two are coincidence. The benchmark shows no token
	// threshold cleanly separates a real citation from same-site noise — site
	// names and specs are title-rare yet body-common — so this is the best
	// balance, not a clean win.
	crossRefMinTokens = 3
)

// titleSig is a page's title broken into match tokens plus the distinctive
// subset (corpus-rare tokens that must anchor any citation).
type titleSig struct {
	tokens      []string
	distinctive []string
}

// crossRefSignatures returns a titleSig per page (by rec index), with token
// corpus-frequency computed across all titles.
func crossRefSignatures(recs []graphRec) []titleSig {
	freq := make(map[string]int)
	toks := make([][]string, len(recs))
	for i := range recs {
		seen := make(map[string]struct{})
		for _, tok := range graphTitleTokens(recs[i].title) {
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			freq[tok]++
			toks[i] = append(toks[i], tok)
		}
	}
	sigs := make([]titleSig, len(recs))
	for i := range recs {
		sig := titleSig{tokens: toks[i]}
		for _, tok := range toks[i] {
			if freq[tok] <= crossRefDistinctiveMax {
				sig.distinctive = append(sig.distinctive, tok)
			}
		}
		sigs[i] = sig
	}
	return sigs
}

// bodyCites reports whether body cites the page described by sig: it contains at
// least one distinctive token AND at least crossRefMinTokens of the title tokens
// overall.
func bodyCites(body string, sig titleSig) bool {
	if len(sig.tokens) < crossRefMinTokens || len(sig.distinctive) == 0 {
		return false
	}
	hasDistinct := false
	for _, t := range sig.distinctive {
		if strings.Contains(body, t) {
			hasDistinct = true
			break
		}
	}
	if !hasDistinct {
		return false
	}
	n := 0
	for _, t := range sig.tokens {
		if strings.Contains(body, t) {
			n++
		}
	}
	return n >= crossRefMinTokens
}

// crossRefsFrom returns the rec indices linked to `seed` by a title citation in
// either direction: seed's body cites page i, or page i's body cites seed. sigs
// is the output of crossRefSignatures(recs).
func crossRefsFrom(recs []graphRec, seed int, sigs []titleSig) map[int]struct{} {
	out := make(map[int]struct{})
	if seed < 0 || seed >= len(recs) {
		return out
	}
	seedBody := recs[seed].bodyLower
	for i := range recs {
		if i == seed {
			continue
		}
		if bodyCites(seedBody, sigs[i]) || bodyCites(recs[i].bodyLower, sigs[seed]) {
			out[i] = struct{}{}
		}
	}
	return out
}

// graphTitleTokens splits a page title into lowercased tokens of >=2 runes,
// breaking on whitespace, brackets, and punctuation.
func graphTitleTokens(title string) []string {
	fields := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		switch r {
		case ' ', '\t', '(', ')', '[', ']', ',', '·', '/', '-', '–', '—', ':':
			return true
		}
		return false
	})
	var out []string
	for _, f := range fields {
		if len([]rune(f)) >= 2 {
			out = append(out, f)
		}
	}
	return out
}
