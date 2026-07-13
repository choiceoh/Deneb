package genesis

import (
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// SelfCorrectionFunnelSummary explains BOTH sides of the deferred
// self-correction queue. An empty queue is ambiguous on its own — it can mean
// "everything was consumed" or "the capture funnel broke / nothing qualified".
// The capture-side counters (lastCaptureAt, rejections7d, ...) let the native
// 자가코딩 개선 screen distinguish the two (2026-07-08: the queue went quiet for
// four days after its first two candidates and the screen could not say why).
//
// The closure-side counters (proposed7d, verdicted7d, applied7d,
// conversionRate, meanTimeToVerdictMs, reopens7d) answer a different question:
// "is the loop actually closing?" A queue that fills but never gets verdicted,
// or one whose fixes never stick (high reopens), is a stuck loop — and without
// these metrics the stuck state is invisible until an operator notices the
// backlog by hand.
type SelfCorrectionFunnelSummary struct {
	// ── Capture side ──
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

	// ── Closure side (loop-closing health) ──
	// Proposed7d counts candidates that entered the queue inside the window.
	// A rising capture rate with flat verdicts = the review lane is the
	// bottleneck.
	Proposed7d int `json:"proposed7d,omitempty"`
	// Verdicted7d counts candidates that received their FIRST verdict inside
	// the window (one per candidate ID, so re-verdicting the same row does not
	// inflate the count). Verdicted = the review lane is alive.
	Verdicted7d int `json:"verdicted7d,omitempty"`
	// Applied7d is the subset of verdicted candidates that landed as applied
	// (the fix was committed). Applied = the loop actually closed.
	Applied7d int `json:"applied7d,omitempty"`
	// ConversionRate is Applied7d / Verdicted7d (0 when no verdicts). This is
	// review-lane quality, not capture quality: a low rate means the queue is
	// full of noise the operator keeps rejecting.
	ConversionRate float64 `json:"conversionRate,omitempty"`
	// MeanTimeToVerdictMs is the average (first-verdict.CreatedAt −
	// candidate.CreatedAt) for candidates verdicted inside the window. A
	// growing mean = the queue is aging before anyone looks at it.
	MeanTimeToVerdictMs int64 `json:"meanTimeToVerdictMs,omitempty"`
	// Reopens7d counts candidates captured inside the window whose source
	// signature had a PRIOR applied candidate — the "fix did not stick"
	// signal. High reopens = fixes are superficial, not root cause.
	Reopens7d int `json:"reopens7d,omitempty"`
}

// SelfCorrectionFunnel summarizes capture + closure activity from the persisted
// JSONL sidecars so the signal survives process restarts (same sourcing rule
// as SelfHarnessSignals).
func (t *Tracker) SelfCorrectionFunnel() SelfCorrectionFunnelSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.computeSelfCorrectionFunnelLocked(time.Now())
}

func (t *Tracker) computeSelfCorrectionFunnelLocked(now time.Time) SelfCorrectionFunnelSummary {
	var s SelfCorrectionFunnelSummary
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()

	records, err := jsonlstore.Load[SelfCorrectionCandidateRecord](t.selfCorrectionPath)
	if err != nil {
		records = nil // fall through to the rejection scan; a missing file is not fatal
	}

	// First pass: collect candidates and first-verdict per ID.
	type firstVerdict struct {
		status    string
		createdAt int64
	}
	candidatesByID := make(map[string]SelfCorrectionCandidateRecord, len(records))
	firstVerdicts := make(map[string]firstVerdict, len(records))
	for _, rec := range records {
		if rec.Type == SelfCorrectionTypeReview {
			if rec.CreatedAt > s.LastReviewAt {
				s.LastReviewAt = rec.CreatedAt
			}
			rec.ID = strings.TrimSpace(rec.ID)
			if rec.ID == "" {
				continue
			}
			// Earliest review row = first verdict (later rows are re-verdicts).
			prior, exists := firstVerdicts[rec.ID]
			status := normalizeSelfCorrectionStatus(rec.Status)
			if !exists || rec.CreatedAt < prior.createdAt {
				firstVerdicts[rec.ID] = firstVerdict{status: status, createdAt: rec.CreatedAt}
			}
			continue
		}
		rec.ID = strings.TrimSpace(rec.ID)
		if rec.ID == "" {
			continue
		}
		if rec.CreatedAt > s.LastCaptureAt {
			s.LastCaptureAt = rec.CreatedAt
		}
		// Keep the earliest candidate row per ID (the original proposal).
		if existing, ok := candidatesByID[rec.ID]; !ok || rec.CreatedAt < existing.CreatedAt {
			candidatesByID[rec.ID] = rec
		}
	}

	// Source-prefix → earliest APPLIED createdAt, for reopen detection.
	// A candidate is a reopen when its source signature had an earlier applied
	// candidate — the fix was attempted once and the signature came back.
	// Source-prefix → earliest APPLIED verdict createdAt, for reopen detection.
	// A candidate is a reopen when its source signature had an earlier applied
	// fix — the "fix did not stick" signal. Status lives on review rows (merged
	// at read time), so disposition comes from firstVerdicts, not the candidate
	// row's initial status.
	appliedBySource := make(map[string]int64)
	for id, cand := range candidatesByID {
		verdict, ok := firstVerdicts[id]
		if !ok || verdict.status != SelfCorrectionStatusApplied {
			continue
		}
		src := strings.TrimSpace(cand.Source)
		if src == "" {
			continue
		}
		if prior, exists := appliedBySource[src]; !exists || verdict.createdAt < prior {
			appliedBySource[src] = verdict.createdAt
		}
	}

	// Closure metrics over the window.
	var verdictLatencySum int64
	for id, cand := range candidatesByID {
		if cand.CreatedAt >= cutoff {
			s.Proposed7d++
			// Reopen: a candidate captured in-window whose source already had
			// an applied fix before this candidate appeared.
			if appliedAt, ok := appliedBySource[strings.TrimSpace(cand.Source)]; ok && appliedAt < cand.CreatedAt {
				s.Reopens7d++
			}
		}
		verdict, ok := firstVerdicts[id]
		if !ok || verdict.createdAt < cutoff {
			continue
		}
		s.Verdicted7d++
		if verdict.status == SelfCorrectionStatusApplied {
			s.Applied7d++
		}
		latency := verdict.createdAt - cand.CreatedAt
		if latency > 0 {
			verdictLatencySum += latency
		}
	}
	if s.Verdicted7d > 0 {
		s.ConversionRate = float64(s.Applied7d) / float64(s.Verdicted7d)
		s.MeanTimeToVerdictMs = verdictLatencySum / int64(s.Verdicted7d)
	}

	// Rejection scan (unchanged from original).
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return s
	}
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
