package server

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestDealDeadlines(t *testing.T) {
	loc := time.UTC
	recs := []wiki.DealRecord{
		{Counterparty: "트라이브", DocType: "세금계산서", AmountRaw: "5,000,000원", DueDate: "2026-06-20"},
		{Counterparty: "트라이브", DocType: "세금계산서", AmountRaw: "5,000,000원", DueDate: "2026-06-20"}, // exact dup
		{Counterparty: "현대", DueDate: ""},   // no due → skip
		{Counterparty: "삼성", DueDate: "협의"}, // unparseable → skip
		{Counterparty: "LG", AmountRaw: "$1,200", DueDate: "2026-07-01"},
	}

	out := dealDeadlines(recs, loc)
	if len(out) != 2 {
		t.Fatalf("want 2 (dedup + 2 skips), got %d: %+v", len(out), out)
	}

	// Due time is end-of-day so a same-day deadline isn't already past at midnight.
	wantDue := time.Date(2026, 6, 20, 23, 59, 59, 0, loc)
	if !out[0].Due.Equal(wantDue) {
		t.Errorf("Due = %v, want end-of-day %v", out[0].Due, wantDue)
	}
	if !strings.Contains(out[0].Label, "트라이브") || !strings.Contains(out[0].Label, "5,000,000원") || !strings.HasSuffix(out[0].Label, "기한") {
		t.Errorf("label = %q", out[0].Label)
	}
	if !strings.Contains(out[1].Label, "LG") {
		t.Errorf("label[1] = %q, want LG", out[1].Label)
	}
}

func TestNewDealDeadlineSignalCollector_NilSafe(t *testing.T) {
	// Nil accessor → nil collector (so combineSignalCollectors skips it).
	if c := newDealDeadlineSignalCollector(nil); c != nil {
		t.Error("nil store accessor should yield a nil collector")
	}
	// Accessor returning nil store → empty snapshot, no panic.
	c := newDealDeadlineSignalCollector(func() *wiki.Store { return nil })
	if c == nil {
		t.Fatal("non-nil accessor should yield a collector")
	}
	in := c(nil)
	if len(in.Deadlines) != 0 {
		t.Errorf("nil store should yield no deadlines, got %d", len(in.Deadlines))
	}
}
