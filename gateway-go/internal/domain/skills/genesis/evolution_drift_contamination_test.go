package genesis

import (
	"testing"
	"time"
)

// 2026-08-16: a genesis package test run wrote 27 fixtures (createdAt≈1000,
// artifact "evolve.md", action auto_adopted) into the production meta ledger.
// On 08-18 the drift brake read the newest 20 rows, saw twenty adoptions of one
// artifact, and froze auto-adoption — the third false-positive freeze. The
// plausibility band at the single ledger reader is the contract these tests
// pin: contaminated rows never reach a consumer, and the exclusion is visible.

func TestDriftBrakeIgnoresContaminatedMetaRows(t *testing.T) {
	tr := driftTracker(t)
	for i := 0; i < 20; i++ {
		if err := tr.LogMetaRevision(MetaRevisionRecord{
			CreatedAt: 1000 + int64(i%3), Epoch: metaEpochProducer, Artifact: "evolve.md",
			Proposed: true, Action: "auto_adopted", RevisionClass: MetaRevisionClassParametric,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A healthy rotation on top: one proposal, two lanes that looked and skipped.
	for _, r := range []MetaRevisionRecord{
		{Epoch: metaEpochProducer, Artifact: "evolve-system-prompt.md", Proposed: true},
		{Epoch: metaEpochEvaluator, Artifact: "skill-judge-system-prompt.md"},
		{Epoch: metaEpochGenesis, Artifact: "genesis-system-prompt.md"},
	} {
		if err := tr.LogMetaRevision(r); err != nil {
			t.Fatal(err)
		}
	}

	v := tr.auditEvolutionDrift()
	if v.Frozen || hasSignal(v, "adoption_monotony") {
		t.Fatalf("contaminated rows drove the brake: %+v", v.Signals)
	}
	if got := tr.MetaLedgerImplausibleRows(); got != 20 {
		t.Fatalf("implausible rows = %d, want 20", got)
	}
	revs, err := tr.RecentMetaRevisions(50)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 {
		t.Fatalf("plausible rows = %d, want 3", len(revs))
	}
	for _, r := range revs {
		if r.CreatedAt < metaLedgerFloorMs {
			t.Fatalf("implausible row leaked through RecentMetaRevisions: %+v", r)
		}
	}
}

func TestRecentMetaRevisionsExcludesFutureStampedRowsAndKeepsLimit(t *testing.T) {
	tr := driftTracker(t)
	future := time.Now().Add(48 * time.Hour).UnixMilli()
	if err := tr.LogMetaRevision(MetaRevisionRecord{CreatedAt: future, Artifact: "evolve-system-prompt.md", Action: "auto_adopted"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: "evolve-system-prompt.md", Proposed: true}); err != nil {
			t.Fatal(err)
		}
	}
	revs, err := tr.RecentMetaRevisions(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 {
		t.Fatalf("limit not honoured after filtering: got %d rows", len(revs))
	}
	for _, r := range revs {
		if r.CreatedAt == future {
			t.Fatalf("future-stamped row leaked: %+v", r)
		}
	}
	if got := tr.MetaLedgerImplausibleRows(); got != 1 {
		t.Fatalf("implausible rows = %d, want 1", got)
	}
}

func TestCleanMetaLedgerReportsZeroImplausibleRows(t *testing.T) {
	tr := driftTracker(t)
	if err := tr.LogMetaRevision(MetaRevisionRecord{Epoch: metaEpochProducer, Artifact: "evolve-system-prompt.md", Proposed: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.RecentMetaRevisions(10); err != nil {
		t.Fatal(err)
	}
	if got := tr.MetaLedgerImplausibleRows(); got != 0 {
		t.Fatalf("clean ledger reported %d implausible rows", got)
	}
}
