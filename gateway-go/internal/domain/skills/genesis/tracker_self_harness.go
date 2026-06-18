package genesis

import (
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
		if strings.Contains(reason, "self-harness audit rejected") && strings.Contains(reason, "missing") {
			s.MissingAuditRejections7d++
		}
		if strings.Contains(reason, "does not match supported failure signatures") ||
			strings.Contains(reason, "no failure evidence bundle") {
			s.SignatureMismatchRejections7d++
		}
		if strings.Contains(reason, "self-harness surface rejected") ||
			strings.Contains(reason, "did not match changed skill.md sections") ||
			strings.Contains(reason, "not editable by skill.md body evolve") {
			s.SurfaceMismatchRejections7d++
		}
		if strings.Contains(reason, "held-out") || strings.Contains(reason, "replay") {
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
		if rec.Type == SelfCorrectionTypeReview || rec.CreatedAt < cutoff {
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
		return
	}
	usage, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return
	}
	perTarget := map[string]int{}
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
		s.TargetRecurrences7d++
		key := target.skill + "\x00" + target.signature
		perTarget[key]++
	}
	for key, count := range perTarget {
		if count <= s.TopRecurringTargetRecurrences {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		s.TopRecurringTargetSkill = parts[0]
		if len(parts) > 1 {
			s.TopRecurringTargetSignature = parts[1]
		}
		s.TopRecurringTargetRecurrences = count
	}
}
