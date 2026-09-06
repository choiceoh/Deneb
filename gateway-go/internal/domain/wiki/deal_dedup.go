// deal_dedup.go — collapsing the document ledger into deals before aggregation.
//
// The ledger stores one row per FILED DOCUMENT, and one deal routinely arrives
// as several documents: the 당진 솔라빌리지 98MW EPC 계약 sits on the production
// ledger four times because four different mails carried the same contract.
// Summing rows therefore answers 총 거래액 with that contract counted four
// times — measured on the production ledger (2026-09-06): KRW 716,169,320,938
// against a true 370,477,420,938, a 48.3% overcount. That defeats the ledger's
// whole reason for existing (deal_records.go: "prose cannot be summed", so the
// typed rows exist to make totals deterministic computation).
//
// Aggregation therefore collapses rows to deals first. The rule is deliberately
// conservative — two rows merge only when they agree on EVERY hard field
// (project-or-counterparty anchor, document type, currency, exact amount) AND
// their counterparty names match. An exact-amount coincidence between different
// counterparties must stay two deals: the production ledger has the 경비-식대
// category ledger and 고흥남정협동조합 both at 100,000원 under one project, a
// month apart.
//
// Near-duplicates are NOT merged — the same contract recorded once at 공급가액
// and once VAT-inclusive differ by an amount no rule can distinguish from a
// genuine second deal. Those surface as a reported count, the same contract the
// unparsed-amount reporting already follows: say what you could not resolve
// rather than guess it.
//
// The ledger file is never rewritten. It is append-only evidence; this is a
// pure read-time projection over it.
package wiki

import (
	"sort"
	"strconv"
	"strings"
)

// DealDuplicateGroup describes one set of ledger rows collapsed into a single
// deal, so a read-out can say what it merged instead of silently shrinking.
type DealDuplicateGroup struct {
	Counterparty string
	DocType      string
	AmountRaw    string
	Currency     string
	Rows         int      // rows in the group (always >= 2)
	Dates        []string // distinct dates the rows carried, ascending
}

// CollapseDealDuplicates returns recs with same-deal rows collapsed to one
// representative each, plus a description of every group it collapsed.
//
// The representative is the FIRST row of its group in ledger order. The ledger
// is append-only oldest-first, so that is the earliest filing of the deal — the
// date a monthly bucket should attribute it to. Rows whose amount did not parse
// are never merged (there is no amount to agree on) and pass through untouched.
//
// Order is preserved: representatives appear in their original ledger position,
// which keeps every caller's "most recent last" assumption intact.
func CollapseDealDuplicates(recs []DealRecord) ([]DealRecord, []DealDuplicateGroup) {
	// A bucket is one candidate deal: rows that already agreed on every hard
	// field AND on the counterparty.
	type bucket struct {
		rep  int   // index of the representative in recs
		rows []int // every index in this group, ledger order
	}
	byKey := map[string][]*bucket{}
	keep := make([]bool, len(recs))
	var merged []*bucket

	for i, r := range recs {
		if !r.AmountParsed {
			keep[i] = true // nothing to compare — never merged
			continue
		}
		key := dealDedupKey(r)
		// A project anchor is shared by every counterparty on that project, so
		// rows under it still have to agree on who the deal is with. A
		// counterparty anchor already IS that agreement — re-checking it there
		// would only add fuzzyNameMatch's two-rune floor, which would leave
		// short names (JA, 3M) permanently unmergeable.
		byProject := len(r.Projects) > 0
		var into *bucket
		for _, b := range byKey[key] {
			if !byProject || fuzzyNameMatch(recs[b.rep].Counterparty, r.Counterparty) {
				into = b
				break
			}
		}
		if into == nil {
			byKey[key] = append(byKey[key], &bucket{rep: i, rows: []int{i}})
			keep[i] = true
			continue
		}
		if len(into.rows) == 1 {
			merged = append(merged, into) // first time this bucket collapses anything
		}
		into.rows = append(into.rows, i)
	}

	if len(merged) == 0 {
		return recs, nil
	}

	out := make([]DealRecord, 0, len(recs))
	for i := range recs {
		if keep[i] {
			out = append(out, recs[i])
		}
	}

	// Report largest groups first — that is the order an operator audits them
	// in. Ties fall back to ledger position so the output is deterministic.
	sort.SliceStable(merged, func(i, j int) bool {
		if len(merged[i].rows) != len(merged[j].rows) {
			return len(merged[i].rows) > len(merged[j].rows)
		}
		return merged[i].rep < merged[j].rep
	})

	groups := make([]DealDuplicateGroup, 0, len(merged))
	for _, b := range merged {
		rep := recs[b.rep]
		seen := map[string]bool{}
		var dates []string
		for _, idx := range b.rows {
			if d := strings.TrimSpace(recs[idx].Date); d != "" && !seen[d] {
				seen[d] = true
				dates = append(dates, d)
			}
		}
		sort.Strings(dates)
		groups = append(groups, DealDuplicateGroup{
			Counterparty: rep.Counterparty,
			DocType:      rep.DocType,
			AmountRaw:    rep.AmountRaw,
			Currency:     rep.Currency,
			Rows:         len(b.rows),
			Dates:        dates,
		})
	}
	return out, groups
}

// dealDedupKey is the hard-field identity of a deal: the project it belongs to
// (the join key that survives a counterparty being written three ways), or the
// counterparty when the row predates project resolution, plus document type,
// currency and the exact parsed amount.
func dealDedupKey(r DealRecord) string {
	var anchor string
	if len(r.Projects) > 0 {
		projects := append([]string(nil), r.Projects...)
		sort.Strings(projects)
		anchor = "p:" + strings.Join(projects, "|")
	} else {
		anchor = "c:" + normalizeTitleKey(r.Counterparty)
	}
	cur := r.Currency
	if cur == "" {
		cur = "?"
	}
	return anchor + "\x00" + strings.TrimSpace(r.DocType) +
		"\x00" + cur + "\x00" + strconv.FormatFloat(r.AmountValue, 'f', 2, 64)
}
