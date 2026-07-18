package genesis

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
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

	// selfCorrectionReopenCooldown is how long an APPLIED self-correction must
	// age before the same signature recurring again re-opens it. 2× the health
	// window: long enough that a genuine fix has had real usage to prove itself,
	// short enough that an unfixed recurrence resurfaces within a fortnight.
	// rejected/superseded candidates never re-open (the operator ruled).
	selfCorrectionReopenCooldown = 2 * evolutionHealthWindow

	// selfCorrectionReopenCap bounds how many times the SAME source signature
	// can re-open as a fresh candidate after an APPLIED fix failed to stick.
	// Without this, a stubbornly recurring defect floods the queue every
	// cooldown (14d) forever — the reopen signal is valuable but not infinitely
	// so. Past the cap, the signature is permanently blocked: the fix path is
	// exhausted and an operator must intervene (re-scope the fix, widen the
	// target, or accept the defect as a known limit).
	selfCorrectionReopenCap = 5

	// failureClusterPromoteThreshold is the minimum cluster support (recurring
	// members) before a failure cluster auto-promotes into a candidate. 2 — same
	// bar as target recurrence: a lone failure can be a one-off flake, but a
	// signature seen twice in the window is a real pattern. The cluster path is
	// the reliable deterministic backstop for lifecycle-log evolve_rejected +
	// recurring usage failures, which the per-event evolver hooks miss (they
	// key off a different recording path).
	failureClusterPromoteThreshold = 2
	// maxClusterPromotionsPerTick caps how many cluster candidates one tick can
	// mint so a burst of distinct failures cannot flood the queue.
	maxClusterPromotionsPerTick = 3
	// failureClusterSource prefixes the per-signature dedup marker.
	failureClusterSource = "failure-cluster"
	// failureClusterScanLimit bounds both the cluster mine and the dedup scan.
	failureClusterScanLimit = 50
)

// skillTargetMissingNote is appended to candidate evidence when the owning
// skill has no SKILL.md on disk — the skill was archived/removed (curation) or
// renamed after the failures were recorded. The candidate then deliberately
// carries no SKILL.md target: a guessed path sends the consumer to a
// nonexistent file (the exact ghost-read failure this note replaced).
const skillTargetMissingNote = "skill file not found under the managed skills root (archived/removed?) — verify with the skills tool before editing"

// resolveSkillTarget returns the ~-compacted SKILL.md path for skillName,
// resolved against the real on-disk nesting under the managed skills root
// (flat, category, genesis/<category> — see skills.FindSkillFile). The
// previous naive root+name join recorded phantom targets like
// ~/.deneb/skills/<name>/SKILL.md while genesis skills live two levels deeper.
func (t *Tracker) resolveSkillTarget(skillName string) (string, bool) {
	root := t.skillsRoot
	if root == "" {
		root = skills.DefaultManagedSkillsDir()
	}
	p, ok := skills.FindSkillFile(root, skillName)
	if !ok {
		return "", false
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, relErr := filepath.Rel(home, p); relErr == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "~/" + filepath.ToSlash(rel), true
		}
	}
	return p, true
}

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
	now := time.Now()
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
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
		if selfCorrectionReopenBlocked(existing, source, rec.lastAt, now) {
			continue
		}
		evidence := fmt.Sprintf("signature=%s; recurrences(window)=%d; lastAt=%s",
			rec.signature, rec.recurrences, time.UnixMilli(rec.lastAt).Format(time.RFC3339))
		targets := []string{"~/.deneb/data/skill_validation_cases.jsonl"}
		if p, ok := t.resolveSkillTarget(rec.skill); ok {
			targets = append([]string{p}, targets...)
		} else {
			evidence += "\n" + skillTargetMissingNote
		}
		if _, err := t.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			Scope:     "skill",
			SkillName: rec.skill,
			Title:     "Accepted evolve target keeps recurring: " + rec.skill,
			Candidate: fmt.Sprintf(
				"The last accepted evolve for %s targeted failure signature %q, but the signature recurred %d times in real usage after the evolve — the fix did not stick.",
				rec.skill, rec.signature, rec.recurrences,
			),
			Evidence:       evidence,
			TargetFiles:    targets,
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

