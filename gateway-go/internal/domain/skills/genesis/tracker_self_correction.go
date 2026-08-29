package genesis

import (
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/surfaces"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	selfCorrectionTypeCandidate = "self_correction_candidate"
	selfCorrectionTypeReview    = "self_correction_review"
	selfCorrectionTypeDispatch  = "self_correction_dispatch"
	selfCorrectionTypeImpact    = "self_correction_impact"

	SelfCorrectionStatusProposed   = string(rsilifecycle.ReviewProposed)
	SelfCorrectionStatusAccepted   = string(rsilifecycle.ReviewAccepted)
	SelfCorrectionStatusApplied    = string(rsilifecycle.ReviewApplied)
	SelfCorrectionStatusRejected   = string(rsilifecycle.ReviewRejected)
	selfCorrectionStatusSuperseded = string(rsilifecycle.ReviewSuperseded)

	selfCorrectionDispatchStarted     = string(rsilifecycle.DeliveryStarted)
	selfCorrectionDispatchPROpened    = string(rsilifecycle.DeliveryPROpened)
	selfCorrectionDispatchMerged      = string(rsilifecycle.DeliveryMerged)
	selfCorrectionDispatchDeployed    = string(rsilifecycle.DeliveryDeployed)
	selfCorrectionDispatchWatchPassed = string(rsilifecycle.DeliveryWatchPassed)
	selfCorrectionDispatchDeclined    = string(rsilifecycle.DeliveryDeclined)
	selfCorrectionDispatchFailed      = string(rsilifecycle.DeliveryFailed)
	selfCorrectionDispatchRolledBack  = string(rsilifecycle.DeliveryRolledBack)
)

// SelfCorrectionCandidateRecord is an append-only proposal for a future coding
// agent to review. It deliberately does not apply anything; the value is in
// preserving the model's observation, evidence, and risk note until a batch
// review can accept/reject it with tests.
type SelfCorrectionCandidateRecord struct {
	Type        string   `json:"type"`
	ID          string   `json:"id"`
	Status      string   `json:"status,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	SkillName   string   `json:"skillName,omitempty"`
	SessionKey  string   `json:"sessionKey,omitempty"`
	Title       string   `json:"title,omitempty"`
	Candidate   string   `json:"candidate,omitempty"`
	Evidence    string   `json:"evidence,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	TargetFiles []string `json:"targetFiles,omitempty"`
	// Surface is the declared editable-surface tier summarizing TargetFiles
	// (editable_surfaces.go): auto-apply | propose-only. Empty on legacy rows
	// and target-less candidates.
	Surface        string `json:"surface,omitempty"`
	ProposedChange string `json:"proposedChange,omitempty"`
	Risk           string `json:"risk,omitempty"`
	Source         string `json:"source,omitempty"`
	// ProcedureRef is the composite procedure token (generation.ProcedureRef,
	// "proc-<hex>") of the LLM procedure that PRODUCED this candidate — the L4
	// analogue of the L1 evolve certificate. Populated for candidates an evolve
	// run spawned (the evolve prompt that emitted them); empty for
	// deterministically-mined candidates (runtime-error signatures, failure
	// clusters — no LLM procedure), and reserved-empty for the dispatch procedure
	// (that governs the out-of-process coding session, a separate stage). Purely
	// additive attribution — it feeds no gate.
	ProcedureRef string `json:"procedureRef,omitempty"`
	// Consumer names who can actually act on this candidate once it leaves
	// review: "coding-dispatch" when the L4 lane will claim it (scope=code on an
	// allowlisted or ladder-graduated source), "none" when nothing will. Set on
	// the REVIEW SURFACE only and never persisted — the allowlist changes over
	// time, so a stored copy would go stale and lie. Without it a reviewer cannot
	// tell "queued" from "shelved forever" and defaults to accepted, which is how
	// 22 candidates went silent (ledger audit 2026-08-01).
	Consumer       string                       `json:"consumer,omitempty"`
	Reviewer       string                       `json:"reviewer,omitempty"`
	ReviewNote     string                       `json:"reviewNote,omitempty"`
	ImpactContract *rsilifecycle.ImpactContract `json:"impactContract,omitempty"`
	ImpactResult   *rsilifecycle.ImpactResult   `json:"impactResult,omitempty"`
	// Dispatch fields are populated on self_correction_dispatch rows and folded
	// onto the candidate read model. The review status and delivery status are
	// deliberately separate: accepted means "approved to try"; applied means a
	// merged deploy survived the rollback watch.
	DispatchPhase string `json:"dispatchPhase,omitempty"`
	AttemptID     string `json:"attemptId,omitempty"`
	Branch        string `json:"branch,omitempty"`
	PRNumber      int    `json:"prNumber,omitempty"`
	PRURL         string `json:"prUrl,omitempty"`
	CommitSHA     string `json:"commitSha,omitempty"`
	DeployHead    string `json:"deployHead,omitempty"`
	OutcomeNote   string `json:"outcomeNote,omitempty"`
	// DispatchFailures is the fold-derived count of dispatch attempts that
	// terminated in the "failed" delivery phase (process failure/timeout, never
	// landed). It is NOT a ledger input: applySelfCorrectionDispatch increments
	// it while folding the append-only history, and SelfCorrectionDispatchEligible
	// reads it to stop re-dispatching a candidate an unattended coding session
	// keeps failing to complete (doctrine-conflicting or too large), which would
	// otherwise burn a coding session on every dispatch tick.
	DispatchFailures int `json:"dispatchFailures,omitempty"`
	// DispatchProcedureRef is the composite procedure token of the coding-session
	// contract prompt (generation.MetaDispatchContractPrompt) that governed THIS
	// dispatch attempt — the dispatch-stage counterpart to ProcedureRef (which is
	// the candidate's PRODUCTION procedure). Stamped in-process at dispatch time
	// and adopted first-seen per attempt (cleared by resetSelfCorrectionDelivery
	// on a new attempt so a retry re-captures the then-current version). Purely
	// additive attribution — it feeds no gate.
	DispatchProcedureRef string `json:"dispatchProcedureRef,omitempty"`
	CreatedAt            int64  `json:"createdAt"`
	UpdatedAt            int64  `json:"updatedAt,omitempty"`
}

