package wiki

import "testing"

// row builds a parsed-amount ledger row; the fields not named here never take
// part in the identity.
func row(cp, docType, date string, amount float64, cur string, projects ...string) DealRecord {
	return DealRecord{
		Counterparty: cp,
		DocType:      docType,
		Date:         date,
		AmountRaw:    "raw",
		AmountValue:  amount,
		Currency:     cur,
		AmountParsed: true,
		Projects:     projects,
	}
}

// TestCollapseDealDuplicates_CollapsesOneContractFiledFromSeveralMails is the
// defect this file exists for, in its production shape: the 당진 98MW EPC
// contract reached the ledger four times because four mails carried it, and
// summing rows reported it four times.
func TestCollapseDealDuplicates_CollapsesOneContractFiledFromSeveralMails(t *testing.T) {
	const amt = 114_705_800_000
	recs := []DealRecord{
		row("주식회사 해봄에너지", "계약서", "2026-08-13", amt, "KRW", "pl2-dsv-epc-001"),
		row("주식회사 해봄에너지", "계약서", "2026-08-13", amt, "KRW", "pl2-dsv-epc-001"),
		row("주식회사 해봄에너지", "계약서", "2026-08-24", amt, "KRW", "pl2-dsv-epc-001"),
		row("주식회사 해봄에너지", "계약서", "2026-08-24", amt, "KRW", "pl2-dsv-epc-001"),
	}
	deals, groups := CollapseDealDuplicates(recs)
	if len(deals) != 1 {
		t.Fatalf("deals = %d, want 1", len(deals))
	}
	// The representative is the earliest filing — the date a monthly bucket
	// must attribute the contract to.
	if deals[0].Date != "2026-08-13" {
		t.Errorf("representative date = %q, want the earliest filing", deals[0].Date)
	}
	if len(groups) != 1 || groups[0].Rows != 4 {
		t.Fatalf("groups = %+v, want one group of 4", groups)
	}
	if len(groups[0].Dates) != 2 || groups[0].Dates[0] != "2026-08-13" {
		t.Errorf("group dates = %v, want the distinct dates ascending", groups[0].Dates)
	}

	tot := SumDealRecords(recs)
	if tot.SumByCurrency["KRW"] != amt {
		t.Errorf("KRW total = %.0f, want the contract counted once (%.0f)", tot.SumByCurrency["KRW"], float64(amt))
	}
	if tot.Count != 1 || tot.RowCount != 4 || tot.DuplicateRows != 3 {
		t.Errorf("totals = count %d / rows %d / dropped %d, want 1 / 4 / 3", tot.Count, tot.RowCount, tot.DuplicateRows)
	}
}

// TestCollapseDealDuplicates_KeepsSameAmountFromDifferentCounterparties is the
// guard that keeps the collapse conservative. The production ledger carries the
// 경비-식대 category ledger and 고흥남정협동조합 at the same round 100,000원 under
// one project, a month apart — two deals that merely collided on a number.
func TestCollapseDealDuplicates_KeepsSameAmountFromDifferentCounterparties(t *testing.T) {
	recs := []DealRecord{
		row("경비-식대", "기타", "2026-07-20", 100_000, "KRW", "pl1-cny-dev-001"),
		row("고흥남정협동조합", "기타", "2026-08-10", 100_000, "KRW", "pl1-cny-dev-001"),
	}
	deals, groups := CollapseDealDuplicates(recs)
	if len(deals) != 2 || len(groups) != 0 {
		t.Fatalf("deals = %d, groups = %d — a coincidence must not merge", len(deals), len(groups))
	}
}

// TestCollapseDealDuplicates_MergesSpellingVariantsOfOneCounterparty: the same
// company reaches the ledger written several ways ((주)광주일보사 / 광주일보사), and
// the project anchor plus fuzzy name matching has to see through that.
func TestCollapseDealDuplicates_MergesSpellingVariantsOfOneCounterparty(t *testing.T) {
	recs := []DealRecord{
		row("(주)광주일보사", "지출결의", "2026-07-16", 1_500_000, "KRW", "충남-영농형-태양광"),
		row("광주일보사", "지출결의", "2026-07-16", 1_500_000, "KRW", "충남-영농형-태양광"),
	}
	deals, groups := CollapseDealDuplicates(recs)
	if len(deals) != 1 || len(groups) != 1 {
		t.Fatalf("deals = %d, groups = %d, want one merged deal", len(deals), len(groups))
	}
}

