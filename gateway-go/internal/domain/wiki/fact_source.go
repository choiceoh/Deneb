// fact_source.go — earning primary_document authority instead of claiming it.
//
// ADR-0005 closed the hole where a model could name its own authority: the
// knowledge tool has no authority field, and writes are capped at
// agent_confirmed. That left the document-backed policies (amount, deadline,
// contract) unreachable by design, with the ADR naming an authenticated
// ingestion path as the way back.
//
// This is that path, and it authenticates the SOURCE rather than the caller: a
// claim is promoted only when the store can open the named page itself and read
// the asserted value inside it, and only with the basis date that page carries.
// A source ref is then evidence, not decoration — the promotion is something the
// server verified, never something a caller asserted.
package wiki

import (
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// FactSourceEvidence is the outcome of checking one source ref against a value.
type FactSourceEvidence struct {
	// Ref is the caller-supplied reference, kept for audit even when unverified.
	Ref string
	// Path is the wiki page the ref resolved to, empty when it resolved to none.
	Path string
	// BasisAt is the document's own date — the page's `updated` frontmatter, not
	// the time of the call and not a caller-supplied string.
	BasisAt time.Time
	// Verified reports whether the page exists, carries a date, and actually
	// contains the asserted value.
	Verified bool
	// Reason explains a refusal, so a caller can be told what would fix it.
	Reason string
}

const factSourceRefPrefix = "w:"

// VerifyFactSource reports whether one source ref proves a claim.
//
// Three things must hold, and each failure is reported rather than guessed at:
// the ref resolves to a real page in this store, that page carries a date to use
// as the basis, and the asserted value occurs in the page body on a token
// boundary. A near-miss (the page says "120,000,000원" while the claim says
// "1억 2,000만원") deliberately does not verify: this checks that a document
// SAYS the value, which is the only thing a text match can honestly establish.
func (s *Store) VerifyFactSource(ref, value string) FactSourceEvidence {
	evidence := FactSourceEvidence{Ref: strings.TrimSpace(ref)}
	if s == nil || evidence.Ref == "" {
		evidence.Reason = "empty source ref"
		return evidence
	}
	value = strings.TrimSpace(value)
	if value == "" {
		evidence.Reason = "empty value"
		return evidence
	}

	path := strings.TrimSpace(strings.TrimPrefix(evidence.Ref, factSourceRefPrefix))
	if path == "" {
		evidence.Reason = "source ref names no page"
		return evidence
	}
	if _, isFact := factSearchClaimID(path); isFact {
		// A fact cannot be its own document. Promoting from the fact plane would
		// let an agent_confirmed claim launder itself into primary_document by
		// citing the row it just wrote.
		evidence.Reason = "a fact reference cannot vouch for a fact"
		return evidence
	}

	page, err := s.ReadPage(path)
	if err != nil || page == nil {
		evidence.Reason = "source page is not readable in this wiki"
		return evidence
	}
	evidence.Path = normalizePagePath(path)

	basis, ok := parseFactSourceDate(page.Meta.Updated)
	if !ok {
		// Without the document's own date there is nothing to order competing
		// document claims by, and the amount/deadline/contract policies are
		// exactly the ones that need that ordering.
		evidence.Reason = "source page has no usable date"
		return evidence
	}
	if !factContainsBoundedValue(page.Body, value) {
		evidence.Reason = "source page does not contain the asserted value"
		return evidence
	}
	evidence.BasisAt = basis
	evidence.Verified = true
	return evidence
}

// VerifyFactSources returns the first ref that proves the value, so a caller can
// pass everything it has and let the store decide which one earns the promotion.
func (s *Store) VerifyFactSources(refs []string, value string) (FactSourceEvidence, bool) {
	var last FactSourceEvidence
	for _, ref := range refs {
		evidence := s.VerifyFactSource(ref, value)
		if evidence.Verified {
			return evidence, true
		}
		if last.Ref == "" || (last.Path == "" && evidence.Path != "") {
			last = evidence
		}
	}
	return last, false
}

func parseFactSourceDate(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, raw, dentime.Location()); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
