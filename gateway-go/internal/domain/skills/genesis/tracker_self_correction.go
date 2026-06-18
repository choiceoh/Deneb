package genesis

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	SelfCorrectionTypeCandidate = "self_correction_candidate"
	SelfCorrectionTypeReview    = "self_correction_review"

	SelfCorrectionStatusProposed   = "proposed"
	SelfCorrectionStatusAccepted   = "accepted"
	SelfCorrectionStatusRejected   = "rejected"
	SelfCorrectionStatusSuperseded = "superseded"
	SelfCorrectionStatusApplied    = "applied"
)

// SelfCorrectionCandidateRecord is an append-only proposal for a future coding
// agent to review. It deliberately does not apply anything; the value is in
// preserving the model's observation, evidence, and risk note until a batch
// review can accept/reject it with tests.
type SelfCorrectionCandidateRecord struct {
	Type           string   `json:"type"`
	ID             string   `json:"id"`
	Status         string   `json:"status,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	SkillName      string   `json:"skillName,omitempty"`
	SessionKey     string   `json:"sessionKey,omitempty"`
	Title          string   `json:"title,omitempty"`
	Candidate      string   `json:"candidate,omitempty"`
	Evidence       string   `json:"evidence,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	TargetFiles    []string `json:"targetFiles,omitempty"`
	ProposedChange string   `json:"proposedChange,omitempty"`
	Risk           string   `json:"risk,omitempty"`
	Source         string   `json:"source,omitempty"`
	Reviewer       string   `json:"reviewer,omitempty"`
	ReviewNote     string   `json:"reviewNote,omitempty"`
	CreatedAt      int64    `json:"createdAt"`
	UpdatedAt      int64    `json:"updatedAt,omitempty"`
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
	found := false
	for _, existing := range entries {
		if existing.Type == SelfCorrectionTypeReview {
			continue
		}
		if strings.TrimSpace(existing.ID) == record.ID {
			found = true
			break
		}
	}
	if !found {
		return record, fmt.Errorf("genesis-tracker: self-correction candidate not found: %s", record.ID)
	}
	if err := jsonlstore.Append(t.selfCorrectionPath, record); err != nil {
		return record, fmt.Errorf("genesis-tracker: append self-correction review: %w", err)
	}
	return record, nil
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
		default:
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
