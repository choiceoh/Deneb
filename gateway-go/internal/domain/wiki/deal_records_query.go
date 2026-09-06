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

	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// DealRecordFilter narrows the ledger. String fields are optional; empty means
// "any". Since/Until compare ISO dates lexically; a row whose Date is not ISO
// carries no month and is excluded from a bounded query — counted in
// DealQueryStats.UndatedExcluded so the caller reports it instead of dropping
// it silently. (Filing normalizes the Korean date shorthands to ISO, so what
// remains here is genuinely dateless: month-only forms, unfilled contract
// blanks — see deal_date.go.)
type DealRecordFilter struct {
	Counterparty string // fuzzy: normalized containment either way
	Project      string // fuzzy match against record Projects
	DocType      string // substring match (견적/계약/세금계산서…)
	Since        string // YYYY-MM-DD inclusive
	Until        string // YYYY-MM-DD inclusive
}

// DealQueryStats reports what a query set aside, so an aggregate answer can say
// what it is not counting.
type DealQueryStats struct {
	// UndatedExcluded counts rows dropped by an active Since/Until bound
	// because their Date is not an ISO date. Always 0 for an unbounded query.
	UndatedExcluded int
}

// QueryDealRecords returns matching records, newest first (ISO dates ordered,
// free-text dates last in recorded order).
func (s *Store) QueryDealRecords(f DealRecordFilter) ([]DealRecord, error) {
	recs, _, err := s.QueryDealRecordsWithStats(f)
	return recs, err
}

// QueryDealRecordsWithStats is QueryDealRecords plus what the date bounds set
// aside — the surface a 월별 집계 needs to qualify its own totals.
func (s *Store) QueryDealRecordsWithStats(f DealRecordFilter) ([]DealRecord, DealQueryStats, error) {
	var stats DealQueryStats
	recs, err := s.ListDealRecords()
	if err != nil {
		return nil, stats, err
	}
	// Range bounds must themselves be ISO or they silently mis-filter via
	// lexical comparison (" 2026-06-01", "지난달"): trim, and drop non-ISO.
	f.Since = strings.TrimSpace(f.Since)
	if !isoDate(f.Since) {
		f.Since = ""
	}
	f.Until = strings.TrimSpace(f.Until)
	if !isoDate(f.Until) {
		f.Until = ""
	}
	// Rows teed before the projects field existed carry none even though the
	// ledger page's Related already names the projects — backfill from the
	// page at query time (memoized per counterparty) so the project filter
	// works across old data too.
	var ledgerProjectsMemo map[string][]string
	if strings.TrimSpace(f.Project) != "" {
		ledgerProjectsMemo = map[string][]string{}
	}
	out := make([]DealRecord, 0, len(recs))
	for _, r := range recs {
		if ledgerProjectsMemo != nil && len(r.Projects) == 0 {
			key := dealSlug(r.Counterparty)
			if _, ok := ledgerProjectsMemo[key]; !ok {
				ledgerProjectsMemo[key] = s.ledgerRelatedProjects(r.Counterparty)
			}
			r.Projects = ledgerProjectsMemo[key]
		}
		if !matchDealRecord(r, f) {
			continue
		}
		// Checked after the other filters so the count describes only rows the
		// query was otherwise about: an undated row has no month to place it
		// in, and letting it through would land it in every bucket a 월별 추이
		// asks for. Counted out loud rather than dropped silently.
		if (f.Since != "" || f.Until != "") && !isoDate(r.Date) {
			stats.UndatedExcluded++
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
	return out, stats, nil
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

// isoDate reports whether s is a plausible YYYY-MM-DD: digits in every date
// position and month/day in range — hyphen positions alone would let strings
// like "2026-0a-01" through, where lexical range comparison mis-filters.
func isoDate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for _, i := range []int{0, 1, 2, 3, 5, 6, 8, 9} {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	month := int(s[5]-'0')*10 + int(s[6]-'0')
	day := int(s[8]-'0')*10 + int(s[9]-'0')
	return month >= 1 && month <= 12 && day >= 1 && day <= 31
}

// ledgerRelatedProjects resolves a counterparty's project names from its ledger
// page's Related links — the query-time backfill for pre-projects ledger rows.
func (s *Store) ledgerRelatedProjects(counterparty string) []string {
	slug := dealSlug(counterparty)
	if slug == "" {
		return nil
	}
	page, err := s.ReadPage(dealCategoryDir + "/" + slug + ".md")
	if err != nil || page == nil {
		return nil
	}
	var names []string
	for _, rel := range page.Meta.Related {
		rel = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(rel), "[["), "]]")
		if name, ok := ProjectNameOf(rel); ok {
			names = append(names, name)
		}
	}
	return textutil.DedupeStrings(names)
}

// DealTotals is the deterministic aggregate of a record set. Amounts sum per
// currency and only over parsed rows; unparsed rows are counted and sampled so
// the model reports them instead of guessing their value.
type DealTotals struct {
	Count           int                  // deals, i.e. rows AFTER same-deal collapse
	RowCount        int                  // ledger rows examined
	DuplicateRows   int                  // rows dropped as same-deal duplicates
	DuplicateGroups []DealDuplicateGroup // largest first, up to dealDuplicateSamples
	SumByCurrency   map[string]float64   // parsed rows; key "" folded into "?"
	CountByCurrency map[string]int
	UnparsedCount   int
	UnparsedSamples []string // up to 3 raw amount strings
	NoAmountCount   int      // rows filed with no amount at all — also outside sums
	CapacityMWSum   float64  // quote-verified, parsed capacities (deal_terms.go)
	CapacityCount   int      // rows contributing to CapacityMWSum
}

// dealDuplicateSamples bounds the groups carried for reporting, the same way
// UnparsedSamples is bounded.
const dealDuplicateSamples = 3

// SumDealRecords computes totals over recs. Pure function — no store access.
//
// Rows collapse to deals first (deal_dedup.go): the ledger holds one row per
// filed document and one contract routinely arrives as several documents, so
// summing rows double-counts it. Every caller wants the deal total, never the
// document total, which is why the collapse lives here rather than behind an
// opt-in — a wrong number should stop being produced everywhere at once.
func SumDealRecords(recs []DealRecord) DealTotals {
	deals, dups := CollapseDealDuplicates(recs)
	t := DealTotals{
		RowCount:        len(recs),
		DuplicateRows:   len(recs) - len(deals),
		SumByCurrency:   map[string]float64{},
		CountByCurrency: map[string]int{},
	}
	if len(dups) > dealDuplicateSamples {
		dups = dups[:dealDuplicateSamples]
	}
	t.DuplicateGroups = dups
	for _, r := range deals {
		t.Count++
		if r.Terms != nil && r.Terms.CapacityMW > 0 {
			t.CapacityMWSum += r.Terms.CapacityMW
			t.CapacityCount++
		}
		if !r.AmountParsed {
			if strings.TrimSpace(r.AmountRaw) != "" {
				t.UnparsedCount++
				if len(t.UnparsedSamples) < 3 {
					t.UnparsedSamples = append(t.UnparsedSamples, r.AmountRaw)
				}
			} else {
				t.NoAmountCount++
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
