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

// RecordSkillOpportunity appends one observed Propus signal.
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
	return jsonlstore.Append(t.opportunityPath, record)
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
