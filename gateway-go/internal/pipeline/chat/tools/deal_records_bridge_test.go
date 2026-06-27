package tools

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// TestDealsStructured exercises the code_action bridge surface that exposes the
// typed deal-record ledger to the Python sandbox (UaC #2): the agent computes
// sums/counts over typed records instead of eyeballing prose pages.
func TestDealsStructured(t *testing.T) {
	store, err := wiki.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	for _, in := range []wiki.DealPageInput{
		{Counterparty: "트라이브", DocType: "견적서", Amount: "5,000,000원", SourceRef: "m1"},
		{Counterparty: "트라이브", DocType: "세금계산서", Amount: "$1,200", SourceRef: "m2"},
		{Counterparty: "현대", DocType: "계약서", Amount: "3,000,000원", SourceRef: "m3"},
	} {
		if _, _, err := store.UpsertDealPage(in, now); err != nil {
			t.Fatalf("UpsertDealPage %s: %v", in.SourceRef, err)
		}
	}

	asRecords := func(v any) []wiki.DealRecord {
		t.Helper()
		recs, ok := v.([]wiki.DealRecord)
		if !ok {
			t.Fatalf("structured result is %T, want []wiki.DealRecord", v)
		}
		return recs
	}

	// No filter → every record.
	all, err := dealsStructured(store, map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("dealsStructured all: %v", err)
	}
	if got := len(asRecords(all)); got != 3 {
		t.Errorf("unfiltered count = %d, want 3", got)
	}

	// counterparty filter (substring, case-insensitive on the stored name).
	tri, err := dealsStructured(store, map[string]any{"counterparty": "트라이브"})
	if err != nil {
		t.Fatalf("dealsStructured counterparty: %v", err)
	}
	if got := len(asRecords(tri)); got != 2 {
		t.Errorf("트라이브 count = %d, want 2", got)
	}

	// currency filter (exact).
	krw, err := dealsStructured(store, map[string]any{"currency": "KRW"})
	if err != nil {
		t.Fatalf("dealsStructured currency: %v", err)
	}
	krwRecs := asRecords(krw)
	if len(krwRecs) != 2 {
		t.Fatalf("KRW count = %d, want 2", len(krwRecs))
	}
	var sum float64
	for _, r := range krwRecs {
		if r.AmountParsed {
			sum += r.AmountValue
		}
	}
	if sum != 8_000_000 { // 5,000,000 + 3,000,000 (per-currency, parsed only)
		t.Errorf("KRW parsed sum = %v, want 8,000,000", sum)
	}
}
