// person_identity_ack.go — the operator-owned "stop asking" for the two
// person-identity scans (homonym, duplicate-name).
//
// Neither scan may auto-fix: splitting a merged person and merging two pages
// are both operator calls (the 2026-07-28 over-merge is why). That is correct,
// and it is also why they needed a way to END: with no place to record "I
// looked, it is fine", the same five people were re-detected every cycle and
// re-counted in every dream card, forever. The scan is not wrong — it is
// unterminating, and an unterminating question reads as nagging.
//
// The ack follows the DueDone contract exactly: store the EVIDENCE that was
// judged, not a bare flag. A page whose reviewed set covers today's evidence
// is silent; a NEW domain (or a NEW same-name page) is not covered by the old
// decision, so it surfaces once and can be judged on its own.
package wiki

import (
	"sort"
	"strings"
)

// identityReviewedSet indexes a page's recorded decision for lookup.
func identityReviewedSet(meta Frontmatter) map[string]bool {
	if len(meta.IdentityReviewed) == 0 {
		return nil
	}
	set := make(map[string]bool, len(meta.IdentityReviewed))
	for _, v := range meta.IdentityReviewed {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			set[v] = true
		}
	}
	return set
}

// identityEvidenceReviewed reports whether every signal is already covered by
// the page's recorded decision. Empty evidence is never "reviewed" — that would
// silence a page the operator never saw.
func identityEvidenceReviewed(meta Frontmatter, evidence []string) bool {
	if len(evidence) == 0 {
		return false
	}
	set := identityReviewedSet(meta)
	if len(set) == 0 {
		return false
	}
	for _, e := range evidence {
		if !set[strings.ToLower(strings.TrimSpace(e))] {
			return false
		}
	}
	return true
}

// duplicatePeerEvidence is the ack token set for the duplicate-name scan: the
// OTHER pages sharing this name. Prefixed so a page path can never be mistaken
// for a domain in the same field.
func duplicatePeerEvidence(self string, group []DuplicatePersonPage) []string {
	out := make([]string, 0, len(group))
	for _, p := range group {
		if p.PagePath != self {
			out = append(out, "dup:"+p.PagePath)
		}
	}
	return out
}

// PersonCompanyDomains returns the company email domains a 인물 page currently
// claims — the evidence an operator decision on the homonym scan covers.
func (s *Store) PersonCompanyDomains(relPath string) []string {
	if s == nil {
		return nil
	}
	page, err := s.ReadPage(relPath)
	if err != nil || page == nil {
		return nil
	}
	domains := companyEmailDomains(append(append([]string(nil), page.Meta.Emails...),
		bodyEmailAddresses(page.Body)...))
	sort.Strings(domains)
	return domains
}
