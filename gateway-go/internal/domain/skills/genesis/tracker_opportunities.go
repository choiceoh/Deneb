package genesis

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// SkillOpportunityRecord is a lightweight backlog of Propus signals.
// Unlike lifecycle logs, this is not only an audit trail of what happened; it is
// fed back into future background reviews so weak no-op/near-miss proposals can
// accumulate into a confident genesis/evolve route.
type SkillOpportunityRecord struct {
	Type       string `json:"type,omitempty"`
	Candidate  string `json:"candidate,omitempty"`
	Route      string `json:"route,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
	SkillName  string `json:"skillName,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Executed   bool   `json:"executed,omitempty"`
	Source     string `json:"source,omitempty"`
	CreatedAt  int64  `json:"createdAt,omitempty"`
}

// noOpEchoWindow collapses repeated no-op proposals of the SAME candidate
// pattern into one backlog entry per window. The reviewer re-encounters a
// rejected pattern on every nudger fire (live 2026-07-06: "Technology research
// via web" was re-proposed and re-no-op'd review after review, each appending
// another record). Recurrence ACROSS days still accumulates — the backlog's
// weak-signals-add-up design keeps its frequency signal, just at day
// granularity instead of per-review-tick. The lifecycle log (LogEvolutionProposal)
// still records every event, so the audit trail is unaffected.
const noOpEchoWindow = 48 * time.Hour

// RecordSkillOpportunity appends one observed Propus signal. A no-op proposal
// whose candidate pattern was already no-op'd within noOpEchoWindow is dropped
// as an echo (nil error, nothing appended).
func (t *Tracker) RecordSkillOpportunity(record SkillOpportunityRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if record.Type == "" {
		record.Type = "skill_opportunity"
	}
	if record.Source == "" {
		record.Source = "proposal"
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	if strings.TrimSpace(record.Route) == "" {
		record.Route = "no-op"
	}
	if record.Route == "no-op" && t.recentNoOpEchoLocked(record) {
		return nil
	}
	return jsonlstore.Append(t.opportunityPath, record)
}

// recentNoOpEchoLocked reports whether an equivalent no-op candidate was
// already recorded within noOpEchoWindow. Caller holds t.mu. Load-per-append
// is fine: the backlog is small (tens of KB) and appends arrive per review,
// not per turn. Fail-open — an unreadable backlog never blocks recording.
func (t *Tracker) recentNoOpEchoLocked(record SkillOpportunityRecord) bool {
	key := opportunityPatternKey(record.Candidate)
	if key == "" {
		return false
	}
	records, err := jsonlstore.Load[SkillOpportunityRecord](t.opportunityPath)
	if err != nil {
		return false
	}
	cutoff := record.CreatedAt - noOpEchoWindow.Milliseconds()
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.CreatedAt < cutoff {
			break // append-ordered file — everything earlier is older
		}
		if r.Route == "no-op" && opportunityPatternKey(r.Candidate) == key {
			return true
		}
	}
	return false
}

// opportunityPatternKey normalizes a candidate description to a comparison
// key. Candidates are LLM prose that varies in the tail but keeps a stable
// "pattern name" head (observed repeats all share "Technology research via
// web: …"), so the head before the first colon is the key; without a usable
// colon head, the first 80 normalized runes stand in.
func opportunityPatternKey(candidate string) string {
	s := strings.ToLower(strings.Join(strings.Fields(candidate), " "))
	if s == "" {
		return ""
	}
	if head, _, ok := strings.Cut(s, ":"); ok {
		if h := strings.TrimSpace(head); len([]rune(h)) >= 12 {
			return h
		}
	}
	if r := []rune(s); len(r) > 80 {
		return string(r[:80])
	}
	return s
}

// RecentSkillOpportunities returns newest-first opportunity records, optionally
// filtered by related skill. skillName="" returns global recent signals.
func (t *Tracker) RecentSkillOpportunities(skillName string, limit int) ([]SkillOpportunityRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}
	records, err := jsonlstore.Load[SkillOpportunityRecord](t.opportunityPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load skill opportunities: %w", err)
	}
	filter := strings.TrimSpace(skillName)
	out := make([]SkillOpportunityRecord, 0, min(limit, len(records)))
	for i := len(records) - 1; i >= 0 && len(out) < limit; i-- {
		rec := records[i]
		if filter != "" && rec.SkillName != filter {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}
