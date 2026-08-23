package wiki

import (
	"context"
	"testing"
	"time"
)

// TestBackfillWithExpansion_SkipsWhenContextBudgetIsNearlyGone: the expander is
// an LLM call inside the search path, and the recall preflight runs that path
// under a ~1.5s budget. Spending the last few hundred ms here does not just
// lose the backfill — overrunning discards the primary results the caller
// already has (15 of 87 preflights in 12 days logged wiki=0(deadline)).
func TestBackfillWithExpansion_SkipsWhenContextBudgetIsNearlyGone(t *testing.T) {
	t.Setenv("DENEB_WIKI_QUERY_EXPANSION", "backfill")
	s, _ := newVerifyStore(t)
	writePageT(t, s, "업무/케이블.md", "케이블 발주", "업무", "TFR-CV 150SQ 발주 이력")

	called := 0
	s.SetQueryExpander(func(context.Context, string) []string {
		called++
		return []string{"전선 발주"}
	})
	primary := []SearchResult{{Path: "업무/케이블.md", Score: 1.2}}

	tight, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	got := s.backfillWithExpansion(tight, "케이블", primary, 8)
	if called != 0 {
		t.Errorf("expander called with %v of budget left", 50*time.Millisecond)
	}
	if len(got) != len(primary) {
		t.Errorf("primary results not preserved: %+v", got)
	}

	roomy, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if _ = s.backfillWithExpansion(roomy, "케이블", primary, 8); called != 1 {
		t.Errorf("expander calls with a full budget = %d, want 1", called)
	}
}
