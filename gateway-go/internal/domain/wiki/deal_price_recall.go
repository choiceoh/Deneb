// deal_price_recall.go — deterministic price-memory recall over the typed
// deal ledger.
//
// Two read surfaces for the approval-analysis loop:
//
//   - PriceHistoryContext: BEFORE the LLM sees a new 결재 문서, match its text
//     against the ledger's known vocabulary (품목명·경비 카테고리·거래처) and
//     render the matching price history as a compact Korean block for prompt
//     injection — the "past price" the operator wants the AI to remember.
//   - PriceDeltaLines: AFTER extraction, diff the new document's figures
//     against each item's / expense category's prior ledger rows — exact,
//     no-LLM deltas (DetectRequote's philosophy extended item-wise).
//
// Both are read-only and deterministic; matching uses normalizeTitleKey
// containment (fuzzyNameMatch's contract) so spelling variants match without a
// curated alias map.
package wiki

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	priceRecallPointsPerItem = 3    // price points echoed per matched item
	priceRecallExpenseWindow = 6    // amount series window per expense kind
	priceRecallMaxLines      = 10   // hard cap on injected lines
	priceRecallMaxRunes      = 1600 // hard cap on the injected block
	priceRecallMinKeyRunes   = 2    // same guard as fuzzyNameMatch
)

// pricePoint is one dated price observation collected from the ledger.
type pricePoint struct {
	display string // rendered fragment, e.g. "145원/W (2026-05-14 · 진코솔라)"
}

// itemNameTokens splits an item name into normalized tokens (≥2 runes) for
// order-insensitive matching — "HD 640W 모듈" and "640W 모듈 2차 발주" share
// the tokens {640w, 모듈} even though neither name contains the other.
func itemNameTokens(name string) []string {
	var out []string
	for _, f := range strings.Fields(name) {
		t := normalizeTitleKey(f)
		if utf8.RuneCountInString(t) >= priceRecallMinKeyRunes {
			out = append(out, t)
		}
	}
	return out
}

// itemTokensInText reports whether an item's tokens appear in normalized text:
// every token for single/double-token names, at least two tokens otherwise
// (precision-first — one generic token like "모듈" alone never matches).
func itemTokensInText(tokens []string, normText string) bool {
	if len(tokens) == 0 {
		return false
	}
	matched := 0
	for _, t := range tokens {
		if strings.Contains(normText, t) {
			matched++
		}
	}
	if len(tokens) <= 2 {
		return matched == len(tokens)
	}
	return matched >= 2
}

// itemNamesMatch reports whether two item names refer to the same 품목: their
// token sets overlap fully on the smaller side, or share at least two tokens.
func itemNamesMatch(a, b string) bool {
	ta, tb := itemNameTokens(a), itemNameTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	set := make(map[string]bool, len(ta))
	for _, t := range ta {
		set[t] = true
	}
	common := 0
	for _, t := range tb {
		if set[t] {
			common++
		}
	}
	minLen := len(ta)
	if len(tb) < minLen {
		minLen = len(tb)
	}
	return common == minLen || common >= 2
}

// PriceHistoryContext renders the ledger's price history relevant to text (a
// new 결재 제목+본문) as a compact block for prompt injection, or "" when the
// ledger knows nothing matching. Deterministic and read-only.
func (s *Store) PriceHistoryContext(text string) string {
	recs, err := s.ListDealRecords()
	if err != nil || len(recs) == 0 {
		return ""
	}
	normText := normalizeTitleKey(text)
	if normText == "" {
		return ""
	}

	var lines []string

	// Axis 1 — 품목 단가: line items whose name appears in the text.
	lines = append(lines, s.matchItemHistory(recs, normText)...)
	// Axis 2 — 반복 경비: expense kinds appearing in the text → amount series.
	lines = append(lines, matchExpenseHistory(recs, normText)...)
	// Axis 3 — 거래처 문서 단가: counterparty names appearing in the text →
	// document-level quote-verified terms (mail-filed 견적 history).
	lines = append(lines, matchCounterpartyHistory(recs, normText)...)

	if len(lines) == 0 {
		return ""
	}
	if len(lines) > priceRecallMaxLines {
		lines = lines[:priceRecallMaxLines]
	}
	block := strings.Join(lines, "\n")
	if utf8.RuneCountInString(block) > priceRecallMaxRunes {
		block = string([]rune(block)[:priceRecallMaxRunes]) + "…"
	}
	return block
}