// RecordSelfCorrectionCandidate appends a deferred self-correction candidate.
func (t *Tracker) RecordSelfCorrectionCandidate(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixMilli()
	record.Type = selfCorrectionTypeCandidate
	record.Status = normalizeSelfCorrectionStatus(record.Status)
	if record.Status == "" {
		record.Status = SelfCorrectionStatusProposed
	}
	record.Scope = normalizeSelfCorrectionScope(record.Scope)
	record.Title = strings.TrimSpace(record.Title)
	record.Candidate = strings.TrimSpace(record.Candidate)
	record.Evidence = strings.TrimSpace(record.Evidence)
	record.Reason = strings.TrimSpace(record.Reason)
	record.ProposedChange = strings.TrimSpace(record.ProposedChange)
	record.Risk = strings.TrimSpace(record.Risk)
	record.Source = strings.TrimSpace(record.Source)
	record.ProcedureRef = strings.TrimSpace(record.ProcedureRef)
	record.SessionKey = strings.TrimSpace(record.SessionKey)
	record.SkillName = strings.TrimSpace(record.SkillName)
	record.TargetFiles = cleanSelfCorrectionStrings(record.TargetFiles, 20)
	impactContract, err := normalizeSelfCorrectionImpactContract(record.ImpactContract)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: invalid self-correction impact contract: %w", err)
	}
	record.ImpactContract = impactContract
	// Declared-surface enforcement (Self-Harness: permission control outside
	// the loop): a forbidden target rejects the whole candidate at record time,
	// and the summary tier travels with the record so reviewers see at a glance
	// whether this could ever auto-apply or must land as a reviewed PR.
	tier, forbidden := surfaces.ClassifyProposalSurfaces(record.TargetFiles)
	if len(forbidden) > 0 {
		return record, fmt.Errorf("genesis-tracker: self-correction targets a forbidden surface: %s", strings.Join(forbidden, ", "))
	}
	record.Surface = tier
	if record.CreatedAt == 0 {
		record.CreatedAt = now
	}
	record.UpdatedAt = record.CreatedAt
	if record.ID == "" {
		record.ID = makeSelfCorrectionID(record)
	}
	record.ID = strings.TrimSpace(record.ID)
	if record.Title == "" && record.Candidate == "" && record.ProposedChange == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate needs title, candidate, or proposedChange")
	}
	if record.ID == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction id is required")
	}
	if err := jsonlstore.Append(t.selfCorrectionPath, record); err != nil {
		return record, fmt.Errorf("genesis-tracker: append self-correction candidate: %w", err)
	}
	return record, nil
}

