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

// The floor must track the MEASURED cost distribution, not a guess: production
// firings run p50 479ms / p90 613ms, so a budget that clears the old 600ms
// floor is still routinely too small.
func TestBackfillWithExpansion_FloorCoversMeasuredP90(t *testing.T) {
	t.Setenv("DENEB_WIKI_QUERY_EXPANSION", "backfill")
	s, _ := newVerifyStore(t)
	called := 0
	s.SetQueryExpander(func(context.Context, string) []string {
		called++
		return nil
	})
	primary := []SearchResult{{Path: "업무/케이블.md", Score: 1.2}}

	// 700ms cleared the old floor and did not cover a p90 call.
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if got := s.backfillWithExpansion(ctx, "케이블", primary, 8); len(got) != len(primary) {
		t.Errorf("primary results not preserved: %+v", got)
	}
	if called != 0 {
		t.Errorf("expander fired with %v left — below the measured p90 + reserve", 700*time.Millisecond)
	}
}

// A slow expander must be CANCELLED by the remaining budget, leaving room for
// the caller to finish. Before this, a 1250ms call inside a 1.5s preflight
// returned just as the deadline expired and the whole wiki source was scored
// incomplete (wiki=0(deadline)) despite ready primary results.
func TestBackfillWithExpansion_CapsSlowExpanderAndKeepsPrimary(t *testing.T) {
	t.Setenv("DENEB_WIKI_QUERY_EXPANSION", "backfill")
	s, _ := newVerifyStore(t)

	var sawDeadline time.Duration
	s.SetQueryExpander(func(ctx context.Context, _ string) []string {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("expander must run under a bounded deadline")
			return nil
		}
		sawDeadline = time.Until(deadline)
		<-ctx.Done() // a slow model: run until cancelled
		return nil
	})
	primary := []SearchResult{{Path: "업무/케이블.md", Score: 1.2}}

	budget := 1200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	start := time.Now()
	got := s.backfillWithExpansion(ctx, "케이블", primary, 8)
	elapsed := time.Since(start)

	if len(got) != len(primary) {
		t.Errorf("primary results lost to a slow expansion: %+v", got)
	}
	if sawDeadline >= budget {
		t.Errorf("expander deadline %v not reduced below the caller budget %v", sawDeadline, budget)
	}
	if remaining := budget - elapsed; remaining < expansionCompletionReserve/2 {
		t.Errorf("expansion consumed the completion window: %v elapsed of %v", elapsed, budget)
	}
}