// selfCorrectionReopenBlocked reports whether a fresh deterministic promotion for
// `source` should be suppressed given the existing candidates (matched by Source
// prefix). Shared by every deterministic promoter (recurrence, failure cluster,
// patch-first repeats).
//
// A signature stays blocked while a twin is still live (proposed/accepted) or was
// rejected/superseded — the operator ruled, and auto re-opening would spam the
// queue. The ONE path that re-opens: a candidate that reached APPLIED (the fix was
// attempted) whose signature recurs AGAIN after selfCorrectionReopenCooldown.
// A terminal no-effect/regressed impact verdict is stronger evidence that the
// fix did not hold, so a recurrence observed after that verdict can reopen
// immediately. Neither path creates work without fresh recurrence evidence.
// freshLastAt is the newest evidence timestamp (unix millis) for the signature.
func selfCorrectionReopenBlocked(existing []SelfCorrectionCandidateRecord, source string, freshLastAt int64, now time.Time) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	var newest *SelfCorrectionCandidateRecord
	sourceTwins := 0
	for i := range existing {
		if !selfCorrectionSourceMatches(existing[i].Source, source) {
			continue
		}
		sourceTwins++
		if newest == nil || existing[i].CreatedAt > newest.CreatedAt {
			c := existing[i]
			newest = &c
		}
	}
	if newest == nil {
		return false // never promoted → allow the first capture
	}
	// Reopen cap: a signature that has already re-opened selfCorrectionReopenCap
	// times (sourceTwins counts every prior candidate for this source) has
	// exhausted the auto-reopen path. Block permanently — the operator must
	// break the cycle manually. The cap is on candidate count, not just reopen
	// events, because each prior twin either WAS a reopen or the original.
	if sourceTwins > selfCorrectionReopenCap {
		return true
	}
	if normalizeSelfCorrectionStatus(newest.Status) != SelfCorrectionStatusApplied {
		return true // live twin, or operator-ruled (rejected/superseded) → block
	}
	if result := newest.ImpactResult; result != nil && result.CheckedAt > 0 &&
		(result.Status == selfCorrectionImpactNoEffect || result.Status == selfCorrectionImpactRegressed) {
		return freshLastAt <= result.CheckedAt
	}
	// Applied: re-open only if the fix had time to prove itself AND the signature
	// recurred again after its latest lifecycle update. UpdatedAt is the actual
	// watch/impact boundary on folded rows; CreatedAt is the legacy fallback.
	appliedAt := max(newest.CreatedAt, newest.UpdatedAt)
	cooled := now.UnixMilli()-appliedAt >= selfCorrectionReopenCooldown.Milliseconds()
	recurredAgain := freshLastAt > appliedAt
	return !(cooled && recurredAgain)
}

// selfCorrectionSourceMatches is the twin test for reopen suppression: an
// exact source, or the same source extended past a separator (":" variants a
// promoter appends). A bare prefix must NOT match — source ids that are
// prefixes of one another ("…latency" vs "…latency-p99") used to cross-block
// (RSI code eval M7/F4). Mirrored by scripts/audit/health_finding_miner.py.
func selfCorrectionSourceMatches(existing, source string) bool {
	return existing == source || strings.HasPrefix(existing, source+":")
}

// PromoteFailureClusterCandidates converts the top recurring failure clusters into
// proposed self-correction candidates DETERMINISTICALLY (no LLM call), so the
// queue is fed even when the LLM sweep turn ignores its nudge. Support-gated,
// per-tick capped, and dedup/cooldown-shared with recurrence promotion. Returns
// how many candidates were captured.
func (t *Tracker) PromoteFailureClusterCandidates() (int, error) {
	clusters := t.FailureEvidenceClusters(failureClusterScanLimit)
	now := time.Now()
	promoted := 0
	for _, c := range clusters {
		if promoted >= maxClusterPromotionsPerTick {
			break
		}
		if c.Support < failureClusterPromoteThreshold {
			continue // support-ordered list, but keep the gate explicit
		}
		signature := strings.TrimSpace(c.Signature)
		if signature == "" {
			continue
		}
		source := failureClusterCandidateSource(signature)
		existing, err := t.RecentSelfCorrectionCandidates(c.Skill, "", failureClusterScanLimit)
		if err != nil {
			return promoted, fmt.Errorf("genesis-tracker: cluster dedup scan: %w", err)
		}
		if selfCorrectionReopenBlocked(existing, source, c.LastAt, now) {
			continue
		}
		skill := strings.TrimSpace(c.Skill)
		scope := "test"
		targets := []string{"~/.deneb/data/skill_validation_cases.jsonl"}
		title := "Recurring failure cluster: " + signature
		skillFileNote := ""
		if skill != "" {
			scope = "skill"
			title = "Recurring failure in " + skill + ": " + signature
			if p, ok := t.resolveSkillTarget(skill); ok {
				targets = append([]string{p}, targets...)
			} else {
				skillFileNote = "\n" + skillTargetMissingNote
			}
		}
		evidence := fmt.Sprintf("kind=%s; support=%d; lastAt=%s",
			c.Kind, c.Support, time.UnixMilli(c.LastAt).Format(time.RFC3339))
		evidence += "\n" + formatFailureRouteEvidence(c.Route)
		if ex := strings.TrimSpace(c.Example); ex != "" {
			evidence += "\nexample: " + ex
		}
		evidence += skillFileNote
		if _, err := t.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			Scope:     scope,
			SkillName: skill,
			Title:     title,
			Candidate: fmt.Sprintf(
				"Failure signature %q recurred %d times in the health window (kind=%s). Find the root cause and either evolve the owning skill or pin a held-out validation case so the pattern is caught next time.",
				signature, c.Support, c.Kind,
			),
			Evidence:       evidence,
			TargetFiles:    targets,
			ProposedChange: "Review the clustered failures, fix the root cause (skill body or handling), and add a held-out validation case reproducing the signature.",
			Risk:           "Deterministic promotion from clustered failure traces; signature matching is fuzzy substring and the shadow route is advisory — confirm the members share a root cause and the intervention surface before acting.",
			Source:         source,
		}); err != nil {
			// A forbidden-surface target or weak-record rejection kills THIS
			// candidate only; keep mining the rest of the clusters.
			if t.logger != nil {
				t.logger.Warn("genesis-tracker: cluster candidate rejected",
					"signature", signature, "error", err)
			}
			continue
		}
		promoted++
	}
	return promoted, nil
}

// failureClusterCandidateSource builds the per-signature dedup marker for a
// failure-cluster promotion. Signature is normalized before hashing so cosmetic
// variants of the same signature share a marker.
func failureClusterCandidateSource(signature string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(normalizedSelfHarnessSignature(signature)))
	return fmt.Sprintf("%s:%08x", failureClusterSource, h.Sum32())
}