// RecordSelfCorrectionReview appends a status update for a deferred candidate.
// ErrSelfCorrectionTransition marks a review whose target has already left the
// reviewable states. It is a sentinel so callers can answer "this one is
// already settled" instead of surfacing a tool ERROR — the heartbeat review
// contract tells the model to abort the whole turn on an error, so one stale id
// carried in from memory used to cost every other candidate its review.
// Measured 2026-08-26: 18 such attempts over 7 days across 7 ids, one retried
// five times, all of them <terminal> -> accepted.
var ErrSelfCorrectionTransition = errors.New("genesis-tracker: invalid self-correction status transition")

func (t *Tracker) RecordSelfCorrectionReview(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record.Type = selfCorrectionTypeReview
	record.ID = strings.TrimSpace(record.ID)
	record.Status = normalizeSelfCorrectionStatus(record.Status)
	record.Reviewer = strings.TrimSpace(record.Reviewer)
	record.ReviewNote = strings.TrimSpace(record.ReviewNote)
	record.UpdatedAt = time.Now().UnixMilli()
	record.CreatedAt = record.UpdatedAt
	if record.ID == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction review id is required")
	}
	if record.Status == "" || record.Status == SelfCorrectionStatusProposed {
		return record, fmt.Errorf("genesis-tracker: review status must be accepted, rejected, superseded, or applied")
	}
	current, found, err := t.mergedSelfCorrectionCandidateLocked(record.ID)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}
	if !found {
		// A hard error on purpose: an unknown id means the caller is reviewing
		// something that never existed, which is a different problem from
		// reviewing something already settled. The id shape is spelled out
		// because a truncated id was the recorded way of getting here.
		return record, fmt.Errorf("genesis-tracker: self-correction candidate not found: %s (ids are sc-<millis>-<hash>; a unique prefix also resolves)", record.ID)
	}
	// A prefix resolved to a full id: write the review under the id the ledger
	// actually keys on, or the row lands as an orphan nothing folds back in —
	// which is the very way this funnel loses work.
	record.ID = current.ID
	if current.Status == record.Status {
		return record, nil // idempotent retry: do not inflate the append-only funnel
	}
	if !validSelfCorrectionStatusTransition(current.Status, record.Status) {
		return record, fmt.Errorf("%w: %s -> %s", ErrSelfCorrectionTransition, current.Status, record.Status)
	}
	if err := jsonlstore.Append(t.selfCorrectionPath, record); err != nil {
		return record, fmt.Errorf("genesis-tracker: append self-correction review: %w", err)
	}
	return record, nil
}

