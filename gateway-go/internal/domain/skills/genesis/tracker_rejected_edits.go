package genesis

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// RejectedSkillEditRecord is the SkillOpt-style rejected-edit buffer for skill
// evolution. A failed candidate is not just discarded; the next optimizer pass
// can read why it failed and avoid repeating the same mutation.
type RejectedSkillEditRecord struct {
	SkillName     string `json:"skillName"`
	Reason        string `json:"reason"`
	CandidateBody string `json:"candidateBody,omitempty"`
	Source        string `json:"source,omitempty"`
	CreatedAt     int64  `json:"createdAt"`
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
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		rec := entries[i]
		if filter != "" && rec.SkillName != filter {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}
