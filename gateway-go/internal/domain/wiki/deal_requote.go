// deal_requote.go — requote change detection over the typed deal ledger
// (사실 레이어 2단계 C).
//
// Korean solar deals renegotiate by resending 견적서: the price signal the
// operator actually cares about is not "a quote arrived" but "the SAME
// counterparty's quote CHANGED — 단가 148→145원/W". With quote-verified terms
// now persisted per record (deal_terms.go), that comparison is deterministic:
// find the counterparty's previous 견적 row, diff 단가/물량/금액, and surface
// the delta as a dated bullet on the linked project's 현재 상태. No LLM.
package wiki

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// FieldChange is one changed commercial figure between two quotes.
type FieldChange struct {
	Label  string // 단가 | 물량 | 금액
	Old    string // raw display value from the previous record
	New    string // raw display value from the new record
	Pct    float64
	HasPct bool
}

// RequoteChange is the diff between a counterparty's new quote and its most
// recent prior quote on the ledger.
type RequoteChange struct {
	Counterparty string
	PrevDate     string // previous quote's date (ISO or raw)
	Fields       []FieldChange
}

// StatusLine renders the change as one Korean status bullet body (the caller's
// AppendProjectStatusLine adds the date prefix and idempotency marker).
func (c *RequoteChange) StatusLine() string {
	parts := make([]string, 0, len(c.Fields))
	for _, f := range c.Fields {
		p := fmt.Sprintf("%s %s → %s", f.Label, f.Old, f.New)
		if f.HasPct {
			p += fmt.Sprintf(" (%+.1f%%)", f.Pct)
		}
		parts = append(parts, p)
	}
	line := "재견적(" + c.Counterparty + "): " + strings.Join(parts, " · ")
	if c.PrevDate != "" {
		line += " — 직전 견적 " + c.PrevDate + " 대비"
	}
	return line
}

// DetectRequote compares an about-to-be-filed 견적 against the counterparty's
// most recent prior 견적 on the ledger and returns the changed figures, or nil
// when there is nothing comparable or nothing changed. Call BEFORE
// UpsertDealPage tees the new record (otherwise the new row is its own
// "previous"). Deterministic and read-only; re-analysis stays safe because the
// previous-row search skips the same SourceRef and the caller's status append
// is ref-idempotent.
func (s *Store) DetectRequote(in DealPageInput, now time.Time) *RequoteChange {
	if !strings.Contains(in.DocType, "견적") {
		return nil // requote = 견적서 vs 견적서; contracts/invoices are PR scope 밖
	}
	next := dealRecordFrom(in, now)
	recs, err := s.ListDealRecords()
	if err != nil {
		return nil
	}
	var prev *DealRecord
	for i := len(recs) - 1; i >= 0; i-- { // ledger is oldest-first → walk back
		r := recs[i]
		if r.SourceRef != "" && r.SourceRef == next.SourceRef {
			continue // re-analysis of the same mail — not a prior quote
		}
		if !strings.Contains(r.DocType, "견적") {
			continue
		}
		if !fuzzyNameMatch(r.Counterparty, next.Counterparty) {
			continue
		}
		prev = &r
		break
	}
	if prev == nil {
		return nil
	}
	return compareQuotes(*prev, next)
}

// compareQuotes diffs the figures both quotes actually carry. A field missing
// on either side is not a change (no 추측); a unit-price comparison requires
// the SAME unit key so 원/W never diffs against 원/m.
func compareQuotes(prev, next DealRecord) *RequoteChange {
	var fields []FieldChange

	if pv, pu, pok := parseUnitPrice(unitPriceOf(prev.Terms)); pok {
		if nv, nu, nok := parseUnitPrice(unitPriceOf(next.Terms)); nok && pu == nu && pv != nv {
			fields = append(fields, fieldChange("단가", unitPriceOf(prev.Terms), unitPriceOf(next.Terms), pv, nv))
		}
	}
	if prev.Terms != nil && next.Terms != nil &&
		prev.Terms.CapacityMW > 0 && next.Terms.CapacityMW > 0 &&
		prev.Terms.CapacityMW != next.Terms.CapacityMW {
		fields = append(fields, fieldChange("물량", prev.Terms.Capacity.Value, next.Terms.Capacity.Value, prev.Terms.CapacityMW, next.Terms.CapacityMW))
	}
	if prev.AmountParsed && next.AmountParsed && prev.Currency == next.Currency &&
		prev.AmountValue != next.AmountValue {
		fields = append(fields, fieldChange("금액", prev.AmountRaw, next.AmountRaw, prev.AmountValue, next.AmountValue))
	}
	if len(fields) == 0 {
		return nil
	}
	return &RequoteChange{Counterparty: next.Counterparty, PrevDate: prev.Date, Fields: fields}
}

func unitPriceOf(t *DealTerms) string {
	if t == nil {
		return ""
	}
	return t.UnitPrice.Value
}

func fieldChange(label, oldRaw, newRaw string, oldV, newV float64) FieldChange {
	f := FieldChange{Label: label, Old: strings.TrimSpace(oldRaw), New: strings.TrimSpace(newRaw)}
	if oldV != 0 {
		f.Pct = (newV - oldV) / oldV * 100
		f.HasPct = true
	}
	return f
}

// unitPriceRe captures the first number (commas/decimal as written) and the
// remainder, which becomes the unit key.
var unitPriceRe = regexp.MustCompile(`([0-9][0-9,.]*)\s*(.*)`)

// parseUnitPrice splits a free-text 단가 into its leading number and a
// normalized unit key ("25,600원/m" → 25600, "원/m"). Comparisons require equal
// unit keys, so prices in different units never diff against each other.
// ok=false when no ASCII number is present ("협의 중") or the number is a
// spelled-out Korean amount prefix ("5천원/W") — same out-of-scope contract as
// parseAmount.
func parseUnitPrice(raw string) (val float64, unit string, ok bool) {
	m := unitPriceRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, "", false
	}
	rest := m[2]
	// Korean-numeral guard on the first rune after the digits. Empty rest
	// decodes to RuneError, which is not in koreanNumeral, so it passes.
	if r, _ := utf8.DecodeRuneInString(rest); koreanNumeral[r] {
		return 0, "", false
	}
	num := strings.TrimRight(strings.ReplaceAll(m[1], ",", ""), ".")
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, "", false
	}
	unit = strings.ToLower(strings.Join(strings.Fields(rest), ""))
	return v, unit, true
}