// RecordSelfCorrectionDispatch appends one delivery event for a coding
// candidate. It is the authoritative result ledger; filesystem dispatch
// markers remain only crash/idempotency guards and daily-budget receipts.
// A watch_passed event is itself the applied verdict in the folded read model,
// so closure is one durable append rather than two writes that could tear.
func (t *Tracker) RecordSelfCorrectionDispatch(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
	if record.ImpactResult != nil {
		return t.recordSelfCorrectionImpact(record)
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixMilli()
	record.Type = selfCorrectionTypeDispatch
	record.ID = strings.TrimSpace(record.ID)
	record.DispatchPhase = normalizeSelfCorrectionDispatchPhase(record.DispatchPhase)
	record.AttemptID = strings.TrimSpace(record.AttemptID)
	record.Branch = strings.TrimSpace(record.Branch)
	record.PRURL = strings.TrimSpace(record.PRURL)
	record.CommitSHA = strings.TrimSpace(record.CommitSHA)
	record.DeployHead = strings.TrimSpace(record.DeployHead)
	record.OutcomeNote = strings.TrimSpace(record.OutcomeNote)
	record.CreatedAt = now
	record.UpdatedAt = now
	if record.ID == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction dispatch id is required")
	}
	if record.DispatchPhase == "" {
		return record, fmt.Errorf("genesis-tracker: invalid self-correction dispatch phase")
	}
	if record.AttemptID == "" {
		return record, fmt.Errorf("genesis-tracker: self-correction dispatch attemptId is required")
	}

	current, found, err := t.mergedSelfCorrectionCandidateLocked(record.ID)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}
	if !found {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate not found: %s (ids are sc-<millis>-<hash>; a unique prefix also resolves)", record.ID)
	}
	record.ID = current.ID // see RecordSelfCorrectionReview: never append under a prefix
	if current.DispatchPhase == record.DispatchPhase && current.AttemptID == record.AttemptID {
		adds, conflict := selfCorrectionDispatchProvenanceDelta(current, record)
		if conflict != "" {
			return record, fmt.Errorf("genesis-tracker: conflicting self-correction dispatch provenance: %s", conflict)
		}
		if !adds {
			return record, nil // exact idempotent RPC retry
		}
		// Same phase may be enriched later (for example GitHub exposes the merge
		// SHA shortly after state first flips to MERGED). Append the missing
		// provenance without advancing or inflating phase counters.
		if err := jsonlstore.Append(t.selfCorrectionPath, record); err != nil {
			return record, fmt.Errorf("genesis-tracker: append self-correction dispatch enrichment: %w", err)
		}
		return record, nil
	}
	if current.AttemptID == record.AttemptID {
		if _, conflict := selfCorrectionDispatchProvenanceDelta(current, record); conflict != "" {
			return record, fmt.Errorf("genesis-tracker: conflicting self-correction dispatch provenance: %s", conflict)
		}
	}
	if !validSelfCorrectionDispatchTransition(current.DispatchPhase, record.DispatchPhase) {
		return record, fmt.Errorf("genesis-tracker: invalid self-correction dispatch transition %s -> %s", current.DispatchPhase, record.DispatchPhase)
	}
	if current.DispatchPhase != "" && record.DispatchPhase != selfCorrectionDispatchStarted && current.AttemptID != record.AttemptID {
		return record, fmt.Errorf("genesis-tracker: dispatch attempt changed inside lifecycle: %s -> %s", current.AttemptID, record.AttemptID)
	}
	if record.DispatchPhase == selfCorrectionDispatchStarted && current.AttemptID == record.AttemptID {
		return record, fmt.Errorf("genesis-tracker: retry must use a new dispatch attemptId")
	}
	if record.DispatchPhase == selfCorrectionDispatchWatchPassed && current.Status != SelfCorrectionStatusApplied &&
		!validSelfCorrectionStatusTransition(current.Status, SelfCorrectionStatusApplied) {
		return record, fmt.Errorf("genesis-tracker: watched dispatch cannot close status %s as applied", current.Status)
	}
	if err := jsonlstore.Append(t.selfCorrectionPath, record); err != nil {
		return record, fmt.Errorf("genesis-tracker: append self-correction dispatch: %w", err)
	}
	return record, nil
}

func selfCorrectionDispatchProvenanceDelta(current, next SelfCorrectionCandidateRecord) (bool, string) {
	for _, field := range []struct {
		name          string
		current, next string
	}{
		{"branch", current.Branch, next.Branch},
		{"prUrl", current.PRURL, next.PRURL},
		{"commitSha", current.CommitSHA, next.CommitSHA},
		{"deployHead", current.DeployHead, next.DeployHead},
	} {
		if field.current != "" && field.next != "" && field.current != field.next {
			return false, fmt.Sprintf("%s changed from %q to %q", field.name, field.current, field.next)
		}
	}
	if current.PRNumber > 0 && next.PRNumber > 0 && current.PRNumber != next.PRNumber {
		return false, fmt.Sprintf("prNumber changed from %d to %d", current.PRNumber, next.PRNumber)
	}
	return (current.Branch == "" && next.Branch != "") ||
		(current.PRNumber == 0 && next.PRNumber > 0) ||
		(current.PRURL == "" && next.PRURL != "") ||
		(current.CommitSHA == "" && next.CommitSHA != "") ||
		(current.DeployHead == "" && next.DeployHead != "") ||
		(current.OutcomeNote == "" && next.OutcomeNote != ""), ""
}

