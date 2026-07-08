package genesis

import (
	"fmt"
	"hash/fnv"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Target-recurrence promotion: the strongest self-improvement signal the
// tracker owns is "an accepted evolve claimed to fix signature S, and S keeps
// happening in real use". Until now that signal only surfaced as a health
// metric (SelfHarnessSignals.TargetRecurrences7d) that nobody consumed — the
// documented capture-without-consumption failure mode. This lane converts it
// into a proposed self-correction candidate deterministically (no LLM call),
// so the heartbeat review lane picks it up within a tick.
const (
	// targetRecurrencePromoteThreshold requires the signature to recur at
	// least twice inside the health window — a single recurrence can be an
	// unrelated flake matching the fuzzy signature.
	targetRecurrencePromoteThreshold = 2
	// targetRecurrenceSource prefixes the per-(skill, signature) dedup marker
	// (same convention as skillPatchFirstRepeatSource): makeSelfCorrectionID
	// embeds CreatedAt so identical content still mints fresh IDs — dedup must
	// prefix-match Source across existing candidates, any status blocking
	// re-promotion.
	targetRecurrenceSource = "target-recurrence"

	targetRecurrenceDedupScanLimit = 50
)

// PromoteTargetRecurrenceCandidates converts fresh target-recurrence signals
// into proposed self-correction candidates. Idempotent per (skill, signature):
// once promoted, that signature never re-promotes regardless of review outcome
// (the operator has ruled; auto re-opening would spam the queue). Returns how
// many candidates were captured.
func (t *Tracker) PromoteTargetRecurrenceCandidates() (int, error) {
	t.mu.Lock()
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		t.mu.Unlock()
		return 0, fmt.Errorf("genesis-tracker: load lifecycle log: %w", err)
	}
	cutoff := time.Now().Add(-evolutionHealthWindow).UnixMilli()
	recs := t.targetRecurrencesLocked(entries, cutoff)
	t.mu.Unlock()

	promoted := 0
	for _, rec := range recs {
		if rec.recurrences < targetRecurrencePromoteThreshold {
			continue
		}
		source := targetRecurrenceCandidateSource(rec.signature)
		existing, err := t.RecentSelfCorrectionCandidates(rec.skill, "", targetRecurrenceDedupScanLimit)
		if err != nil {
			return promoted, fmt.Errorf("genesis-tracker: recurrence dedup scan: %w", err)
		}
		blocked := false
		for _, cand := range existing {
			if cand.Source == source {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		if _, err := t.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			Scope:     "skill",
			SkillName: rec.skill,
			Title:     "Accepted evolve target keeps recurring: " + rec.skill,
			Candidate: fmt.Sprintf(
				"The last accepted evolve for %s targeted failure signature %q, but the signature recurred %d times in real usage after the evolve — the fix did not stick.",
				rec.skill, rec.signature, rec.recurrences),
			Evidence: fmt.Sprintf("signature=%s; recurrences(window)=%d; lastAt=%s",
				rec.signature, rec.recurrences, time.UnixMilli(rec.lastAt).Format(time.RFC3339)),
			TargetFiles: []string{
				"~/.deneb/skills/" + rec.skill + "/SKILL.md",
				"~/.deneb/data/skill_validation_cases.jsonl",
			},
			ProposedChange: "Re-evolve the skill against this recurrence evidence, or pin the signature as a held-out validation case so the next evolve cannot be accepted without actually fixing it.",
			Risk:           "Deterministic promotion from usage traces; signature matching is fuzzy substring — confirm the recurrences share the root cause before re-evolving.",
			Source:         source,
		}); err != nil {
			return promoted, fmt.Errorf("genesis-tracker: record recurrence candidate: %w", err)
		}
		promoted++
	}
	return promoted, nil
}

// targetRecurrenceCandidateSource builds the dedup marker for one signature.
// The signature is normalized before hashing so cosmetic whitespace/spacing
// variants of the same signature do not re-promote.
func targetRecurrenceCandidateSource(signature string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(normalizedSelfHarnessSignature(signature)))
	return fmt.Sprintf("%s:%08x", targetRecurrenceSource, h.Sum32())
}