// matchItemHistory collects per-item unit-price points for ledger line items
// whose normalized name is contained in normText.
func (s *Store) matchItemHistory(recs []DealRecord, normText string) []string {
	type itemHist struct {
		display string
		points  []pricePoint
	}
	hist := map[string]*itemHist{}
	var order []string
	// Walk newest-first so points render most-recent first.
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		for _, li := range r.LineItems {
			tokens := itemNameTokens(li.Name)
			if !itemTokensInText(tokens, normText) {
				continue
			}
			key := strings.Join(tokens, " ")
			h, ok := hist[key]
			if !ok {
				h = &itemHist{display: li.Name}
				hist[key] = h
				order = append(order, key)
			}
			if len(h.points) >= priceRecallPointsPerItem {
				continue
			}
			frag := strings.TrimSpace(li.UnitPrice)
			if frag == "" {
				frag = strings.TrimSpace(li.Amount)
			}
			if frag == "" {
				continue
			}
			if q := strings.TrimSpace(li.Qty); q != "" {
				frag += " × " + q
			}
			frag += " (" + pointMeta(r) + ")"
			h.points = append(h.points, pricePoint{display: frag})
		}
	}
	var lines []string
	for _, key := range order {
		h := hist[key]
		if len(h.points) == 0 {
			continue
		}
		frags := make([]string, 0, len(h.points))
		for _, p := range h.points {
			frags = append(frags, p.display)
		}
		lines = append(lines, "- 품목 "+h.display+": "+strings.Join(frags, " · "))
	}
	return lines
}

// matchExpenseHistory renders the amount series of expense kinds contained in
// normText, with the median as the comparison anchor.
func matchExpenseHistory(recs []DealRecord, normText string) []string {
	type expHist struct {
		display  string
		currency string
		amounts  []float64
		frags    []string
	}
	hist := map[string]*expHist{}
	var order []string
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		key := normalizeTitleKey(r.ExpenseKind)
		if utf8.RuneCountInString(key) < priceRecallMinKeyRunes || !strings.Contains(normText, key) {
			continue
		}
		if !r.AmountParsed {
			continue
		}
		h, ok := hist[key]
		if !ok {
			h = &expHist{display: r.ExpenseKind, currency: r.Currency}
			hist[key] = h
			order = append(order, key)
		}
		if r.Currency != h.currency || len(h.amounts) >= priceRecallExpenseWindow {
			continue
		}
		h.amounts = append(h.amounts, r.AmountValue)
		h.frags = append(h.frags, formatMoney(r.AmountValue, r.Currency)+"("+shortDate(r.Date)+")")
	}
	var lines []string
	for _, key := range order {
		h := hist[key]
		if len(h.amounts) == 0 {
			continue
		}
		line := "- 경비 " + h.display + " 최근 " + strconv.Itoa(len(h.amounts)) + "건: " + strings.Join(h.frags, " · ")
		if len(h.amounts) >= 2 {
			line += " — 중앙값 " + formatMoney(medianOf(h.amounts), h.currency)
		}
		lines = append(lines, line)
	}
	return lines
}

// matchCounterpartyHistory renders document-level quote-verified terms of
// counterparties named in normText (the mail-filed 견적/계약 history).
func matchCounterpartyHistory(recs []DealRecord, normText string) []string {
	type cpHist struct {
		display string
		frags   []string
	}
	hist := map[string]*cpHist{}
	var order []string
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		key := normalizeTitleKey(r.Counterparty)
		if utf8.RuneCountInString(key) < priceRecallMinKeyRunes || !strings.Contains(normText, key) {
			continue
		}
		up := ""
		if r.Terms != nil {
			up = strings.TrimSpace(r.Terms.UnitPrice.Value)
		}
		if up == "" {
			continue // only unit-price-bearing documents are price memory here
		}
		h, ok := hist[key]
		if !ok {
			h = &cpHist{display: r.Counterparty}
			hist[key] = h
			order = append(order, key)
		}
		if len(h.frags) >= priceRecallPointsPerItem {
			continue
		}
		frag := r.DocType
		if frag != "" {
			frag += " "
		}
		frag += "단가 " + up + " (" + shortDate(r.Date) + ")"
		h.frags = append(h.frags, frag)
	}
	var lines []string
	for _, key := range order {
		h := hist[key]
		lines = append(lines, "- 거래처 "+h.display+": "+strings.Join(h.frags, " · "))
	}
	return lines
}