// TestCollapseDealDuplicates_SeparatesHardFieldDisagreements: every hard field
// is part of the identity, so a difference in any one of them is two deals.
// The VAT pair (공급가액 vs VAT 포함 of one contract) lands here on purpose —
// no rule can tell it from a genuine second deal, so it stays separate and
// visible rather than being guessed away.
func TestCollapseDealDuplicates_SeparatesHardFieldDisagreements(t *testing.T) {
	base := row("해봄에너지", "계약서", "2026-08-13", 114_705_800_000, "KRW", "pl2-dsv-epc-001")
	cases := map[string]DealRecord{
		"다른 금액(공급가액)": row("해봄에너지", "계약서", "2026-08-10", 104_100_000_000, "KRW", "pl2-dsv-epc-001"),
		"다른 문서종류":     row("해봄에너지", "견적서", "2026-08-13", 114_705_800_000, "KRW", "pl2-dsv-epc-001"),
		"다른 통화":       row("해봄에너지", "계약서", "2026-08-13", 114_705_800_000, "USD", "pl2-dsv-epc-001"),
		"다른 프로젝트":     row("해봄에너지", "계약서", "2026-08-13", 114_705_800_000, "KRW", "pl2-ysg-epc-001"),
	}
	for name, other := range cases {
		t.Run(name, func(t *testing.T) {
			deals, _ := CollapseDealDuplicates([]DealRecord{base, other})
			if len(deals) != 2 {
				t.Fatalf("deals = %d, want 2 — %s must stay a separate deal", len(deals), name)
			}
		})
	}
}

// TestCollapseDealDuplicates_NeverMergesUnparsedRows: a row whose amount did
// not parse has no amount to agree on, so it can only pass through. Without
// this, several 미파싱 rows of one counterparty would silently become one.
func TestCollapseDealDuplicates_NeverMergesUnparsedRows(t *testing.T) {
	unparsed := DealRecord{Counterparty: "남도에코", DocType: "견적서", AmountRaw: "오백만원", Projects: []string{"p1"}}
	recs := []DealRecord{unparsed, unparsed, {Counterparty: "남도에코", DocType: "견적서"}}
	deals, groups := CollapseDealDuplicates(recs)
	if len(deals) != 3 || len(groups) != 0 {
		t.Fatalf("deals = %d, groups = %d — unparsed rows must pass through", len(deals), len(groups))
	}
	tot := SumDealRecords(recs)
	if tot.UnparsedCount != 2 || tot.NoAmountCount != 1 || tot.DuplicateRows != 0 {
		t.Errorf("totals = %+v, want the existing unparsed accounting untouched", tot)
	}
}

// TestCollapseDealDuplicates_FallsBackToCounterpartyWithoutProjects: rows filed
// before project resolution carry no project, and must still collapse.
func TestCollapseDealDuplicates_FallsBackToCounterpartyWithoutProjects(t *testing.T) {
	recs := []DealRecord{
		row("진영상사", "계약서", "2026-06-30", 1_573_000_000, "KRW"),
		row("진영상사", "계약서", "2026-06-30", 1_573_000_000, "KRW"),
		row("다른상사", "계약서", "2026-06-30", 1_573_000_000, "KRW"),
	}
	deals, groups := CollapseDealDuplicates(recs)
	if len(deals) != 2 || len(groups) != 1 {
		t.Fatalf("deals = %d, groups = %d, want 진영상사 merged and 다른상사 kept", len(deals), len(groups))
	}
}

// TestCollapseDealDuplicates_PreservesLedgerOrderAndIsStable: callers walk the
// ledger back-to-front for "the most recent one", so the collapse must not
// reorder what it returns, and must return the same thing every run.
func TestCollapseDealDuplicates_PreservesLedgerOrderAndIsStable(t *testing.T) {
	recs := []DealRecord{
		row("A", "계약서", "2026-01-01", 100, "KRW"),
		row("B", "계약서", "2026-02-01", 200, "KRW"),
		row("A", "계약서", "2026-03-01", 100, "KRW"), // duplicate of index 0
		row("C", "계약서", "2026-04-01", 300, "KRW"),
	}
	for i := 0; i < 5; i++ {
		deals, groups := CollapseDealDuplicates(recs)
		if len(deals) != 3 {
			t.Fatalf("deals = %d, want 3", len(deals))
		}
		if deals[0].Counterparty != "A" || deals[1].Counterparty != "B" || deals[2].Counterparty != "C" {
			t.Fatalf("order = %v, want ledger order preserved",
				[]string{deals[0].Counterparty, deals[1].Counterparty, deals[2].Counterparty})
		}
		if len(groups) != 1 || groups[0].Counterparty != "A" {
			t.Fatalf("groups = %+v", groups)
		}
	}
}

// TestSumDealRecords_ReportsMoreGroupsThanItSamples keeps the reporting honest
// when there are more duplicate groups than the sample cap.
func TestSumDealRecords_ReportsMoreGroupsThanItSamples(t *testing.T) {
	var recs []DealRecord
	for i := 0; i < dealDuplicateSamples+2; i++ {
		r := row(string(rune('A'+i)), "계약서", "2026-01-01", float64(100+i), "KRW")
		recs = append(recs, r, r)
	}
	tot := SumDealRecords(recs)
	if tot.DuplicateRows != dealDuplicateSamples+2 {
		t.Errorf("dropped = %d, want %d — the COUNT must cover every group", tot.DuplicateRows, dealDuplicateSamples+2)
	}
	if len(tot.DuplicateGroups) != dealDuplicateSamples {
		t.Errorf("samples = %d, want them capped at %d", len(tot.DuplicateGroups), dealDuplicateSamples)
	}
}
