package wiki

import (
	"strings"
	"testing"
	"time"
)

// filePriceHistory seeds the ledger through the real write path so recall
// tests exercise the same records production files.
func filePriceHistory(t *testing.T, s *Store) {
	t.Helper()
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	docs := []DealPageInput{
		{
			Counterparty: "진코솔라",
			DocType:      "발주품의",
			Amount:       "290,000,000원",
			Date:         "2026-05-14",
			SourceRef:    "approval:100",
			LineItems: []DealLineItem{
				{Name: "태양광 모듈 640W", Qty: "2,000장", UnitPrice: "145원/W", Amount: "290,000,000원", Quote: "태양광 모듈 640W 2,000장 145원/W"},
			},
		},
		{
			Counterparty: "진코솔라",
			DocType:      "견적서",
			Amount:       "296,000,000원",
			Date:         "2026-04-29",
			SourceRef:    "mail:m1",
			Terms:        &DealTerms{UnitPrice: QuotedTerm{Value: "148원/W", Quote: "단가 148원/W"}},
		},
		{
			Counterparty: "경비-주유비",
			DocType:      "지출결의",
			Amount:       "480,000원",
			Date:         "2026-06-15",
			SourceRef:    "approval:200",
			ExpenseKind:  "주유비",
		},
		{
			Counterparty: "경비-주유비",
			DocType:      "지출결의",
			Amount:       "520,000원",
			Date:         "2026-05-15",
			SourceRef:    "approval:201",
			ExpenseKind:  "주유비",
		},
		{
			Counterparty: "경비-주유비",
			DocType:      "지출결의",
			Amount:       "500,000원",
			Date:         "2026-04-15",
			SourceRef:    "approval:202",
			ExpenseKind:  "주유비",
		},
	}
	for _, d := range docs {
		if _, _, err := s.UpsertDealPage(d, now); err != nil {
			t.Fatalf("UpsertDealPage(%s): %v", d.SourceRef, err)
		}
	}
}

func TestPriceHistoryContext_MatchesItemExpenseAndCounterparty(t *testing.T) {
	s := newDealStore(t)
	filePriceHistory(t, s)

	got := s.PriceHistoryContext("640W 모듈 3차 발주 품의 — 진코솔라, 6월 주유비 정산 포함")
	if !strings.Contains(got, "145원/W") {
		t.Errorf("item unit price missing:\n%s", got)
	}
	if !strings.Contains(got, "주유비") || !strings.Contains(got, "중앙값 500,000원") {
		t.Errorf("expense series/median missing:\n%s", got)
	}
	if !strings.Contains(got, "거래처 진코솔라") || !strings.Contains(got, "148원/W") {
		t.Errorf("counterparty document terms missing:\n%s", got)
	}
}

func TestPriceHistoryContext_NoMatchReturnsEmpty(t *testing.T) {
	s := newDealStore(t)
	filePriceHistory(t, s)

	if got := s.PriceHistoryContext("연차 휴가 신청의 건"); got != "" {
		t.Errorf("expected empty context, got:\n%s", got)
	}
}

func TestPriceDeltaLines_ItemUnitPriceChange(t *testing.T) {
	s := newDealStore(t)
	filePriceHistory(t, s)

	lines := s.PriceDeltaLines(DealPageInput{
		Counterparty: "진코솔라",
		DocType:      "발주품의",
		SourceRef:    "approval:300",
		LineItems: []DealLineItem{
			{Name: "모듈 640W", UnitPrice: "152원/W"},
		},
	})
	if len(lines) != 1 {
		t.Fatalf("lines = %v, want 1", lines)
	}
	if !strings.Contains(lines[0], "145원/W → 152원/W") || !strings.Contains(lines[0], "+4.8%") {
		t.Errorf("delta line = %q", lines[0])
	}
	if !strings.Contains(lines[0], "2026-05-14") {
		t.Errorf("delta line missing prev date: %q", lines[0])
	}
}

func TestPriceDeltaLines_ExpenseMedianComparison(t *testing.T) {
	s := newDealStore(t)
	filePriceHistory(t, s)

	lines := s.PriceDeltaLines(DealPageInput{
		Counterparty: "경비-주유비",
		DocType:      "지출결의",
		Amount:       "1,000,000원",
		SourceRef:    "approval:301",
		ExpenseKind:  "주유비",
	})
	if len(lines) != 1 {
		t.Fatalf("lines = %v, want 1", lines)
	}
	if !strings.Contains(lines[0], "중앙값 500,000원") || !strings.Contains(lines[0], "+100.0%") {
		t.Errorf("expense delta = %q", lines[0])
	}
}

func TestPriceDeltaLines_SkipsOwnSourceRefAndUnitMismatch(t *testing.T) {
	s := newDealStore(t)
	filePriceHistory(t, s)

	// Same SourceRef as the filed 발주 → its own row must not be "previous";
	// no other row shares the 원/장 unit, so no delta at all.
	lines := s.PriceDeltaLines(DealPageInput{
		Counterparty: "진코솔라",
		SourceRef:    "approval:100",
		LineItems: []DealLineItem{
			{Name: "태양광 모듈 640W", UnitPrice: "150,000원/장"},
		},
	})
	if len(lines) != 0 {
		t.Errorf("lines = %v, want none (unit mismatch + own ref)", lines)
	}
}

func TestUpsertDealPage_PersistsLineItemsAndExpenseKind(t *testing.T) {
	s := newDealStore(t)
	filePriceHistory(t, s)

	recs, err := s.ListDealRecords()
	if err != nil {
		t.Fatalf("ListDealRecords: %v", err)
	}
	if len(recs) != 5 {
		t.Fatalf("records = %d, want 5", len(recs))
	}
	if len(recs[0].LineItems) != 1 || recs[0].LineItems[0].UnitPrice != "145원/W" {
		t.Errorf("line items not persisted: %+v", recs[0].LineItems)
	}
	if recs[2].ExpenseKind != "주유비" {
		t.Errorf("expense kind not persisted: %+v", recs[2])
	}
	// Prose page echoes the item compactly.
	page, err := s.ReadPage("프로젝트/거래/진코솔라.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if !strings.Contains(page.Body, "태양광 모듈 640W 145원/W") {
		t.Errorf("prose entry missing item echo:\n%s", page.Body)
	}
}