// RecentSelfCorrectionCandidates returns the latest merged view of deferred
// self-correction candidates, newest first. statusFilter="" means all statuses.
func (t *Tracker) RecentSelfCorrectionCandidates(skillName, statusFilter string, limit int) ([]SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}
	statusFilter = normalizeSelfCorrectionStatus(statusFilter)
	skillName = strings.TrimSpace(skillName)
	merged, err := t.mergedSelfCorrectionCandidatesLocked()
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}

	out := make([]SelfCorrectionCandidateRecord, 0, len(merged))
	for _, rec := range merged {
		if skillName != "" && rec.SkillName != skillName {
			continue
		}
		if statusFilter != "" && rec.Status != statusFilter {
			continue
		}
		out = append(out, rec)
	}
	sortSelfCorrectionCandidates(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// allSelfCorrectionCandidates returns the complete folded ledger for internal
// policy/status consumers that must agree with dispatch selection beyond the
// bounded operator list view.
func (t *Tracker) allSelfCorrectionCandidates() ([]SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	merged, err := t.mergedSelfCorrectionCandidatesLocked()
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}
	out := make([]SelfCorrectionCandidateRecord, 0, len(merged))
	for _, record := range merged {
		out = append(out, record)
	}
	return out, nil
}

func (t *Tracker) mergedSelfCorrectionCandidatesLocked() (map[string]SelfCorrectionCandidateRecord, error) {
	merged := make(map[string]SelfCorrectionCandidateRecord)
	err := t.scanSelfCorrectionRecords(func(rec SelfCorrectionCandidateRecord) {
		mergeSelfCorrectionRecord(merged, rec)
	})
	return merged, err
}

// mergedSelfCorrectionCandidateLocked folds every ledger row for one candidate
// into its current state. The id may be a PREFIX of the stored id, resolved
// only when exactly one candidate matches.
//
// Candidate ids are minted as sc-<epochMillis>-<hash8>, and callers lose the
// hash: production hit `self-correction candidate not found: sc-1787744942869`
// on 2026-08-26 and again on 2026-08-29, both times for an id whose full form
// (sc-…-5a66182a, sc-…-7735ae71) was sitting in the ledger, unique on that
// prefix. The review died on an exact-match miss, and a self-correction that
// never lands is the funnel's known way of losing work silently. An ambiguous
// prefix is still an error — the point is to resolve what is unambiguous, never
// to guess between two candidates.
func (t *Tracker) mergedSelfCorrectionCandidateLocked(id string) (SelfCorrectionCandidateRecord, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return SelfCorrectionCandidateRecord{}, false, nil
	}

	var current SelfCorrectionCandidateRecord
	found := false
	prefixIDs := map[string]struct{}{}
	err := t.scanSelfCorrectionRecords(func(rec SelfCorrectionCandidateRecord) {
		rec.ID = strings.TrimSpace(rec.ID)
		switch {
		case rec.ID == id:
			current, found = foldSelfCorrectionRecord(current, found, rec)
		case strings.HasPrefix(rec.ID, id):
			prefixIDs[rec.ID] = struct{}{}
		}
	})
	if err != nil || found {
		return current, found, err
	}
	switch len(prefixIDs) {
	case 0:
		return current, false, nil
	case 1:
		var full string
		for candidateID := range prefixIDs {
			full = candidateID
		}
		return t.mergedSelfCorrectionCandidateLocked(full)
	default:
		return current, false, fmt.Errorf(
			"genesis-tracker: self-correction id %q matches %d candidates (%s) — pass the full sc-<millis>-<hash> id",
			id, len(prefixIDs), strings.Join(sortedKeys(prefixIDs), ", "),
		)
	}
}

