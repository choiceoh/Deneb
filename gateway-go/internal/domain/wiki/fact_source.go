// fact_source.go — checking whether a cited source actually says what a claim
// says, and reporting the answer.
//
// ADR-0005 closed the hole where a model could name its own authority: the
// knowledge tool has no authority field, and writes are capped at
// agent_confirmed. It is tempting to reopen the document authorities by
// "verifying" a source ref — open the page, find the value, promote the claim.
// That does not hold. This wiki has no page-level provenance: `knowledge`
// (op="record") lets the same model author an arbitrary page, and the adapter
// stamps today's date on it, so a model that wants primary_document need only
// write the page it then cites. Because primary_document outranks direct_user
// on amount/deadline/contract, that would launder a model claim over the
// user's own — exactly what this ADR exists to prevent, through another door.
//
// So verification here is EVIDENCE REPORTING, never promotion. The store opens
// the cited page itself and reports whether the page exists, carries a date,
// and contains the asserted value; the caller learns what its citation is
// worth and what would fix a bad one. The recorded authority is unaffected.
// Promotion stays blocked until pages carry provenance that separates
// authenticated ingestion from model-authored text.
package wiki

import (
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// FactSourceEvidence is the outcome of checking one source ref against a value.
// It is advisory: it tells a caller whether its citation holds up, and never
// decides the authority a claim is recorded at (see the file header).
type FactSourceEvidence struct {
	// Ref is the caller-supplied reference, kept for audit even when unverified.
	Ref string
	// Path is the wiki page the ref resolved to, empty when it resolved to none.
	Path string
	// BasisAt is the document's own date — the page's `updated` frontmatter, zero
	// when the page carries none. It is reported for the audit trail only; it
	// does not become a claim's basis, because `updated` is stamped by whoever
	// last wrote the page, including the model itself. A missing date therefore
	// does not sink a citation: nothing downstream needs it.
	BasisAt time.Time
	// Verified reports whether the page exists and actually contains the
	// asserted value.
	Verified bool
	// Checked reports whether this store was able to judge the ref at all. A ref
	// naming another knowledge layer (`f:…`) is not checked here, and must not be
	// reported to a caller as a bad citation — it is simply not this store's to
	// confirm.
	Checked bool
	// Reason explains the outcome, so a caller can be told what would fix a bad
	// citation — or, when Checked is false, why nothing was judged.
	Reason string
}

const factSourceRefPrefix = "w:"

// VerifyFactSource reports whether one source ref backs a claim.
//
// "Backs" is deliberately weaker than "proves": a page containing the value is
// not proof that the page ASSERTS it as current (a rejected option, a history
// line and a live figure all read the same to a substring check), and nothing
// here says who wrote the page. Callers may show this to a model; no caller may
// raise an authority from it.
//
// Two things must hold, and each failure is reported rather than guessed at: the
// ref resolves to a real page in this store, and the asserted value occurs in
// the page body on a token boundary. A near-miss (the page says "120,000,000원"
// while the claim says "1억 2,000만원") deliberately does not verify: this checks
// that a document SAYS the value, which is the only thing a text match can
// honestly establish. The page's date is reported when it has one and is not
// required — it fed the withdrawn promotion, and nothing consumes it now.
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

	if layer, ok := factSourceRefLayer(evidence.Ref); ok && layer != factSourceWikiLayer {
		// Refs carry their layer (`w:` wiki, `f:` files). Only the wiki layer is
		// this store's to open; another layer's ref is a perfectly good citation
		// that simply cannot be judged here.
		evidence.Reason = "source ref names the " + layer + " layer, which this store cannot open"
		return evidence
	}
	evidence.Checked = true

	path := strings.TrimSpace(strings.TrimPrefix(evidence.Ref, factSourceRefPrefix))
	if path == "" {
		evidence.Reason = "source ref names no page"
		return evidence
	}
	if _, isFact := factSearchClaimID(path); isFact {
		// A fact cannot be its own document: citing the row just written would
		// report a claim as corroborated by itself.
		evidence.Reason = "a fact reference cannot vouch for a fact"
		return evidence
	}

	page, err := s.ReadPage(path)
	if err != nil || page == nil {
		evidence.Reason = "source page is not readable in this wiki"
		return evidence
	}
	evidence.Path = normalizePagePath(path)

	if !factSourceStatesValue(page, value) {
		evidence.Reason = "source page does not contain the asserted value"
		return evidence
	}
	evidence.BasisAt, _ = parseFactSourceDate(page.Meta.Updated)
	evidence.Verified = true
	return evidence
}

// VerifyFactSources returns the first ref that backs the value, so a caller can
// pass everything it has and report the one that held up — or, when none did,
// the most informative refusal.
func (s *Store) VerifyFactSources(refs []string, value string) (FactSourceEvidence, bool) {
	var last FactSourceEvidence
	for _, ref := range refs {
		evidence := s.VerifyFactSource(ref, value)
		if evidence.Verified {
			return evidence, true
		}
		// Prefer the most informative refusal: one this store actually judged, and
		// among those the one that at least resolved to a page.
		if last.Ref == "" ||
			(!last.Checked && evidence.Checked) ||
			(last.Checked == evidence.Checked && last.Path == "" && evidence.Path != "") {
			last = evidence
		}
	}
	return last, false
}

const factSourceWikiLayer = "w"

// factSourceRefLayer reports the layer prefix a ref carries, if any. A bare
// wiki path ("프로젝트/abc") and a fact ref ("@facts/…") carry none.
func factSourceRefLayer(ref string) (string, bool) {
	colon := strings.IndexByte(ref, ':')
	if colon <= 0 {
		return "", false
	}
	layer := ref[:colon]
	if strings.ContainsAny(layer, "/\\ ") {
		return "", false
	}
	return layer, true
}

// factSourceStatesValue reports whether the page says the value — in its prose,
// or in one of the frontmatter fields that state facts in their own right, so a
// page whose deadline lives in `due:` rather than in a sentence is not reported
// as failing to mention it.
//
// Each field is searched on its own. Joining them first would let fragments from
// two unrelated fields (`status: 진행중` next to `client: ABC`) run together into
// a phrase neither field states, and this reporting is only honest while every
// match comes from one place that actually holds the text.
func factSourceStatesValue(page *Page, value string) bool {
	meta := page.Meta
	claims := []string{
		page.Body,
		meta.Due, meta.DueDone, meta.Status, meta.Stage, meta.Client, meta.Address,
		meta.ContractDate, meta.ConstructionStart, meta.ModuleDelivery,
		meta.PreUseInspection, meta.CompletionInspection, meta.Summary,
	}
	for _, claim := range claims {
		if claim != "" && factContainsBoundedValue(claim, value) {
			return true
		}
	}
	return false
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