// PriceDeltaLines diffs an about-to-be-filed document's figures against the
// ledger and returns exact Korean delta lines (nil when nothing comparable).
// Call BEFORE UpsertDealPage tees the new record (DetectRequote's contract);
// the same-SourceRef guard keeps re-analysis safe either way.
func (s *Store) PriceDeltaLines(in DealPageInput) []string {
	recs, err := s.ListDealRecords()
	if err != nil || len(recs) == 0 {
		return nil
	}
	var lines []string

	// Item axis: each new line item vs its most recent prior ledger row with
	// the same normalized name and the same unit key.
	for _, li := range in.LineItems {
		nv, nu, nok := parseUnitPrice(li.UnitPrice)
		if !nok {
			continue
		}
		prev, prevRec := findPrevLineItem(recs, li.Name, nu, in.SourceRef)
		if prev == nil {
			continue
		}
		pv, _, _ := parseUnitPrice(prev.UnitPrice)
		line := "품목 " + strings.TrimSpace(li.Name) + ": 단가 " + strings.TrimSpace(prev.UnitPrice) +
			" → " + strings.TrimSpace(li.UnitPrice)
		if pv != 0 && pv != nv {
			line += fmt.Sprintf(" (%+.1f%%)", (nv-pv)/pv*100)
		} else if pv == nv {
			line += " (동일)"
		}
		line += " — 직전 " + shortDate(prevRec.Date)
		if cp := strings.TrimSpace(prevRec.Counterparty); cp != "" {
			line += " " + cp
		}
		lines = append(lines, line)
	}

	// Expense axis: new amount vs the category's recent median.
	if kind := strings.TrimSpace(in.ExpenseKind); kind != "" {
		if nv, cur, ok := parseAmount(in.Amount); ok {
			key := normalizeTitleKey(kind)
			var amounts []float64
			n := 0
			for i := len(recs) - 1; i >= 0 && n < priceRecallExpenseWindow; i-- {
				r := recs[i]
				if r.SourceRef != "" && r.SourceRef == strings.TrimSpace(in.SourceRef) {
					continue
				}
				if normalizeTitleKey(r.ExpenseKind) != key || !r.AmountParsed || r.Currency != cur {
					continue
				}
				amounts = append(amounts, r.AmountValue)
				n++
			}
			if len(amounts) > 0 {
				med := medianOf(amounts)
				line := "경비 " + kind + ": " + formatMoney(nv, cur) + " — 최근 " +
					strconv.Itoa(len(amounts)) + "건 중앙값 " + formatMoney(med, cur)
				if med != 0 {
					line += fmt.Sprintf(" 대비 %+.1f%%", (nv-med)/med*100)
				}
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// findPrevLineItem walks the ledger newest-first for the latest line item
// matching name (fuzzy) with the same unit key, skipping the new document's
// own SourceRef.
func findPrevLineItem(recs []DealRecord, name, unit, sourceRef string) (*DealLineItem, *DealRecord) {
	sourceRef = strings.TrimSpace(sourceRef)
	for i := len(recs) - 1; i >= 0; i-- {
		r := recs[i]
		if r.SourceRef != "" && r.SourceRef == sourceRef {
			continue
		}
		for j := range r.LineItems {
			li := &r.LineItems[j]
			if !itemNamesMatch(li.Name, name) {
				continue
			}
			if _, u, ok := parseUnitPrice(li.UnitPrice); !ok || u != unit {
				continue
			}
			return li, &r
		}
	}
	return nil, nil
}

// pointMeta renders the "(date · counterparty · docType)" suffix of one point.
func pointMeta(r DealRecord) string {
	parts := make([]string, 0, 3)
	if d := shortDate(r.Date); d != "" {
		parts = append(parts, d)
	}
	if cp := strings.TrimSpace(r.Counterparty); cp != "" {
		parts = append(parts, cp)
	}
	if dt := strings.TrimSpace(r.DocType); dt != "" {
		parts = append(parts, dt)
	}
	return strings.Join(parts, " · ")
}

func shortDate(d string) string { return strings.TrimSpace(d) }

// formatMoney renders a parsed amount back to compact Korean form. KRW (and
// unknown currency) renders as a comma-grouped integer + 원; others keep the
// ISO code suffix.
func formatMoney(v float64, currency string) string {
	switch currency {
	case "KRW", "":
		return groupThousands(v) + "원"
	default:
		return groupThousands(v) + " " + currency
	}
}

// groupThousands renders v with comma grouping, keeping up to two decimals
// only when v is not integral.
func groupThousands(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := v - float64(whole)
	s := strconv.FormatInt(whole, 10)
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	out := b.String()
	if frac > 0.005 {
		out += strings.TrimPrefix(strconv.FormatFloat(frac, 'f', 2, 64), "0")
	}
	if neg {
		out = "-" + out
	}
	return out
}

func medianOf(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]float64(nil), vals...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}