// sortedKeys returns a set's keys in a stable order so an ambiguity error reads
// the same way twice.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (t *Tracker) scanSelfCorrectionRecords(visit func(SelfCorrectionCandidateRecord)) error {
	stats, err := jsonlstore.Scan(t.selfCorrectionPath, visit)
	if err != nil {
		return err
	}
	if stats.SkippedLines() > 0 {
		return fmt.Errorf(
			"self-correction ledger %s is corrupt (malformed=%d oversize=%d)",
			t.selfCorrectionPath,
			stats.CorruptLines,
			stats.OversizeLines,
		)
	}
	return nil
}

func mergeSelfCorrectionRecord(merged map[string]SelfCorrectionCandidateRecord, rec SelfCorrectionCandidateRecord) {
	id := strings.TrimSpace(rec.ID)
	if id == "" {
		return
	}
	base, found := merged[id]
	if next, ok := foldSelfCorrectionRecord(base, found, rec); ok {
		merged[id] = next
	}
}

func mergeSelfCorrectionRecords(records []SelfCorrectionCandidateRecord) map[string]SelfCorrectionCandidateRecord {
	merged := make(map[string]SelfCorrectionCandidateRecord)
	for _, rec := range records {
		mergeSelfCorrectionRecord(merged, rec)
	}
	return merged
}

func foldSelfCorrectionRecord(
	base SelfCorrectionCandidateRecord,
	found bool,
	rec SelfCorrectionCandidateRecord,
) (SelfCorrectionCandidateRecord, bool) {
	switch rec.Type {
	case selfCorrectionTypeReview:
		if found {
			base = applySelfCorrectionReview(base, rec)
		}
	case selfCorrectionTypeDispatch:
		if found {
			base = applySelfCorrectionDispatch(base, rec)
		}
	case selfCorrectionTypeImpact:
		if found {
			base = applySelfCorrectionImpact(base, rec)
		}
	case "", selfCorrectionTypeCandidate: // empty type is a legacy candidate row
		return normalizedSelfCorrectionCandidate(rec), true
	}
	return base, found
}

// applySelfCorrectionReview folds one review row into the merged candidate.
func applySelfCorrectionReview(base, rec SelfCorrectionCandidateRecord) SelfCorrectionCandidateRecord {
	if status := normalizeSelfCorrectionStatus(rec.Status); status != "" {
		base.Status = status
	}
	if rec.Reviewer != "" {
		base.Reviewer = rec.Reviewer
	}
	if rec.ReviewNote != "" {
		base.ReviewNote = rec.ReviewNote
	}
	if rec.UpdatedAt > 0 {
		base.UpdatedAt = rec.UpdatedAt
	}
	return base
}

// applySelfCorrectionDispatch folds one dispatch row into the merged candidate.
func applySelfCorrectionDispatch(base, rec SelfCorrectionCandidateRecord) SelfCorrectionCandidateRecord {
	phase := normalizeSelfCorrectionDispatchPhase(rec.DispatchPhase)
	if phase == selfCorrectionDispatchStarted && rec.AttemptID != "" &&
		base.AttemptID != "" && rec.AttemptID != base.AttemptID {
		base = resetSelfCorrectionDelivery(base)
	}
	samePhase := base.DispatchPhase == phase && base.AttemptID == rec.AttemptID
	if phase == selfCorrectionDispatchFailed && !samePhase {
		// Count each distinct attempt that terminates in failure. samePhase means
		// this is a duplicate "failed" row for the attempt already folded, so it
		// must not double-count. resetSelfCorrectionDelivery deliberately leaves
		// DispatchFailures intact so the count accumulates across retries.
		base.DispatchFailures++
	}
	base.DispatchPhase = phase
	if rec.AttemptID != "" {
		base.AttemptID = rec.AttemptID
	}
	base = fillSelfCorrectionDelivery(base, rec)
	if rec.OutcomeNote != "" && (!samePhase || base.OutcomeNote == "") {
		base.OutcomeNote = rec.OutcomeNote
	}
	if derived := rsilifecycle.ReviewAfterDelivery(
		rsilifecycle.ReviewState(base.Status),
		rsilifecycle.DeliveryPhase(base.DispatchPhase),
	); derived == rsilifecycle.ReviewApplied && base.Status != SelfCorrectionStatusApplied {
		base.Status = SelfCorrectionStatusApplied
		base.Reviewer = "deploy-watch"
		if base.ReviewNote == "" {
			base.ReviewNote = "merged deployment survived rollback watch"
		}
	}
	if rec.UpdatedAt > 0 {
		base.UpdatedAt = rec.UpdatedAt
	}
	return base
}

