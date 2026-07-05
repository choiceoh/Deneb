// deal_records_query.go — deterministic filtering and aggregation over the
// typed deal ledger (deal_records.go).
//
// The ledger's whole point is that 합계/건수/기간 questions become computation
// instead of the model eyeballing prose figures (the measured failure class:
// wiki-qa pipeline-h2 re-summed a documented ~1,068MW into "~1,070MW"). These
// helpers are the computation; the deal_ledger chat tool is the surface.
package wiki

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// DealRecordFilter narrows the ledger. String fields are optional; empty means
// "any". Since/Until compare ISO dates lexically and only against records whose
// Date is itself ISO (free-text dates pass the range filter rather than being
// silently dropped).
type DealRecordFilter struct {
	Counterparty string // fuzzy: normalized containment either way
	Project      string // fuzzy match against record Projects
	DocType      string // substring match (견적/계약/세금계산서…)
	Since        string // YYYY-MM-DD inclusive
	Until        string // YYYY-MM-DD inclusive
}

// QueryDealRecords returns matching records, newest first (ISO dates ordered,
// free-text dates last in recorded order).
func (s *Store) QueryDealRecords(f DealRecordFilter) ([]DealRecord, error) {
	recs, err := s.ListDealRecords()
	if err != nil {
		return nil, err
	}
	out := make([]DealRecord, 0, len(recs))
	for _, r := range recs {
		if !matchDealRecord(r, f) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := isoDate(out[i].Date), isoDate(out[j].Date)
		switch {
		case di && dj:
			return out[i].Date > out[j].Date
		case di != dj:
			return di // ISO-dated rows before free-text ones
		default:
			return out[i].RecordedAt > out[j].RecordedAt
		}
	})
	return out, nil
}

func matchDealRecord(r DealRecord, f DealRecordFilter) bool {
	if cp := strings.TrimSpace(f.Counterparty); cp != "" && !fuzzyNameMatch(r.Counterparty, cp) {
		return false
	}
	if pj := strings.TrimSpace(f.Project); pj != "" {
		hit := false
		for _, p := range r.Projects {
			if fuzzyNameMatch(p, pj) {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	if dt := strings.TrimSpace(f.DocType); dt != "" && !strings.Contains(r.DocType, dt) {
		return false
	}
	if f.Since != "" && isoDate(r.Date) && r.Date < f.Since {
		return false
	}
	if f.Until != "" && isoDate(r.Date) && r.Date > f.Until {
		return false
	}
	return true
}

// fuzzyNameMatch reports whether two company/project names refer to each other:
// normalized (lowercase, letters+digits) containment in either direction, so
// "ja solar" matches "JA Solar" and "대한전선" matches "대한전선(주)". Guards very
// short keys the same way the anchors do.
func fuzzyNameMatch(a, b string) bool {
	ka, kb := normalizeTitleKey(a), normalizeTitleKey(b)
	if utf8.RuneCountInString(ka) < 2 || utf8.RuneCountInString(kb) < 2 {
		return false
	}
	return strings.Contains(ka, kb) || strings.Contains(kb, ka)
}

func isoDate(s string) bool {
	return len(s) == 10 && s[4] == '-' && s[7] == '-'
}

// DealTotals is the deterministic aggregate of a record set. Amounts sum per
// currency and only over parsed rows; unparsed rows are counted and sampled so
// the model reports them instead of guessing their value.
type DealTotals struct {
	Count           int
	SumByCurrency   map[string]float64 // parsed rows; key "" folded into "?"
	CountByCurrency map[string]int
	UnparsedCount   int
	UnparsedSamples []string // up to 3 raw amount strings
}

// SumDealRecords computes totals over recs. Pure function — no store access.
func SumDealRecords(recs []DealRecord) DealTotals {
	t := DealTotals{
		SumByCurrency:   map[string]float64{},
		CountByCurrency: map[string]int{},
	}
	for _, r := range recs {
		t.Count++
		if !r.AmountParsed {
			if strings.TrimSpace(r.AmountRaw) != "" {
				t.UnparsedCount++
				if len(t.UnparsedSamples) < 3 {
					t.UnparsedSamples = append(t.UnparsedSamples, r.AmountRaw)
				}
			}
			continue
		}
		cur := r.Currency
		if cur == "" {
			cur = "?"
		}
		t.SumByCurrency[cur] += r.AmountValue
		t.CountByCurrency[cur]++
	}
	return t
}
