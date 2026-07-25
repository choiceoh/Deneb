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
	// Rejections7d counts evolve_rejected events inside the health window that
	// were an actual VERDICT on a candidate. Infrastructure failures are
	// excluded and counted separately below: mixing them in reads as "the
	// gates are rejecting more" when the truth is "the judge was down", which
	// points diagnosis at candidate quality instead of at the outage.
	Rejections7d int `json:"rejections7d,omitempty"`
	// InfraRejections7d counts the excluded outages (judge call errored, teacher
	// rewrite produced nothing) so they stay VISIBLE rather than silently
	// vanishing from the funnel — a spike here is an availability signal.
	InfraRejections7d int `json:"infraRejections7d,omitempty"`
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
	// Applied7d is the subset of candidates first verdicted in-window that have
	// since reached applied, including accepted -> deploy-watch closure.
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
	// PendingCount and OldestPendingAgeMs make queue aging visible even when no
	// new verdict landed in the seven-day window.
	PendingCount       int   `json:"pendingCount,omitempty"`
	OldestPendingAgeMs int64 `json:"oldestPendingAgeMs,omitempty"`
	// Delivery outcomes prove whether accepted code candidates actually crossed
	// the automated release boundary.
	Dispatched7d  int `json:"dispatched7d,omitempty"`
	WatchPassed7d int `json:"watchPassed7d,omitempty"`
	RolledBack7d  int `json:"rolledBack7d,omitempty"`
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
	candidatesByID, firstVerdicts, firstApplied := collectSelfCorrectionFunnel(records, cutoff, &s)
	appliedBySource := selfCorrectionAppliedBySource(candidatesByID, firstApplied)
	verdictLatencySum := addSelfCorrectionClosureMetrics(&s, candidatesByID, firstVerdicts, firstApplied, appliedBySource, cutoff)
	addSelfCorrectionPendingMetrics(&s, records, now.UnixMilli())
	if s.Verdicted7d > 0 {
		s.ConversionRate = float64(s.Applied7d) / float64(s.Verdicted7d)
		s.MeanTimeToVerdictMs = verdictLatencySum / int64(s.Verdicted7d)
	}
	t.addSelfCorrectionRejectionMetrics(&s, cutoff)
	return s
}

// collectSelfCorrectionFunnel performs the chronological fold once and returns
// the three indexes every closure metric shares.
func collectSelfCorrectionFunnel(records []SelfCorrectionCandidateRecord, cutoff int64, s *SelfCorrectionFunnelSummary) (
	map[string]SelfCorrectionCandidateRecord, map[string]int64, map[string]int64,
) {
	fold := funnelFold{
		cutoff:         cutoff,
		summary:        s,
		candidatesByID: make(map[string]SelfCorrectionCandidateRecord, len(records)),
		firstVerdicts:  make(map[string]int64, len(records)),
		firstApplied:   make(map[string]int64, len(records)),
	}
	for _, rec := range records {
		switch rec.Type {
		case selfCorrectionTypeReview:
			fold.review(rec)
		case selfCorrectionTypeDispatch:
			fold.dispatch(rec)
		case "", selfCorrectionTypeCandidate:
			fold.candidate(rec)
		}
	}
	return fold.candidatesByID, fold.firstVerdicts, fold.firstApplied
}

// funnelFold carries the shared indexes of the single chronological fold so
// each record-type transition stays a small, separately readable step.
type funnelFold struct {
	cutoff         int64
	summary        *SelfCorrectionFunnelSummary
	candidatesByID map[string]SelfCorrectionCandidateRecord
	firstVerdicts  map[string]int64
	firstApplied   map[string]int64
}

func (f *funnelFold) review(rec SelfCorrectionCandidateRecord) {
	if rec.CreatedAt > f.summary.LastReviewAt {
		f.summary.LastReviewAt = rec.CreatedAt
	}
	rec.ID = strings.TrimSpace(rec.ID)
	if rec.ID == "" {
		return
	}
	// Earliest review row = first verdict (later rows are re-verdicts).
	setEarlier(f.firstVerdicts, rec.ID, rec.CreatedAt)
	if normalizeSelfCorrectionStatus(rec.Status) == SelfCorrectionStatusApplied {
		setEarlier(f.firstApplied, rec.ID, rec.CreatedAt)
	}
}

