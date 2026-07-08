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
		Type: SelfCorrectionTypeCandidate, ID: "sc-a", Title: "a", CreatedAt: captureAt - 1000,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: SelfCorrectionTypeCandidate, ID: "sc-b", Title: "b", CreatedAt: captureAt,
	})
	appendFunnel(t, tr.selfCorrectionPath, SelfCorrectionCandidateRecord{
		Type: SelfCorrectionTypeReview, ID: "sc-b", Status: SelfCorrectionStatusApplied, CreatedAt: reviewAt,
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

func appendFunnel[T any](t *testing.T, path string, record T) {
	t.Helper()
	if err := jsonlstore.Append(path, record); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}
