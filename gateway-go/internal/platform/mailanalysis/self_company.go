// self_company.go — deterministic "is this counterparty actually us?" guard.
//
// The deal extractor keys a 거래 원장 (프로젝트/거래/<slug>.md) off the
// counterparty name it pulls from a business document. A document our OWN firm
// issued (우리가 발행한 견적서 등) can lead the tiny model to name the sender —
// our own legal entity — as the counterparty, minting a self-ledger: a
// 거래/탑솔라㈜.md page whose entries are really deals with 무림피앤피·인하공전·
// 현대차. Two such self-ledgers were found and hand-cleaned in the 2026-07-07
// wiki sweep. This guard rejects a self-named counterparty before any artifact
// (page·ledger record·notebook pin·deal-question card) is created — mirroring
// gateDealAmount/verifyDealFacts: a deterministic post-extraction gate that
// drops bad data and Warn-logs, never a silent one.
//
// Self-identity reuses the party anchor's our-domains set (ourAnchorDomains /
// defaultOurDomain, party_anchor.go) as its single source of truth: the
// romanized name stem is DERIVED from each our-domain (topsolar.kr → topsolar),
// so the domain constant stays the only place to update. A Korean name has no
// derivable romanization, so it is a small explicit default (탑솔라),
// overridable via DENEB_MAIL_OUR_NAMES for group affiliates — same shape as
// DENEB_MAIL_OUR_DOMAINS.
package mailanalysis

import (
	"os"
	"strings"
)

// defaultOurNames is the operator organization's own firm name(s) used by the
// counterparty self-guard. Extend with DENEB_MAIL_OUR_NAMES (comma-separated)
// when affiliates file under additional names. The romanized form (topsolar) is
// derived from ourAnchorDomains, so it is intentionally NOT listed here.
const defaultOurNames = "탑솔라"

// companyDecorations are corporate-form tokens peeled from a firm name before
// keying, so "탑솔라㈜" / "탑솔라(주)" / "TOPSOLAR CO.,LTD" all reduce to their
// bare stem. Removed as substrings (case-folded input); leftover punctuation is
// then dropped by companyMatchKey, so partial fragments (co. / ,ltd) still
// vanish.
var companyDecorations = []string{
	"주식회사", "유한회사", "㈜", "(주)", "(유)",
	"co.,ltd.", "co.,ltd", "co.ltd", "co.", "company",
	"ltd.", "ltd", "inc.", "inc", "corp.", "corp",
}

// isSelfCounterparty reports whether name denotes our own firm rather than an
// external 거래처 — so the deal extractor can refuse to mint a self-ledger from
// it. Two independent deterministic signals:
//
//  1. the name carries one of our mail domains (topsolar.kr) — an address, or a
//     "(topsolar.kr)" parenthetical the tiny model sometimes echoes — matched
//     against ourAnchorDomains; or
//  2. the name, reduced to a match key (case-folded, corporate decorations and
//     punctuation/whitespace stripped), EQUALS one of our self-identifier
//     tokens (탑솔라, topsolar). Equality, not substring: a distinct firm that
//     merely shares a prefix (were "탑솔라파트너스" a real separate 거래처) is not
//     blocked — over-blocking would silently drop a genuine deal.
func isSelfCounterparty(name string) bool {
	domains := ourAnchorDomains()
	lower := strings.ToLower(name)
	for dom := range domains {
		if strings.Contains(lower, dom) {
			return true
		}
	}
	key := companyMatchKey(name)
	if key == "" {
		return false
	}
	return ourCompanyNames(domains)[key]
}

// ourCompanyNames returns the set of normalized self-identifier match keys a
// counterparty is compared against: the configured/default Korean names plus
// the romanized stem of every our-domain (topsolar.kr → topsolar). Passing the
// already-built domain set in keeps isSelfCounterparty from reading env twice.
func ourCompanyNames(domains map[string]bool) map[string]bool {
	out := map[string]bool{}
	raw := strings.TrimSpace(os.Getenv("DENEB_MAIL_OUR_NAMES"))
	if raw == "" {
		raw = defaultOurNames
	}
	for _, n := range strings.Split(raw, ",") {
		if k := companyMatchKey(n); k != "" {
			out[k] = true
		}
	}
	// Derive the romanized stem from each our-domain so the domain constant is
	// the single source of truth: topsolar.kr → "topsolar" catches the English
	// "TOPSOLAR CO.,LTD" form without a second hardcoded list. First label only
	// (before the first dot) — the org name, not a TLD/subdomain.
	for dom := range domains {
		stem, _, _ := strings.Cut(dom, ".")
		if k := companyMatchKey(stem); k != "" {
			out[k] = true
		}
	}
	return out
}

// companyMatchKey reduces a firm name to a comparison key: case-folded,
// corporate-form decorations removed, then every remaining rune that is not
// ASCII alphanumeric or CJK/Hangul dropped (spaces, dots, commas, parentheses).
// "탑솔라㈜" → "탑솔라"; "TOPSOLAR CO.,LTD" → "topsolar".
func companyMatchKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	for _, dec := range companyDecorations {
		s = strings.ReplaceAll(s, dec, "")
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 0xAC00 && r <= 0xD7A3: // Hangul syllables
			b.WriteRune(r)
		case r >= 0x4E00 && r <= 0x9FFF: // CJK unified ideographs (漢字 firm names)
			b.WriteRune(r)
		}
	}
	return b.String()
}
