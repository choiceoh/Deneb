package genesis

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

func TestSelfCorrectionFunnel_DistinguishesConsumedFromBroken(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	dayMs := int64(24 * time.Hour / time.Millisecond)

	// Capture two candidates four days ago, review one of them a day later.
	captureAt := now.UnixMilli() - 4*dayMs
	reviewAt := captureAt + dayMs
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-a", Title: "a", CreatedAt: captureAt - 1000,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-b", Title: "b", CreatedAt: captureAt,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeReview, ID: "sc-b", Status: SelfCorrectionStatusApplied, CreatedAt: reviewAt,
	})

	// Rejections: one promotable + one patch-first inside the window, one
	// ancient promotable outside it, plus a non-rejection entry as noise.
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "old", CreatedAt: now.UnixMilli() - 30*dayMs,
		Reason: "held-out selection rejected: stale",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "contract-review", CreatedAt: now.UnixMilli() - 2*dayMs,
		Reason: "held-out selection rejected: candidate did not improve validation score",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "system-health-check", CreatedAt: now.UnixMilli() - dayMs,
		Reason: "Hermes patch-first gate rejected broad rewrite: changed 5 sections, max 3",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolved", SkillName: "system-health-check", CreatedAt: now.UnixMilli(),
	})

	tr.mu.Lock()
	s := tr.computeSelfCorrectionFunnelLocked(now)
	tr.mu.Unlock()

	if s.LastCaptureAt != captureAt {
		t.Fatalf("LastCaptureAt = %d, want %d", s.LastCaptureAt, captureAt)
	}
	if s.LastReviewAt != reviewAt {
		t.Fatalf("LastReviewAt = %d, want %d", s.LastReviewAt, reviewAt)
	}
	if s.Rejections7d != 2 {
		t.Fatalf("Rejections7d = %d, want 2", s.Rejections7d)
	}
	if s.PromotableRejections7d != 1 {
		t.Fatalf("PromotableRejections7d = %d, want 1 (patch-first class must not count)", s.PromotableRejections7d)
	}
	if s.LastRejectionAt != now.UnixMilli()-dayMs {
		t.Fatalf("LastRejectionAt = %d, want %d", s.LastRejectionAt, now.UnixMilli()-dayMs)
	}
}

func TestSelfCorrectionFunnel_EmptyStateIsAllZero(t *testing.T) {
	tr := newTestTracker(t)
	s := tr.SelfCorrectionFunnel()
	if s != (SelfCorrectionFunnelSummary{}) {
		t.Fatalf("empty tracker funnel = %+v, want zero", s)
	}
}

func TestSelfCorrectionFunnel_ClosureMetrics(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	dayMs := int64(24 * time.Hour / time.Millisecond)

	// Two candidates in-window: sc-a gets applied after 2 days, sc-b gets
	// rejected after 1 day. Conversion = 1 applied / 2 verdicted = 0.5.
	captureA := now.UnixMilli() - 5*dayMs
	captureB := now.UnixMilli() - 3*dayMs
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-a", Source: "failure-cluster:aaa", CreatedAt: captureA,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-b", Source: "failure-cluster:bbb", CreatedAt: captureB,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeReview, ID: "sc-a", Status: SelfCorrectionStatusApplied, CreatedAt: captureA + 2*dayMs,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeReview, ID: "sc-b", Status: SelfCorrectionStatusRejected, CreatedAt: captureB + dayMs,
	})

	// A third candidate in-window that re-opens an OLD applied signature —
	// the "fix did not stick" signal. The prior applied candidate is outside
	// the window (oldApplied) but its source matches the new candidate.
	oldApplied := now.UnixMilli() - 20*dayMs
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-old", Source: "failure-cluster:reopen-sig", CreatedAt: oldApplied,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeReview, ID: "sc-old", Status: SelfCorrectionStatusApplied, CreatedAt: oldApplied + dayMs,
	})
	reopenCapture := now.UnixMilli() - dayMs
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-reopen", Source: "failure-cluster:reopen-sig", CreatedAt: reopenCapture,
	})

	tr.mu.Lock()
	s := tr.computeSelfCorrectionFunnelLocked(now)
	tr.mu.Unlock()

	if s.Proposed7d != 3 {
		t.Fatalf("Proposed7d = %d, want 3 (sc-a, sc-b, sc-reopen in window; sc-old outside)", s.Proposed7d)
	}
	if s.Verdicted7d != 2 {
		t.Fatalf("Verdicted7d = %d, want 2 (sc-a + sc-b verdicted in window; sc-old outside)", s.Verdicted7d)
	}
	if s.Applied7d != 1 {
		t.Fatalf("Applied7d = %d, want 1 (only sc-a)", s.Applied7d)
	}
	if s.ConversionRate != 0.5 {
		t.Fatalf("ConversionRate = %f, want 0.5 (1 applied / 2 verdicted)", s.ConversionRate)
	}
	if s.Reopens7d != 1 {
		t.Fatalf("Reopens7d = %d, want 1 (sc-reopen re-opens reopen-sig)", s.Reopens7d)
	}
	// MeanTimeToVerdict: sc-a took 2 days, sc-b took 1 day → avg 1.5 days.
	wantMean := (2*dayMs + dayMs) / 2
	if s.MeanTimeToVerdictMs != wantMean {
		t.Fatalf("MeanTimeToVerdictMs = %d, want %d", s.MeanTimeToVerdictMs, wantMean)
	}
}