// resetSelfCorrectionDelivery clears delivery evidence when a new dispatch
// attempt starts, so a retry never inherits the previous attempt's PR/commit.
func resetSelfCorrectionDelivery(base SelfCorrectionCandidateRecord) SelfCorrectionCandidateRecord {
	base.Branch = ""
	base.PRNumber = 0
	base.PRURL = ""
	base.CommitSHA = ""
	base.DeployHead = ""
	base.OutcomeNote = ""
	// A new attempt may run under a revised dispatch contract, so drop the ref
	// too — the attempt's own started row re-captures the then-current version.
	base.DispatchProcedureRef = ""
	return base
}

// fillSelfCorrectionDelivery adopts first-seen delivery evidence from a
// dispatch row without overwriting evidence already recorded.
func fillSelfCorrectionDelivery(base, rec SelfCorrectionCandidateRecord) SelfCorrectionCandidateRecord {
	if base.Branch == "" && rec.Branch != "" {
		base.Branch = rec.Branch
	}
	if base.PRNumber == 0 && rec.PRNumber > 0 {
		base.PRNumber = rec.PRNumber
	}
	if base.PRURL == "" && rec.PRURL != "" {
		base.PRURL = rec.PRURL
	}
	if base.CommitSHA == "" && rec.CommitSHA != "" {
		base.CommitSHA = rec.CommitSHA
	}
	if base.DeployHead == "" && rec.DeployHead != "" {
		base.DeployHead = rec.DeployHead
	}
	if base.DispatchProcedureRef == "" && rec.DispatchProcedureRef != "" {
		base.DispatchProcedureRef = rec.DispatchProcedureRef
	}
	return base
}

// normalizedSelfCorrectionCandidate normalizes a candidate (or legacy) row
// before it seeds the merged map.
func normalizedSelfCorrectionCandidate(rec SelfCorrectionCandidateRecord) SelfCorrectionCandidateRecord {
	rec.Type = selfCorrectionTypeCandidate
	rec.Status = normalizeSelfCorrectionStatus(rec.Status)
	if rec.Status == "" {
		rec.Status = SelfCorrectionStatusProposed
	}
	if rec.UpdatedAt == 0 {
		rec.UpdatedAt = rec.CreatedAt
	}
	return rec
}

func validSelfCorrectionStatusTransition(from, to string) bool {
	return rsilifecycle.CanReviewTransition(
		rsilifecycle.ReviewState(from),
		rsilifecycle.ReviewState(to),
	)
}

func normalizeSelfCorrectionDispatchPhase(phase string) string {
	return string(rsilifecycle.NormalizeDelivery(phase))
}

func validSelfCorrectionDispatchTransition(from, to string) bool {
	return rsilifecycle.CanDeliveryTransition(
		rsilifecycle.DeliveryPhase(from),
		rsilifecycle.DeliveryPhase(to),
	)
}

func normalizeSelfCorrectionStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return ""
	}
	return string(rsilifecycle.NormalizeReview(status))
}

func normalizeSelfCorrectionScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "skill", "code", "prompt", "docs", "ops", "config", "test":
		return strings.ToLower(strings.TrimSpace(scope))
	default:
		if strings.TrimSpace(scope) == "" {
			return "code"
		}
		return "other"
	}
}

func cleanSelfCorrectionStrings(values []string, limit int) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func makeSelfCorrectionID(record SelfCorrectionCandidateRecord) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(record.Scope))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(record.SkillName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(record.Title))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(record.Candidate))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(record.ProposedChange))
	return fmt.Sprintf("sc-%d-%08x", record.CreatedAt, h.Sum32())
}

func sortSelfCorrectionCandidates(items []SelfCorrectionCandidateRecord) {
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i].UpdatedAt, items[j].UpdatedAt
		if left == 0 {
			left = items[i].CreatedAt
		}
		if right == 0 {
			right = items[j].CreatedAt
		}
		if left == right {
			return items[i].ID > items[j].ID
		}
		return left > right
	})
}