func (f *funnelFold) dispatch(rec SelfCorrectionCandidateRecord) {
	rec.ID = strings.TrimSpace(rec.ID)
	if rec.ID == "" {
		return
	}
	switch normalizeSelfCorrectionDispatchPhase(rec.DispatchPhase) {
	case selfCorrectionDispatchStarted:
		if rec.CreatedAt >= f.cutoff {
			f.summary.Dispatched7d++
		}
	case selfCorrectionDispatchWatchPassed:
		if rec.CreatedAt >= f.cutoff {
			f.summary.WatchPassed7d++
		}
		if rec.CreatedAt > f.summary.LastReviewAt {
			f.summary.LastReviewAt = rec.CreatedAt
		}
		setEarlier(f.firstVerdicts, rec.ID, rec.CreatedAt)
		setEarlier(f.firstApplied, rec.ID, rec.CreatedAt)
	case selfCorrectionDispatchRolledBack:
		if rec.CreatedAt >= f.cutoff {
			f.summary.RolledBack7d++
		}
	}
}

func (f *funnelFold) candidate(rec SelfCorrectionCandidateRecord) {
	rec.ID = strings.TrimSpace(rec.ID)
	if rec.ID == "" {
		return
	}
	if rec.CreatedAt > f.summary.LastCaptureAt {
		f.summary.LastCaptureAt = rec.CreatedAt
	}
	// Keep the earliest candidate row per ID (the original proposal).
	if existing, ok := f.candidatesByID[rec.ID]; !ok || rec.CreatedAt < existing.CreatedAt {
		f.candidatesByID[rec.ID] = rec
	}
}

func setEarlier(index map[string]int64, id string, createdAt int64) {
	if prior, exists := index[id]; !exists || createdAt < prior {
		index[id] = createdAt
	}
}

func selfCorrectionAppliedBySource(candidates map[string]SelfCorrectionCandidateRecord, firstApplied map[string]int64) map[string]int64 {
	// Source → earliest APPLIED transition createdAt, for reopen detection.
	// A candidate is a reopen when its exact source string had an earlier
	// applied fix — the "fix did not stick" signal. This is an exact-match
	// lookup (not prefix), matching how candidates store their Source field.
	// Applied can arrive as a direct review or as a deploy-watch closure event.
	appliedBySource := make(map[string]int64)
	for id, cand := range candidates {
		appliedAt, ok := firstApplied[id]
		if !ok {
			continue
		}
		src := strings.TrimSpace(cand.Source)
		if src == "" {
			continue
		}
		if prior, exists := appliedBySource[src]; !exists || appliedAt < prior {
			appliedBySource[src] = appliedAt
		}
	}
	return appliedBySource
}

func addSelfCorrectionClosureMetrics(
	s *SelfCorrectionFunnelSummary,
	candidates map[string]SelfCorrectionCandidateRecord,
	firstVerdicts, firstApplied, appliedBySource map[string]int64,
	cutoff int64,
) int64 {
	// Closure metrics over the window.
	var verdictLatencySum int64
	for id, cand := range candidates {
		if cand.CreatedAt >= cutoff {
			s.Proposed7d++
			// Reopen: a candidate captured in-window whose source already had
			// an applied fix before this candidate appeared.
			if appliedAt, ok := appliedBySource[strings.TrimSpace(cand.Source)]; ok && appliedAt < cand.CreatedAt {
				s.Reopens7d++
			}
		}
		verdictAt, ok := firstVerdicts[id]
		if !ok || verdictAt < cutoff {
			continue
		}
		s.Verdicted7d++
		if _, applied := firstApplied[id]; applied {
			s.Applied7d++
		}
		latency := verdictAt - cand.CreatedAt
		if latency > 0 {
			verdictLatencySum += latency
		}
	}
	return verdictLatencySum
}

func addSelfCorrectionPendingMetrics(s *SelfCorrectionFunnelSummary, records []SelfCorrectionCandidateRecord, nowMs int64) {
	// Current queue age is computed from the same folded state clients see.
	for _, cand := range mergeSelfCorrectionRecords(records) {
		if cand.Status != SelfCorrectionStatusProposed && cand.Status != SelfCorrectionStatusAccepted {
			continue
		}
		if normalizeSelfCorrectionDispatchPhase(cand.DispatchPhase) == selfCorrectionDispatchDeclined {
			continue
		}
		s.PendingCount++
		age := nowMs - cand.CreatedAt
		if age > s.OldestPendingAgeMs {
			s.OldestPendingAgeMs = age
		}
	}
}

func (t *Tracker) addSelfCorrectionRejectionMetrics(s *SelfCorrectionFunnelSummary, cutoff int64) {
	// Rejection scan is independent of the queue fold: it reads the upstream
	// evolve lifecycle and explains whether capture inputs still arrive.
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return
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
		if isInfrastructureRejection(entry.Reason) {
			s.InfraRejections7d++
			continue
		}
		s.Rejections7d++
		if isSelfHarnessOrReplayRejection(entry.Reason) {
			s.PromotableRejections7d++
		}
	}
}