func TestSelfCorrectionFunnel_NoVerdictsMeansZeroClosure(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	dayMs := int64(24 * time.Hour / time.Millisecond)

	// Candidate captured in-window but never verdicted — a stuck queue.
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-stuck", Source: "failure-cluster:stuck", CreatedAt: now.UnixMilli() - dayMs,
	})

	tr.mu.Lock()
	s := tr.computeSelfCorrectionFunnelLocked(now)
	tr.mu.Unlock()

	if s.Proposed7d != 1 {
		t.Fatalf("Proposed7d = %d, want 1", s.Proposed7d)
	}
	if s.Verdicted7d != 0 {
		t.Fatalf("Verdicted7d = %d, want 0 (never reviewed)", s.Verdicted7d)
	}
	if s.Applied7d != 0 || s.ConversionRate != 0 || s.MeanTimeToVerdictMs != 0 {
		t.Fatalf("closure side = %+v, want all zero for un-verdicted candidate", s)
	}
	if s.PendingCount != 1 || s.OldestPendingAgeMs != dayMs {
		t.Fatalf("pending age = %+v, want one candidate aged one day", s)
	}
}

func TestSelfCorrectionFunnel_AcceptedThenWatchPassedCountsApplied(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	dayMs := int64(24 * time.Hour / time.Millisecond)
	captured := now.UnixMilli() - 4*dayMs
	accepted := captured + dayMs
	attempt := "attempt-1"

	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeCandidate, ID: "sc-close", Source: "self-harness:close", CreatedAt: captured,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: selfCorrectionTypeReview, ID: "sc-close", Status: SelfCorrectionStatusAccepted, CreatedAt: accepted,
	})
	for i, phase := range []string{
		selfCorrectionDispatchStarted,
		SelfCorrectionDispatchMerged,
		selfCorrectionDispatchDeployed,
		selfCorrectionDispatchWatchPassed,
	} {
		appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
			Type: selfCorrectionTypeDispatch, ID: "sc-close", DispatchPhase: phase,
			AttemptID: attempt, CreatedAt: accepted + int64(i+1)*1000,
		})
	}

	tr.mu.Lock()
	s := tr.computeSelfCorrectionFunnelLocked(now)
	tr.mu.Unlock()

	if s.Verdicted7d != 1 || s.Applied7d != 1 || s.ConversionRate != 1 {
		t.Fatalf("accepted -> watch_passed must close the funnel: %+v", s)
	}
	if s.Dispatched7d != 1 || s.WatchPassed7d != 1 || s.RolledBack7d != 0 {
		t.Fatalf("dispatch outcomes not counted: %+v", s)
	}
	if s.PendingCount != 0 {
		t.Fatalf("watch-passed candidate still pending: %+v", s)
	}
}

func appendFunnel[T any](t *testing.T, path string, record T) {
	t.Helper()
	if err := jsonlstore.Append(path, record); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}
