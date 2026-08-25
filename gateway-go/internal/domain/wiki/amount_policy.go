// amount_policy.go — advisory detection of VAT-inclusive-only amounts.
//
// Operator policy (fact wiki.amount_vat_policy, direct instruction): wiki
// amounts are recorded as supply price (공급가액); VAT-inclusive figures alone
// are confusing and forbidden. The 2026-08-25 content audit found 34 pages
// carrying VAT-포함-only amounts; the rep-page instances were corrected by
// hand against their source deals, and this scan keeps the class visible so
// new violations surface in the wiki-review log instead of silently
// accumulating again.
//
// Advisory ONLY — no automatic edits. Converting an amount needs the source
// document (or the statutory 10% arithmetic labeled as 환산), which is a
// judgment call the sweep must not make.
package wiki

import (
	"path/filepath"
	"regexp"
	"strings"
)

// AmountPolicyHit is one flagged amount with enough context to fix by hand.
type AmountPolicyHit struct {
	Path    string
	Snippet string
}

// vatInclusiveRe finds an amount annotated as VAT-inclusive. The exemption
// window then looks for a supply-price companion nearby.
var vatInclusiveRe = regexp.MustCompile(`[0-9.,]+\s*(?:억|만|천)?\s*원?[^\n]{0,30}(?:VAT|부가세)\s*포함`)

const vatExemptionWindow = 150

// ScanAmountPolicyViolations walks wiki-authored project surfaces (대표.md and
// 로그.md — NOT 메일분석, which quotes source mails verbatim and is half-exempt)
// and reports amounts written VAT-inclusive with no supply-price companion in
// the surrounding context. Bounded by limit; deterministic, read-only.
func (s *Store) ScanAmountPolicyViolations(limit int) []AmountPolicyHit {
	if s == nil || limit <= 0 {
		return nil
	}
	pages, err := s.ListPages(projectCategoryPrefix)
	if err != nil {
		return nil
	}
	var hits []AmountPolicyHit
	for _, rp := range pages {
		rp = filepath.ToSlash(rp)
		base := filepath.Base(rp)
		if base != "대표.md" && base != "로그.md" {
			continue
		}
		page, perr := s.ReadPage(rp)
		if perr != nil || page == nil {
			continue
		}
		content := page.Meta.Summary + "\n" + page.Body
		for _, loc := range vatInclusiveRe.FindAllStringIndex(content, -1) {
			start, end := loc[0], loc[1]
			ctxStart := max(0, start-vatExemptionWindow)
			ctxEnd := min(len(content), end+vatExemptionWindow)
			ctx := content[ctxStart:ctxEnd]
			if strings.Contains(ctx, "VAT 별도") || strings.Contains(ctx, "공급가") {
				continue
			}
			hits = append(hits, AmountPolicyHit{Path: rp, Snippet: strings.TrimSpace(content[start:end])})
			break // one hit per page is enough for the advisory
		}
		if len(hits) >= limit {
			break
		}
	}
	return hits
}
