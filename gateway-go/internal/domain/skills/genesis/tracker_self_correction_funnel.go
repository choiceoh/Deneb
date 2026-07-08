package genesis

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// SelfCorrectionFunnelSummary explains the CAPTURE side of the deferred
// self-correction queue. An empty queue is ambiguous on its own — it can mean
// "everything was consumed" or "the capture funnel broke / nothing qualified".
// These counters let the native 자가코딩 개선 screen distinguish the two
// (2026-07-08: the queue went quiet for four days after its first two
// candidates and the screen could not say why).
type SelfCorrectionFunnelSummary struct {
	// LastCaptureAt is when a candidate last entered the queue (0 = never).
	LastCaptureAt int64 `json:"lastCaptureAt,omitempty"`
	// LastReviewAt is when a review verdict was last recorded (0 = never).
	LastReviewAt int64 `json:"lastReviewAt,omitempty"`
	// Rejections7d counts evolve_rejected events inside the health window.
	Rejections7d int `json:"rejections7d,omitempty"`
	// PromotableRejections7d is the subset whose reason class qualifies for
	// automatic promotion into a validation-draft candidate.
	PromotableRejections7d int `json:"promotableRejections7d,omitempty"`
	// LastRejectionAt is the newest evolve_rejected timestamp across the whole
	// log (not windowed) — proves the upstream evolve loop is alive even when
	// the 7d counters are zero.
	LastRejectionAt int64 `json:"lastRejectionAt,omitempty"`
}

// SelfCorrectionFunnel summarizes capture-side activity from the persisted
// JSONL sidecars so the signal survives process restarts (same sourcing rule
// as SelfHarnessSignals).
func (t *Tracker) SelfCorrectionFunnel() SelfCorrectionFunnelSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.computeSelfCorrectionFunnelLocked(time.Now())
}

func (t *Tracker) computeSelfCorrectionFunnelLocked(now time.Time) SelfCorrectionFunnelSummary {
	var s SelfCorrectionFunnelSummary
	if records, err := jsonlstore.Load[SelfCorrectionCandidateRecord](t.selfCorrectionPath); err == nil {
		for _, rec := range records {
			if rec.Type == SelfCorrectionTypeReview {
				if rec.CreatedAt > s.LastReviewAt {
					s.LastReviewAt = rec.CreatedAt
				}
				continue
			}
			if rec.CreatedAt > s.LastCaptureAt {
				s.LastCaptureAt = rec.CreatedAt
			}
		}
	}
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return s
	}
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
	for _, entry := range entries {
		if entry.Type != "evolve_rejected" {
			continue
		}
		if entry.CreatedAt > s.LastRejectionAt {
			s.LastRejectionAt = entry.CreatedAt
		}
		if entry.CreatedAt < cutoff {
			continue
		}
		s.Rejections7d++
		if isSelfHarnessOrReplayRejection(entry.Reason) {
			s.PromotableRejections7d++
		}
	}
	return s
}
