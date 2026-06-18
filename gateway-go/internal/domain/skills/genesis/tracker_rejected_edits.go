package genesis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// RejectedSkillEditRecord is the SkillOpt-style rejected-edit buffer for skill
// evolution. A failed candidate is not just discarded; the next optimizer pass
// can read why it failed and avoid repeating the same mutation.
type RejectedSkillEditRecord struct {
	SkillName        string            `json:"skillName"`
	Reason           string            `json:"reason"`
	CandidateBody    string            `json:"candidateBody,omitempty"`
	Source           string            `json:"source,omitempty"`
	SelfHarnessAudit *HarnessEditAudit `json:"selfHarnessAudit,omitempty"`
	CreatedAt        int64             `json:"createdAt"`
}

// RecordRejectedSkillEdit appends a failed skill-evolution candidate to the
// rejected-edit buffer. The candidate body is bounded so one bad rewrite cannot
// bloat the state sidecar or future prompts.
func (t *Tracker) RecordRejectedSkillEdit(record RejectedSkillEditRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record.SkillName = strings.TrimSpace(record.SkillName)
	record.Reason = strings.TrimSpace(record.Reason)
	record.Source = strings.TrimSpace(record.Source)
	record.CandidateBody = strings.TrimSpace(truncateRunes(record.CandidateBody, 1997))
	if record.SelfHarnessAudit != nil && record.SelfHarnessAudit.empty() {
		record.SelfHarnessAudit = nil
	}
	if record.SkillName == "" {
		return fmt.Errorf("genesis-tracker: rejected edit skillName is required")
	}
	if record.Reason == "" {
		record.Reason = "rejected"
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	if err := jsonlstore.Append(t.rejectedPath, record); err != nil {
		return fmt.Errorf("genesis-tracker: append rejected edit: %w", err)
	}
	return nil
}

// RecentRejectedSkillEdits returns rejected evolution candidates newest first.
// When skillName is empty, it returns recent rejected edits across all skills.
func (t *Tracker) RecentRejectedSkillEdits(skillName string, limit int) ([]RejectedSkillEditRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limit <= 0 {
		limit = 5
	}
	filter := strings.TrimSpace(skillName)
	entries, err := jsonlstore.Load[RejectedSkillEditRecord](t.rejectedPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load rejected edits: %w", err)
	}
	out := make([]RejectedSkillEditRecord, 0, min(limit, len(entries)))
	for i := len(entries) - 1; i >= 0; i-- {
		rec := entries[i]
		if filter != "" && rec.SkillName != filter {
			continue
		}
		out = append(out, rec)
	}
	fallback, err := t.rejectedSkillEditsFromLifecycleLocked(filter)
	if err != nil {
		return nil, err
	}
	out = append(out, fallback...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	out = dedupeRejectedSkillEdits(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (t *Tracker) rejectedSkillEditsFromLifecycleLocked(filter string) ([]RejectedSkillEditRecord, error) {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load lifecycle rejected edits: %w", err)
	}
	out := make([]RejectedSkillEditRecord, 0)
	for _, entry := range entries {
		if entry.Type != "evolve_rejected" {
			continue
		}
		skillName := strings.TrimSpace(entry.SkillName)
		if skillName == "" || (filter != "" && skillName != filter) {
			continue
		}
		reason := strings.TrimSpace(entry.Reason)
		if reason == "" {
			reason = "rejected"
		}
		out = append(out, RejectedSkillEditRecord{
			SkillName:        skillName,
			Reason:           reason,
			Source:           "lifecycle-fallback",
			SelfHarnessAudit: entry.SelfHarnessAudit,
			CreatedAt:        entry.CreatedAt,
		})
	}
	return out, nil
}

func dedupeRejectedSkillEdits(records []RejectedSkillEditRecord) []RejectedSkillEditRecord {
	seen := make(map[string]bool, len(records))
	out := records[:0]
	for _, rec := range records {
		key := strings.TrimSpace(rec.SkillName) + "\x00" + strings.TrimSpace(rec.Reason)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rec)
	}
	return out
}
