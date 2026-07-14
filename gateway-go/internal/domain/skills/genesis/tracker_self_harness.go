package genesis

import (
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// SelfHarnessSignalSummary exposes the Propus/Self-Harness quality loop as
// health metrics: which gate rejected candidates, whether weak candidates are
// being converted into reviewable validation drafts, and whether an accepted
// evolve's target signature recurred in real use.
type SelfHarnessSignalSummary struct {
	Rejections7d                  int    `json:"rejections7d"`
	MissingAuditRejections7d      int    `json:"missingAuditRejections7d"`
	SignatureMismatchRejections7d int    `json:"signatureMismatchRejections7d"`
	SurfaceMismatchRejections7d   int    `json:"surfaceMismatchRejections7d"`
	HeldOutReplayRejections7d     int    `json:"heldOutReplayRejections7d"`
	ValidationDrafts7d            int    `json:"validationDrafts7d"`
	TargetRecurrences7d           int    `json:"targetRecurrences7d"`
	TopRecurringTargetSkill       string `json:"topRecurringTargetSkill,omitempty"`
	TopRecurringTargetSignature   string `json:"topRecurringTargetSignature,omitempty"`
	TopRecurringTargetRecurrences int    `json:"topRecurringTargetRecurrences,omitempty"`
}

// SelfHarnessSignals summarizes recent Self-Harness behavior from persisted
// JSONL sidecars so the signal survives process restarts.
func (t *Tracker) SelfHarnessSignals() SelfHarnessSignalSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.computeSelfHarnessSignalsLocked(time.Now())
}

func (t *Tracker) computeSelfHarnessSignalsLocked(now time.Time) SelfHarnessSignalSummary {
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return SelfHarnessSignalSummary{}
	}
	var s SelfHarnessSignalSummary
	for _, entry := range entries {
		if entry.CreatedAt < cutoff || entry.Type != "evolve_rejected" {
			continue
		}
		reason := strings.ToLower(strings.TrimSpace(entry.Reason))
		if isSelfHarnessOrReplayRejection(reason) {
			s.Rejections7d++
		}
		// Non-exclusive counters over the SHARED classifier substrings
		// (tracker_failure_clusters.go) — a reason can bump several at once, but
		// the substrings themselves are defined once.
		if evolveRejectionMatchesClass(reason, "missing-audit") {
			s.MissingAuditRejections7d++
		}
		if evolveRejectionMatchesClass(reason, "signature-mismatch") {
			s.SignatureMismatchRejections7d++
		}
		if evolveRejectionMatchesClass(reason, "surface-mismatch") {
			s.SurfaceMismatchRejections7d++
		}
		if evolveRejectionMatchesClass(reason, "heldout-replay") {
			s.HeldOutReplayRejections7d++
		}
	}
	s.ValidationDrafts7d = t.countRejectedEvolveValidationDraftsLocked(cutoff)
	t.addTargetRecurrenceSignalsLocked(&s, entries, cutoff)
	return s
}

func (t *Tracker) countRejectedEvolveValidationDraftsLocked(cutoff int64) int {
	records, err := jsonlstore.Load[SelfCorrectionCandidateRecord](t.selfCorrectionPath)
	if err != nil {
		return 0
	}
	count := 0
	for _, rec := range records {
		if rec.Type == selfCorrectionTypeReview || rec.CreatedAt < cutoff {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rec.Source), "self-harness-rejected-evolve") {
			count++
		}
	}
	return count
}

type latestSelfHarnessTarget struct {
	skill     string
	signature string
	at        int64
}

func (t *Tracker) addTargetRecurrenceSignalsLocked(s *SelfHarnessSignalSummary, entries []LifecycleLogEntry, cutoff int64) {
	recs := t.targetRecurrencesLocked(entries, cutoff)
	for _, rec := range recs {
		s.TargetRecurrences7d += rec.recurrences
	}
	if len(recs) > 0 {
		s.TopRecurringTargetSkill = recs[0].skill
		s.TopRecurringTargetSignature = recs[0].signature
		s.TopRecurringTargetRecurrences = recs[0].recurrences
	}
}

// targetRecurrence is one "the accepted evolve did not stick" observation:
// real usage failures whose trace signature matches the latest evolved target
// signature for that skill, after the evolve landed.
type targetRecurrence struct {
	skill       string
	signature   string
	recurrences int
	lastAt      int64
}

// targetRecurrencesLocked reports per-(skill, latest target signature)
// recurrence counts within the window, most-recurring first (ties by skill
// name for determinism). Caller holds t.mu. Shared by the health summary
// above and the promotion lane (tracker_recurrence_promotion.go).
func (t *Tracker) targetRecurrencesLocked(entries []LifecycleLogEntry, cutoff int64) []targetRecurrence {
	latestTargets := map[string]latestSelfHarnessTarget{}
	for _, entry := range entries {
		if entry.CreatedAt < cutoff || entry.Type != "evolved" || strings.TrimSpace(entry.SkillName) == "" {
			continue
		}
		target := ""
		if entry.SelfHarnessAudit != nil {
			target = strings.TrimSpace(entry.SelfHarnessAudit.TargetSignature)
		}
		if target == "" {
			delete(latestTargets, entry.SkillName)
			continue
		}
		latestTargets[entry.SkillName] = latestSelfHarnessTarget{
			skill:     entry.SkillName,
			signature: target,
			at:        entry.CreatedAt,
		}
	}
	if len(latestTargets) == 0 {
		return nil
	}
	usage, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return nil
	}
	perTarget := map[string]*targetRecurrence{}
	for _, record := range usage {
		if record.UsedAt < cutoff || record.Success || !isRealUsageRecord(record) {
			continue
		}
		target, ok := latestTargets[record.SkillName]
		if !ok || record.UsedAt <= target.at {
			continue
		}
		trace := usageFailureTraceFromRecord(record)
		if trace == nil {
			continue
		}
		if !selfHarnessSignatureMatches(normalizedSelfHarnessSignature(target.signature), normalizedSelfHarnessSignature(trace.Signature)) {
			continue
		}
		key := target.skill + "\x00" + target.signature
		rec, ok := perTarget[key]
		if !ok {
			rec = &targetRecurrence{skill: target.skill, signature: target.signature}
			perTarget[key] = rec
		}
		rec.recurrences++
		if record.UsedAt > rec.lastAt {
			rec.lastAt = record.UsedAt
		}
	}
	out := make([]targetRecurrence, 0, len(perTarget))
	for _, rec := range perTarget {
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].recurrences != out[j].recurrences {
			return out[i].recurrences > out[j].recurrences
		}
		return out[i].skill < out[j].skill
	})
	return out
}
