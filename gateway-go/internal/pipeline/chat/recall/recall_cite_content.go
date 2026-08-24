// recall_cite_content.go — content-grounded citation matching for the
// end-of-turn cite pass (2026-08-25).
//
// The name-based matcher (matchCitedPaths) requires the answer to contain the
// page's path, title, or project code — and Korean answers almost never name
// pages; they quote the page's CONTENT (the figures, the counterparty, the
// hostname). Measured over 172 real injected turns: name matching credited 5
// (the ledger's ~3% cite rate exactly), while the answers were visibly built
// from the evidence. This pass closes that gap from the content side: extract
// the page body's DISTINCTIVE tokens — precise amounts with units, project
// codes, email addresses, latin proper nouns — and credit the page only when
// the answer reproduces at least two of them.
//
// Precision-first, like everything on the cite path: a false positive grants
// unearned utility credit (and, since TRS, unearned rank protection). The
// validated filters each kill a measured false-positive class:
//   - round numbers ("20%", "300억") are excluded — round percentages matched
//     unrelated finance answers in replay;
//   - corpus-common tokens are excluded via the FTS rarity index — "Tailscale"
//     appears across the 시스템 category and must not let one page claim an
//     answer about another;
//   - a single matched token is never enough (two distinct, always).
//
// Under the tightened rules the 172-turn replay yielded 7 turns, every one a
// true citation (vaultwarden credentials answer ↔ vaultwarden page; a 42MW/
// 336억 scenario tree ↔ the wind-project pages it was computed from).
package recall

import (
	"regexp"
	"strings"
)

const (
	// citeContentMinMatches is the two-distinct-tokens floor. One token —
	// however rare — proved insufficient in replay (a shared hostname credited
	// a sibling page).
	citeContentMinMatches = 2
	// citeContentRarityFloor mirrors the BM25 gate's notion of "common": a
	// token at or below this rarity is carried by too much of the corpus to
	// attribute an answer to one page.
	citeContentRarityFloor = 0.55
	// citeContentMaxTokens bounds per-page token extraction; pages are small
	// but a pathological table must not turn the post-delivery pass quadratic.
	citeContentMaxTokens = 64
)

var (
	// citeAmountRe: a number with a value-bearing unit. Bare percents are
	// excluded on purpose — replay showed round percentages ("20%", "70%")
	// matching unrelated finance answers.
	citeAmountRe = regexp.MustCompile(`[0-9][0-9.,]+\s*(?:억|조|만원|위안|달러|kW|MW|GW|kWh|MWh|km|mm|톤)`)
	// citeProjectCodeRe: the frozen composite project identity (wiki-layout).
	citeProjectCodeRe = regexp.MustCompile(`\b[a-z]{2,4}[0-9]?-[a-z]{2,4}-[a-z]{3}-[0-9]{3}\b`)
	citeEmailRe       = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`)
	// citeProperRe: latin proper nouns long enough to be names, plus Korean
	// company forms. Short latin words are everyday vocabulary.
	citeProperRe = regexp.MustCompile(`[가-힣]{2,}(?:주식회사|㈜)|[A-Z][A-Za-z]{5,}`)
	citeDigitsRe = regexp.MustCompile(`^([0-9][0-9.,]*)`)
)

// citeRoundNumber reports whether the token's numeric head is a round figure
// (one significant digit): 20%, 300억, 1,000만원. Round figures recur across
// unrelated documents and answers, so they carry no attribution power.
func citeRoundNumber(token string) bool {
	m := citeDigitsRe.FindStringSubmatch(token)
	if m == nil {
		return false
	}
	digits := strings.NewReplacer(",", "", ".", "").Replace(m[1])
	return len(strings.TrimRight(digits, "0")) <= 1
}

// citeDistinctiveTokens extracts the body's attribution-bearing tokens.
func citeDistinctiveTokens(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(tok string) {
		tok = strings.TrimSpace(tok)
		if len(tok) < 3 || len(out) >= citeContentMaxTokens {
			return
		}
		if _, dup := seen[tok]; dup {
			return
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	for _, tok := range citeAmountRe.FindAllString(body, -1) {
		if !citeRoundNumber(strings.TrimSpace(tok)) {
			add(tok)
		}
	}
	for _, re := range []*regexp.Regexp{citeProjectCodeRe, citeEmailRe, citeProperRe} {
		for _, tok := range re.FindAllString(body, -1) {
			add(tok)
		}
	}
	return out
}

// tokenRarity is the corpus-distinctiveness oracle (wiki.Store.TokenRarity in
// production); injectable so the matcher is testable without an index.
type tokenRarity func(token string) float64

// citeContentMatches reports whether the answer reproduces enough of the
// page body's distinctive content to credit the page as cited.
func citeContentMatches(answer, body string, rarity tokenRarity) bool {
	if strings.TrimSpace(answer) == "" || strings.TrimSpace(body) == "" {
		return false
	}
	matched := 0
	for _, tok := range citeDistinctiveTokens(body) {
		if !strings.Contains(answer, tok) {
			continue
		}
		if rarity != nil && rarity(tok) <= citeContentRarityFloor {
			continue
		}
		matched++
		if matched >= citeContentMinMatches {
			return true
		}
	}
	return false
}
