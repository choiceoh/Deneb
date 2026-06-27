package wiki

import (
	"testing"
	"time"
)

func TestParseAmount(t *testing.T) {
	cases := []struct {
		raw     string
		wantVal float64
		wantCur string
		wantOK  bool
	}{
		{"5,000,000원", 5_000_000, "KRW", true},
		{"₩5,000,000", 5_000_000, "KRW", true},
		{"$1,200", 1200, "USD", true},
		{"$1,200.50", 1200.50, "USD", true},
		{"1200달러", 1200, "USD", true},
		{"1,234,567 KRW", 1_234_567, "KRW", true},
		{"5000000", 5_000_000, "", true},
		{"$0", 0, "USD", true},
		{"", 0, "", false},
		{"미정", 0, "", false},
		{"협의 예정", 0, "", false},
		// Spelled-out / mixed Korean numerals are intentionally out of scope: they
		// must be unparsed (value 0), NOT silently truncated to a leading digit.
		{"오백만원", 0, "KRW", false},
		{"5천만원", 0, "KRW", false}, // must NOT parse as 5
		{"500만", 0, "", false},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			val, cur, ok := ParseAmount(c.raw)
			if ok != c.wantOK || val != c.wantVal || cur != c.wantCur {
				t.Errorf("ParseAmount(%q) = (%v, %q, %v), want (%v, %q, %v)",
					c.raw, val, cur, ok, c.wantVal, c.wantCur, c.wantOK)
			}
		})
	}
}

func TestDealRecordsTee(t *testing.T) {
	s, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

	in1 := DealPageInput{Counterparty: "트라이브", DocType: "견적서", Amount: "5,000,000원", Date: "2026-06-10", SourceRef: "mail:1"}
	in2 := DealPageInput{Counterparty: "트라이브", DocType: "세금계산서", Amount: "$1,200", SourceRef: "mail:2"}
	for _, in := range []DealPageInput{in1, in2} {
		if _, _, err := s.UpsertDealPage(in, now); err != nil {
			t.Fatalf("UpsertDealPage(%s): %v", in.SourceRef, err)
		}
	}
	// Re-filing the same SourceRef is a page no-op → it must NOT tee a 2nd record.
	if _, _, err := s.UpsertDealPage(in1, now); err != nil {
		t.Fatalf("re-file: %v", err)
	}

	recs, err := s.ListDealRecords()
	if err != nil {
		t.Fatalf("ListDealRecords: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 typed records (idempotent re-file), got %d: %+v", len(recs), recs)
	}
	if recs[0].Counterparty != "트라이브" || recs[0].AmountValue != 5_000_000 || recs[0].Currency != "KRW" || !recs[0].AmountParsed {
		t.Errorf("rec0 = %+v", recs[0])
	}
	if recs[0].Date != "2026-06-10" {
		t.Errorf("rec0.Date = %q, want explicit 2026-06-10", recs[0].Date)
	}
	if recs[1].AmountValue != 1200 || recs[1].Currency != "USD" || !recs[1].AmountParsed {
		t.Errorf("rec1 = %+v", recs[1])
	}
	if recs[1].Date != "2026-06-15" { // no Date on in2 → defaults to now
		t.Errorf("rec1.Date = %q, want now-default 2026-06-15", recs[1].Date)
	}
}
