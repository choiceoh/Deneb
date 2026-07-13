package genesis

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/surfaces"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	SelfCorrectionTypeCandidate = "self_correction_candidate"
	SelfCorrectionTypeReview    = "self_correction_review"
	SelfCorrectionTypeDispatch  = "self_correction_dispatch"

	SelfCorrectionStatusProposed   = "proposed"
	SelfCorrectionStatusAccepted   = "accepted"
	SelfCorrectionStatusRejected   = "rejected"
	SelfCorrectionStatusSuperseded = "superseded"
	SelfCorrectionStatusApplied    = "applied"

	SelfCorrectionDispatchStarted     = "started"
	SelfCorrectionDispatchPROpened    = "pr_opened"
	SelfCorrectionDispatchMerged      = "merged"
	SelfCorrectionDispatchDeployed    = "deployed"
	SelfCorrectionDispatchWatchPassed = "watch_passed"
	SelfCorrectionDispatchFailed      = "failed"
	SelfCorrectionDispatchRolledBack  = "rolled_back"
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
	Reviewer       string `json:"reviewer,omitempty"`
	ReviewNote     string `json:"reviewNote,omitempty"`
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
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt,omitempty"`
}

// RecordSelfCorrectionCandidate appends a deferred self-correction candidate.
func (t *Tracker) RecordSelfCorrectionCandidate(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixMilli()
	record.Type = SelfCorrectionTypeCandidate
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
	record.SessionKey = strings.TrimSpace(record.SessionKey)
	record.SkillName = strings.TrimSpace(record.SkillName)
	record.TargetFiles = cleanSelfCorrectionStrings(record.TargetFiles, 20)
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
func (t *Tracker) RecordSelfCorrectionReview(record SelfCorrectionCandidateRecord) (SelfCorrectionCandidateRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	record.Type = SelfCorrectionTypeReview
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
	entries, err := jsonlstore.Load[SelfCorrectionCandidateRecord](t.selfCorrectionPath)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}
	current, found := mergedSelfCorrectionCandidate(entries, record.ID)
	if !found {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate not found: %s", record.ID)
	}
	if current.Status == record.Status {
		return record, nil // idempotent retry: do not inflate the append-only funnel
	}
	if !validSelfCorrectionStatusTransition(current.Status, record.Status) {
		return record, fmt.Errorf("genesis-tracker: invalid self-correction status transition %s -> %s", current.Status, record.Status)
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
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now().UnixMilli()
	record.Type = SelfCorrectionTypeDispatch
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

	entries, err := jsonlstore.Load[SelfCorrectionCandidateRecord](t.selfCorrectionPath)
	if err != nil {
		return record, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}
	current, found := mergedSelfCorrectionCandidate(entries, record.ID)
	if !found {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate not found: %s", record.ID)
	}
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
	if current.DispatchPhase != "" && record.DispatchPhase != SelfCorrectionDispatchStarted && current.AttemptID != record.AttemptID {
		return record, fmt.Errorf("genesis-tracker: dispatch attempt changed inside lifecycle: %s -> %s", current.AttemptID, record.AttemptID)
	}
	if record.DispatchPhase == SelfCorrectionDispatchStarted && current.AttemptID == record.AttemptID {
		return record, fmt.Errorf("genesis-tracker: retry must use a new dispatch attemptId")
	}
	if record.DispatchPhase == SelfCorrectionDispatchWatchPassed && current.Status != SelfCorrectionStatusApplied &&
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
	entries, err := jsonlstore.Load[SelfCorrectionCandidateRecord](t.selfCorrectionPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load self-correction candidates: %w", err)
	}

	merged := mergeSelfCorrectionRecords(entries)

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

func mergedSelfCorrectionCandidate(entries []SelfCorrectionCandidateRecord, id string) (SelfCorrectionCandidateRecord, bool) {
	rec, ok := mergeSelfCorrectionRecords(entries)[strings.TrimSpace(id)]
	return rec, ok
}

func mergeSelfCorrectionRecords(entries []SelfCorrectionCandidateRecord) map[string]SelfCorrectionCandidateRecord {
	merged := make(map[string]SelfCorrectionCandidateRecord)
	for _, rec := range entries {
		rec.ID = strings.TrimSpace(rec.ID)
		if rec.ID == "" {
			continue
		}
		switch rec.Type {
		case SelfCorrectionTypeReview:
			base, ok := merged[rec.ID]
			if !ok {
				continue
			}
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
			merged[rec.ID] = base
		case SelfCorrectionTypeDispatch:
			base, ok := merged[rec.ID]
			if !ok {
				continue
			}
			phase := normalizeSelfCorrectionDispatchPhase(rec.DispatchPhase)
			newAttempt := phase == SelfCorrectionDispatchStarted && rec.AttemptID != "" &&
				base.AttemptID != "" && rec.AttemptID != base.AttemptID
			if newAttempt {
				base.Branch = ""
				base.PRNumber = 0
				base.PRURL = ""
				base.CommitSHA = ""
				base.DeployHead = ""
				base.OutcomeNote = ""
			}
			samePhase := base.DispatchPhase == phase && base.AttemptID == rec.AttemptID
			base.DispatchPhase = phase
			if rec.AttemptID != "" {
				base.AttemptID = rec.AttemptID
			}
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
			if rec.OutcomeNote != "" && (!samePhase || base.OutcomeNote == "") {
				base.OutcomeNote = rec.OutcomeNote
			}
			if base.DispatchPhase == SelfCorrectionDispatchWatchPassed {
				base.Status = SelfCorrectionStatusApplied
				base.Reviewer = "deploy-watch"
				if base.ReviewNote == "" {
					base.ReviewNote = "merged deployment survived rollback watch"
				}
			}
			if rec.UpdatedAt > 0 {
				base.UpdatedAt = rec.UpdatedAt
			}
			merged[rec.ID] = base
		case "", SelfCorrectionTypeCandidate: // empty type is a legacy candidate row
			rec.Type = SelfCorrectionTypeCandidate
			rec.Status = normalizeSelfCorrectionStatus(rec.Status)
			if rec.Status == "" {
				rec.Status = SelfCorrectionStatusProposed
			}
			if rec.UpdatedAt == 0 {
				rec.UpdatedAt = rec.CreatedAt
			}
			merged[rec.ID] = rec
		}
	}
	return merged
}

func validSelfCorrectionStatusTransition(from, to string) bool {
	from = normalizeSelfCorrectionStatus(from)
	to = normalizeSelfCorrectionStatus(to)
	switch from {
	case SelfCorrectionStatusProposed:
		return to == SelfCorrectionStatusAccepted || to == SelfCorrectionStatusRejected ||
			to == SelfCorrectionStatusSuperseded || to == SelfCorrectionStatusApplied
	case SelfCorrectionStatusAccepted:
		return to == SelfCorrectionStatusRejected || to == SelfCorrectionStatusSuperseded || to == SelfCorrectionStatusApplied
	default:
		return false // rejected/superseded/applied are terminal
	}
}

func normalizeSelfCorrectionDispatchPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case SelfCorrectionDispatchStarted, SelfCorrectionDispatchPROpened,
		SelfCorrectionDispatchMerged, SelfCorrectionDispatchDeployed,
		SelfCorrectionDispatchWatchPassed, SelfCorrectionDispatchFailed,
		SelfCorrectionDispatchRolledBack:
		return strings.ToLower(strings.TrimSpace(phase))
	default:
		return ""
	}
}

