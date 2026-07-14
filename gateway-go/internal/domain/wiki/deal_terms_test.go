package wiki

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseCapacityMW(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"2,940.5kW", 2.9405, true},
		{"3.5MW", 3.5, true},
		{"500kWp", 0.5, true},
		{"1.2 MWp", 1.2, true},
		{"2,940.5kW 기준", 2.9405, true}, // trailing prose ignored
		{"12,500m", 0, false},          // meters are not power
		{"3000", 0, false},             // unit-mandatory
		{"", 0, false},
		{"협의 중", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseCapacityMW(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseCapacityMW(%q) = %v,%v want %v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDealRecordFrom_PreservesTerms(t *testing.T) {
	in := DealPageInput{
		Counterparty: "남도에코",
		Terms: &DealTerms{
			Capacity:  QuotedTerm{Value: "2,940.5kW", Quote: "설비용량: 2,940.5kW 기준"},
			UnitPrice: QuotedTerm{Value: "25,600원/m", Quote: "단가: 25,600원/m"},
		},
	}
	rec := dealRecordFrom(in, time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC))
	if rec.Terms == nil {
		t.Fatal("terms must persist onto the record")
	}
	if rec.Terms.CapacityMW != 2.9405 {
		t.Errorf("CapacityMW = %v, want 2.9405", rec.Terms.CapacityMW)
	}
	if in.Terms.CapacityMW != 0 {
		t.Error("dealRecordFrom must not mutate the caller's Terms")
	}
	// Round-trip through the ledger's JSON encoding: quotes survive, empty
	// terms are omitted.
	line, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	var back DealRecord
	if err := json.Unmarshal(line, &back); err != nil {
		t.Fatal(err)
	}
	if back.Terms == nil || back.Terms.Capacity.Quote != "설비용량: 2,940.5kW 기준" {
		t.Errorf("quote lost in ledger round-trip: %+v", back.Terms)
	}

	// No surviving terms → no Terms on the record at all.
	rec2 := dealRecordFrom(DealPageInput{Counterparty: "x", Terms: &DealTerms{}}, time.Now())
	if rec2.Terms != nil {
		t.Errorf("empty terms must stay off the record, got %+v", rec2.Terms)
	}
}

func TestSumDealRecords_ReturnsCapacityTotals(t *testing.T) {
	recs := []DealRecord{
		{Counterparty: "a", Terms: &DealTerms{CapacityMW: 2.9405}},
		{Counterparty: "b", Terms: &DealTerms{CapacityMW: 0.5}},
		{Counterparty: "c", Terms: &DealTerms{Capacity: QuotedTerm{Value: "협의 중"}}}, // unparsed
		{Counterparty: "d"}, // no terms
	}
	tot := SumDealRecords(recs)
	if tot.CapacityMWSum != 3.4405 || tot.CapacityCount != 2 {
		t.Errorf("capacity totals = %v (%d건), want 3.4405 (2건)", tot.CapacityMWSum, tot.CapacityCount)
	}
}

func TestUpsertDealPage_SavesTermsToLedger(t *testing.T) {
	store := newDealStore(t)
	_, _, err := store.UpsertDealPage(DealPageInput{
		Counterparty: "남도에코",
		DocType:      "견적서",
		SourceRef:    "mail:terms-1",
		Terms: &DealTerms{
			Capacity: QuotedTerm{Value: "3.5MW", Quote: "총 설비용량 3.5MW"},
			Payment:  QuotedTerm{Value: "선수금 30%", Quote: "대금 지급 조건: 선수금 30%"},
		},
	}, time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	recs, err := store.ListDealRecords()
	if err != nil || len(recs) != 1 {
		t.Fatalf("ledger rows = %d (%v), want 1", len(recs), err)
	}
	got := recs[0].Terms
	if got == nil || got.CapacityMW != 3.5 || got.Payment.Value != "선수금 30%" {
		t.Errorf("ledger terms = %+v, want capacity 3.5MW + payment", got)
	}
}
