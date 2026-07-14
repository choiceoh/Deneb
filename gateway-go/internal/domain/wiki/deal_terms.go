// deal_terms.go — quote-verified commercial terms on the typed deal ledger
// (사실 레이어 2단계 B).
//
// The mail pipeline's quote gate (platform/mailanalysis/deal_facts.go) already
// guarantees each term's value came verbatim from the source document. This
// file gives those terms a persistent, computable home: DealTerms rides on the
// DealRecord ledger row (with the audit quote kept), and capacity additionally
// parses to a numeric MW so "이 거래처 총 몇 MW" becomes deterministic
// summation instead of the model eyeballing prose — the same motivation as
// AmountValue/parseAmount in deal_records.go.
package wiki

import (
	"regexp"
	"strconv"
	"strings"
)

// QuotedTerm is one commercial term plus the verbatim source sentence that
// backs it. The quote is provenance already verified upstream; keeping it on
// the ledger makes every row auditable ("어느 문장에서 나온 값인가") long after
// the mail scrolls away.
type QuotedTerm struct {
	Value string `json:"value"`
	Quote string `json:"quote,omitempty"`
}

// Empty reports whether the term carries no value.
func (q QuotedTerm) Empty() bool { return strings.TrimSpace(q.Value) == "" }

// DealTerms are the quote-verified terms of one filed document. Every field is
// optional — a term exists only if it survived the verbatim-quote gate.
// CapacityMW is the parse of Capacity.Value (0 = unparsed; raw always kept).
type DealTerms struct {
	Capacity     QuotedTerm `json:"capacity,omitzero"`
	CapacityMW   float64    `json:"capacityMW,omitempty"`
	UnitPrice    QuotedTerm `json:"unitPrice,omitzero"`
	Payment      QuotedTerm `json:"payment,omitzero"`
	Warranty     QuotedTerm `json:"warranty,omitzero"`
	DelayPenalty QuotedTerm `json:"delayPenalty,omitzero"`
}

// Empty reports whether no term is present.
func (t *DealTerms) Empty() bool {
	return t == nil ||
		(t.Capacity.Empty() && t.UnitPrice.Empty() && t.Payment.Empty() &&
			t.Warranty.Empty() && t.DelayPenalty.Empty())
}

// capacityRe matches the first capacity figure with an explicit power unit —
// "2,940.5kW", "3.5MW", "500kWp", "1.2 MWp", "3㎿" (commas stripped before
// matching). Deliberately unit-mandatory: a bare number is not a capacity.
var capacityRe = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(mwp?|kwp?|㎿|㎾)`)

// parseCapacityMW parses a free-text 설비용량/물량 into megawatts. ok=false when
// no unit-tagged figure is present — the raw string stays on the record either
// way, so unparsed capacity is surfaced rather than guessed (parseAmount's
// contract).
func parseCapacityMW(raw string) (mw float64, ok bool) {
	s := strings.ReplaceAll(strings.TrimSpace(raw), ",", "")
	if s == "" {
		return 0, false
	}
	m := capacityRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	switch strings.ToLower(m[2]) {
	case "kw", "kwp", "㎾":
		v /= 1000
	}
	return v, true
}