func validSelfCorrectionDispatchTransition(from, to string) bool {
	from = normalizeSelfCorrectionDispatchPhase(from)
	to = normalizeSelfCorrectionDispatchPhase(to)
	switch from {
	case "":
		return to == SelfCorrectionDispatchStarted
	case SelfCorrectionDispatchStarted:
		return to == SelfCorrectionDispatchPROpened || to == SelfCorrectionDispatchMerged || to == SelfCorrectionDispatchFailed
	case SelfCorrectionDispatchPROpened:
		return to == SelfCorrectionDispatchMerged || to == SelfCorrectionDispatchFailed
	case SelfCorrectionDispatchMerged:
		return to == SelfCorrectionDispatchDeployed || to == SelfCorrectionDispatchFailed
	case SelfCorrectionDispatchDeployed:
		return to == SelfCorrectionDispatchWatchPassed || to == SelfCorrectionDispatchRolledBack
	case SelfCorrectionDispatchFailed:
		// A session can exit before GitHub exposes the PR. Late reconciliation
		// may promote that same attempt from failed to its observed PR state.
		return to == SelfCorrectionDispatchStarted || to == SelfCorrectionDispatchPROpened || to == SelfCorrectionDispatchMerged
	case SelfCorrectionDispatchRolledBack:
		return to == SelfCorrectionDispatchStarted
	default:
		return false // watch_passed is terminal
	}
}

func normalizeSelfCorrectionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "proposed", "open":
		if strings.TrimSpace(status) == "" {
			return ""
		}
		return SelfCorrectionStatusProposed
	case "accept", "accepted":
		return SelfCorrectionStatusAccepted
	case "reject", "rejected":
		return SelfCorrectionStatusRejected
	case "supersede", "superseded":
		return SelfCorrectionStatusSuperseded
	case "apply", "applied":
		return SelfCorrectionStatusApplied
	default:
		return ""
	}
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
