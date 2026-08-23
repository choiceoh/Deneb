package genesis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

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
	// Infrastructure marks a row where the loop never actually judged the
	// candidate (judge call errored, teacher rewrite produced nothing). Such a
	// row is kept — it is the evidence trail that surfaced the outage in the
	// first place — but it is NOT a quality verdict, so rate metrics and the
	// "avoid repeating this mutation" feedback must exclude it. Classified at
	// write time by isInfrastructureRejection so readers never re-parse prose.
	Infrastructure   bool              `json:"infrastructure,omitempty"`
	SelfHarnessAudit *HarnessEditAudit `json:"selfHarnessAudit,omitempty"`
	CreatedAt        int64             `json:"createdAt"`
}

// rejectedEditBodyLimit bounds the stored candidate body. It must stay ABOVE
// real SKILL.md sizes (the largest bundled skill is ~14K runes): the body has a
// second consumer that SCORES it — mineFalseRejects (judge_accuracy.go) replays
// the rejected body through stored validation cases and flags a strict
// improvement as a suspected judge false reject, the only organic label source
// P3 verifier co-evolution has. A body chopped mid-document silently loses the
// assertions living in its tail, so the rejected candidate always under-scores
// and the lane can never fire. (2026-08-23: 45 of 47 buffered rows sat exactly
// at the old 1997-rune ceiling while judge false-reject labels stayed at 0 for
// 30 days.) The prompt consumer does its own 800-rune clamp at render time
// (formatRejectedSkillEdits), so nothing downstream depends on this being tight.
const rejectedEditBodyLimit = 20000

// RecordRejectedSkillEdit appends a failed skill-evolution candidate to the
// rejected-edit buffer. The candidate body is bounded so one bad rewrite cannot
// bloat the state sidecar.
func (t *Tracker) RecordRejectedSkillEdit(record RejectedSkillEditRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record.SkillName = strings.TrimSpace(record.SkillName)
	record.Reason = strings.TrimSpace(record.Reason)
	record.Source = strings.TrimSpace(record.Source)
	record.CandidateBody = strings.TrimSpace(genesiscommon.TruncateRunes(record.CandidateBody, rejectedEditBodyLimit))
	if record.SelfHarnessAudit != nil {
		audit := withHarnessDimensions(*record.SelfHarnessAudit)
		record.SelfHarnessAudit = audit.Ptr()
	}
	if record.SkillName == "" {
		return fmt.Errorf("genesis-tracker: rejected edit skillName is required")
	}
	if record.Reason == "" {
		record.Reason = "rejected"
	}
	record.Infrastructure = isInfrastructureRejection(record.Reason)
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
			SkillName: skillName,
			Reason:    reason,
			Source:    "lifecycle-fallback",
			// Classify on the way out too: rows reconstructed from the
			// lifecycle log predate the stored flag, and a reader that saw the
			// flag set on fresh rows but empty on historical ones would draw
			// the wrong trend.
			Infrastructure:   isInfrastructureRejection(reason),
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
