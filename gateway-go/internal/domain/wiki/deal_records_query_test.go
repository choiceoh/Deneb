package wiki

import (
	"path/filepath"
	"testing"
	"time"
)

func seedDealStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	file := func(in DealPageInput) {
		t.Helper()
		if _, _, err := s.UpsertDealPage(in, now); err != nil {
			t.Fatalf("UpsertDealPage(%s): %v", in.Counterparty, err)
		}
	}
	file(DealPageInput{
		Counterparty: "JA Solar", DocType: "견적서", Amount: "$1,200,000",
		Date: "2026-06-22", SourceRef: "mail:ja1",
		RelatedProjects: []string{"프로젝트/당진-솔라빌리지/대표.md"},
	})
	file(DealPageInput{
		Counterparty: "JA Solar", DocType: "견적서", Amount: "$800,000.50",
		Date: "2026-06-24", SourceRef: "mail:ja2",
	})
	file(DealPageInput{
		Counterparty: "대한전선", DocType: "세금계산서", Amount: "5,000,000원",
		Date: "2026-05-10", SourceRef: "mail:dh1",
	})
	file(DealPageInput{
		Counterparty: "대한전선", DocType: "견적서", Amount: "오백만원", // spelled-out → unparsed
		Date: "지난주", SourceRef: "mail:dh2",
	})
	return s
}

func TestQueryDealRecords(t *testing.T) {
	s := seedDealStore(t)

	// Fuzzy counterparty: lowercase/space variant matches, newest ISO date first.
	recs, err := s.QueryDealRecords(DealRecordFilter{Counterparty: "ja solar"})
	if err != nil {
		t.Fatalf("QueryDealRecords: %v", err)
	}
	if len(recs) != 2 || recs[0].Date != "2026-06-24" || recs[1].Date != "2026-06-22" {
		t.Fatalf("ja solar query = %+v", recs)
	}
	// Project name resolved from the rep-page path at file time.
	if len(recs[1].Projects) != 1 || recs[1].Projects[0] != "당진-솔라빌리지" {
		t.Errorf("projects = %+v", recs[1].Projects)
	}

	// Project filter.
	recs, _ = s.QueryDealRecords(DealRecordFilter{Project: "당진 솔라빌리지"})
	if len(recs) != 1 || recs[0].SourceRef != "mail:ja1" {
		t.Errorf("project filter = %+v", recs)
	}

	// Date range keeps free-text-dated rows (never silently dropped).
	recs, _ = s.QueryDealRecords(DealRecordFilter{Counterparty: "대한전선", Since: "2026-06-01"})
	if len(recs) != 1 || recs[0].Date != "지난주" {
		t.Errorf("since filter = %+v", recs)
	}

	// DocType substring.
	recs, _ = s.QueryDealRecords(DealRecordFilter{DocType: "견적"})
	if len(recs) != 3 {
		t.Errorf("docType filter = %d rows, want 3", len(recs))
	}
}

func TestSumDealRecords(t *testing.T) {
	s := seedDealStore(t)
	recs, err := s.QueryDealRecords(DealRecordFilter{})
	if err != nil {
		t.Fatalf("QueryDealRecords: %v", err)
	}
	tot := SumDealRecords(recs)
	if tot.Count != 4 {
		t.Errorf("count = %d", tot.Count)
	}
	if got := tot.SumByCurrency["USD"]; got != 2_000_000.50 {
		t.Errorf("USD sum = %v", got)
	}
	if got := tot.SumByCurrency["KRW"]; got != 5_000_000 {
		t.Errorf("KRW sum = %v", got)
	}
	if tot.CountByCurrency["USD"] != 2 || tot.CountByCurrency["KRW"] != 1 {
		t.Errorf("currency counts = %+v", tot.CountByCurrency)
	}
	// Spelled-out Korean amount stays out of sums and is sampled for reporting.
	if tot.UnparsedCount != 1 || len(tot.UnparsedSamples) != 1 || tot.UnparsedSamples[0] != "오백만원" {
		t.Errorf("unparsed = %d %+v", tot.UnparsedCount, tot.UnparsedSamples)
	}
}
