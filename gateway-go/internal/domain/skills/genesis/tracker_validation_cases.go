package genesis

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

var ErrWeakAutomaticValidationCase = errors.New("weak automatic validation case")

// SkillValidationCaseRecord is a lightweight held-out selection fixture for a
// skill. It does not replay a full agent session yet; it encodes invariants
// distilled from real failures so candidate skill bodies can be rejected if
// they regress known requirements before they reach the LLM judge.
type SkillValidationCaseRecord struct {
	SkillName           string                `json:"skillName"`
	ID                  string                `json:"id,omitempty"`
	Description         string                `json:"description,omitempty"`
	RequiredSubstrings  []string              `json:"requiredSubstrings,omitempty"`
	ForbiddenSubstrings []string              `json:"forbiddenSubstrings,omitempty"`
	RequiredHeadings    []string              `json:"requiredHeadings,omitempty"`
	Replay              SkillReplayCaseRecord `json:"replay,omitempty"`
	Source              string                `json:"source,omitempty"`
	CreatedAt           int64                 `json:"createdAt"`
}

// SkillValidationCaseSummary is an operator-facing rollup for the held-out
// replay corpus. Raw records stay append-only, while UniqueRecords is the set
// that scoring/prompt consumers actually use after de-dupe.
type SkillValidationCaseSummary struct {
	SkillName                string `json:"skillName,omitempty"`
	RawRecords               int    `json:"rawRecords"`
	UniqueRecords            int    `json:"uniqueRecords"`
	DuplicateRecords         int    `json:"duplicateRecords"`
	AutomaticRecords         int    `json:"automaticRecords"`
	WeakAutomaticRecords     int    `json:"weakAutomaticRecords"`
	UniqueAutomaticRecords   int    `json:"uniqueAutomaticRecords"`
	UniqueWeakAutomaticCases int    `json:"uniqueWeakAutomaticCases"`
	SkillsWithCases          int    `json:"skillsWithCases,omitempty"`
	TopSkill                 string `json:"topSkill,omitempty"`
	TopSkillUniqueCases      int    `json:"topSkillUniqueCases,omitempty"`
	LastCaseAt               int64  `json:"lastCaseAt,omitempty"`
	LastAutomaticCaseAt      int64  `json:"lastAutomaticCaseAt,omitempty"`
	LastWeakAutomaticCaseAt  int64  `json:"lastWeakAutomaticCaseAt,omitempty"`
}

// SkillReplayCaseRecord is a deterministic dry-run fixture for a skill. It
// captures a realistic user task and the actions/tools the skill should make an
// agent choose, without executing external side effects during validation.
type SkillReplayCaseRecord struct {
	Input                 string                      `json:"input,omitempty"`
	Context               []string                    `json:"context,omitempty"`
	RequiredActions       []string                    `json:"requiredActions,omitempty"`
	ForbiddenActions      []string                    `json:"forbiddenActions,omitempty"`
	RequiredObservations  []string                    `json:"requiredObservations,omitempty"`
	ForbiddenObservations []string                    `json:"forbiddenObservations,omitempty"`
	RequiredTools         []string                    `json:"requiredTools,omitempty"`
	ForbiddenTools        []string                    `json:"forbiddenTools,omitempty"`
	ExpectedToolCalls     []SkillReplayToolCallRecord `json:"expectedToolCalls,omitempty"`
	ForbiddenToolCalls    []SkillReplayToolCallRecord `json:"forbiddenToolCalls,omitempty"`
	RequireOrder          bool                        `json:"requireOrder,omitempty"`
}

// SkillReplayToolCallRecord captures a tool invocation shape from a successful
// or forbidden replay trace. It intentionally stores substrings rather than full
// JSON args so validation can survive harmless formatting differences while
// still protecting the operationally important command/path/query fragments.
type SkillReplayToolCallRecord struct {
	Name          string   `json:"name,omitempty"`
	InputIncludes []string `json:"inputIncludes,omitempty"`
	InputExcludes []string `json:"inputExcludes,omitempty"`
	FixtureOutput string   `json:"fixtureOutput,omitempty"`
	FixtureError  bool     `json:"fixtureError,omitempty"`
}

// RecordSkillValidationCase appends a held-out validation invariant for a
// skill. At least one assertion is required; otherwise the case would only add
// noise to the selection gate.
func (t *Tracker) RecordSkillValidationCase(record SkillValidationCaseRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	record.SkillName = strings.TrimSpace(record.SkillName)
	record.ID = strings.TrimSpace(truncateRunes(record.ID, 120))
	record.Description = strings.TrimSpace(truncateRunes(record.Description, 400))
	record.Source = strings.TrimSpace(truncateRunes(record.Source, 120))
	record.RequiredSubstrings = cleanValidationStrings(record.RequiredSubstrings)
	record.ForbiddenSubstrings = cleanValidationStrings(record.ForbiddenSubstrings)
	record.RequiredHeadings = cleanValidationStrings(record.RequiredHeadings)
	record.Replay = cleanSkillReplayCase(record.Replay)
	if record.SkillName == "" {
		return fmt.Errorf("genesis-tracker: validation case skillName is required")
	}
	if isWeakAutomaticValidationCase(record) {
		t.recordEvolutionActivityLocked(SkillActivityValidationRejected, true, "")
		return fmt.Errorf("genesis-tracker: %w: needs a concrete action, observation, heading, substring, or tool input/output fragment", ErrWeakAutomaticValidationCase)
	}
	if !record.hasAssertions() {
		t.recordEvolutionActivityLocked(SkillActivityValidationRejected, true, "")
		return fmt.Errorf("genesis-tracker: validation case needs at least one assertion")
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	if err := jsonlstore.Append(t.validationPath, record); err != nil {
		return fmt.Errorf("genesis-tracker: append validation case: %w", err)
	}
	return nil
}

// RecentSkillValidationCases returns recent validation cases newest first,
// de-duplicated by stable case identity. The underlying JSONL stays append-only
// for auditability, but selection/prompt/status consumers must not overweight a
// replay case just because the background reviewer recorded the same session
// more than once.
func (t *Tracker) RecentSkillValidationCases(skillName string, limit int) ([]SkillValidationCaseRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if limit <= 0 {
		limit = 20
	}
	filter := strings.TrimSpace(skillName)
	entries, err := jsonlstore.Load[SkillValidationCaseRecord](t.validationPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load validation cases: %w", err)
	}
	out := make([]SkillValidationCaseRecord, 0, min(limit, len(entries)))
	seen := make(map[string]struct{}, len(entries))
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		rec := entries[i]
		if filter != "" && rec.SkillName != filter {
			continue
		}
		key := validationCaseDedupeKey(rec)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rec)
	}
	return out, nil
}

// ValidationCaseSummary reports raw vs unique case counts and weak automatic
// case pressure. It mirrors RecentSkillValidationCases' de-dupe identity so
// health/status output matches the scoring corpus.
func (t *Tracker) ValidationCaseSummary(skillName string) (SkillValidationCaseSummary, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	filter := strings.TrimSpace(skillName)
	entries, err := jsonlstore.Load[SkillValidationCaseRecord](t.validationPath)
	if err != nil {
		return SkillValidationCaseSummary{}, fmt.Errorf("genesis-tracker: load validation cases: %w", err)
	}
	summary := SkillValidationCaseSummary{SkillName: filter}
	seen := map[string]SkillValidationCaseRecord{}
	for _, rec := range entries {
		if filter != "" && rec.SkillName != filter {
			continue
		}
		summary.RawRecords++
		if rec.CreatedAt > summary.LastCaseAt {
			summary.LastCaseAt = rec.CreatedAt
		}
		auto := isAutomaticValidationCase(rec)
		weak := isWeakAutomaticValidationCase(rec)
		if auto {
			summary.AutomaticRecords++
			if rec.CreatedAt > summary.LastAutomaticCaseAt {
				summary.LastAutomaticCaseAt = rec.CreatedAt
			}
		}
		if weak {
			summary.WeakAutomaticRecords++
			if rec.CreatedAt > summary.LastWeakAutomaticCaseAt {
				summary.LastWeakAutomaticCaseAt = rec.CreatedAt
			}
		}
		key := validationCaseDedupeKey(rec)
		if prev, ok := seen[key]; !ok || rec.CreatedAt >= prev.CreatedAt {
			seen[key] = rec
		}
	}
	summary.UniqueRecords = len(seen)
	summary.DuplicateRecords = summary.RawRecords - summary.UniqueRecords
	bySkill := map[string]int{}
	for _, rec := range seen {
		if isAutomaticValidationCase(rec) {
			summary.UniqueAutomaticRecords++
		}
		if isWeakAutomaticValidationCase(rec) {
			summary.UniqueWeakAutomaticCases++
		}
		name := strings.TrimSpace(rec.SkillName)
		if name != "" {
			bySkill[name]++
			if bySkill[name] > summary.TopSkillUniqueCases {
				summary.TopSkill = name
				summary.TopSkillUniqueCases = bySkill[name]
			}
		}
	}
	if filter == "" {
		summary.SkillsWithCases = len(bySkill)
	}
	return summary, nil
}

func validationCaseDedupeKey(rec SkillValidationCaseRecord) string {
	skillName := normalizedValidationCaseKey(rec.SkillName)
	if id := normalizedValidationCaseKey(rec.ID); id != "" {
		return "id:" + skillName + "\x00" + id
	}
	payload := struct {
		SkillName           string                `json:"skillName"`
		RequiredSubstrings  []string              `json:"requiredSubstrings,omitempty"`
		ForbiddenSubstrings []string              `json:"forbiddenSubstrings,omitempty"`
		RequiredHeadings    []string              `json:"requiredHeadings,omitempty"`
		Replay              SkillReplayCaseRecord `json:"replay,omitempty"`
	}{
		SkillName:           skillName,
		RequiredSubstrings:  normalizedValidationCaseStrings(rec.RequiredSubstrings),
		ForbiddenSubstrings: normalizedValidationCaseStrings(rec.ForbiddenSubstrings),
		RequiredHeadings:    normalizedValidationCaseStrings(rec.RequiredHeadings),
		Replay:              normalizedReplayCaseForKey(rec.Replay),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "fallback:" + skillName
	}
	return "payload:" + string(data)
}

func normalizedReplayCaseForKey(replay SkillReplayCaseRecord) SkillReplayCaseRecord {
	return SkillReplayCaseRecord{
		Input:                 normalizedValidationCaseKey(replay.Input),
		Context:               normalizedValidationCaseStrings(replay.Context),
		RequiredActions:       normalizedValidationCaseStrings(replay.RequiredActions),
		ForbiddenActions:      normalizedValidationCaseStrings(replay.ForbiddenActions),
		RequiredObservations:  normalizedValidationCaseStrings(replay.RequiredObservations),
		ForbiddenObservations: normalizedValidationCaseStrings(replay.ForbiddenObservations),
		RequiredTools:         normalizedValidationCaseStrings(replay.RequiredTools),
		ForbiddenTools:        normalizedValidationCaseStrings(replay.ForbiddenTools),
		ExpectedToolCalls:     normalizedReplayToolCallsForKey(replay.ExpectedToolCalls),
		ForbiddenToolCalls:    normalizedReplayToolCallsForKey(replay.ForbiddenToolCalls),
		RequireOrder:          replay.RequireOrder,
	}
}

func normalizedReplayToolCallsForKey(calls []SkillReplayToolCallRecord) []SkillReplayToolCallRecord {
	out := make([]SkillReplayToolCallRecord, 0, len(calls))
	for _, call := range calls {
		out = append(out, SkillReplayToolCallRecord{
			Name:          normalizedValidationCaseKey(call.Name),
			InputIncludes: normalizedValidationCaseStrings(call.InputIncludes),
			InputExcludes: normalizedValidationCaseStrings(call.InputExcludes),
			FixtureOutput: normalizedValidationCaseKey(call.FixtureOutput),
			FixtureError:  call.FixtureError,
		})
	}
	return out
}

func normalizedValidationCaseStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizedValidationCaseKey(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizedValidationCaseKey(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func isAutomaticValidationCase(rec SkillValidationCaseRecord) bool {
	switch strings.ToLower(strings.TrimSpace(rec.Source)) {
	case "review-session", "review-finding", "self-review", "session-backfill", "auto-failed-skill-use":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(rec.Source)), "review")
	}
}

func isWeakAutomaticValidationCase(rec SkillValidationCaseRecord) bool {
	if !isAutomaticValidationCase(rec) {
		return false
	}
	if len(rec.RequiredSubstrings)+len(rec.ForbiddenSubstrings)+len(rec.RequiredHeadings) > 0 {
		return false
	}
	replay := rec.Replay
	if len(replay.RequiredActions)+len(replay.ForbiddenActions)+len(replay.RequiredObservations)+len(replay.ForbiddenObservations)+len(replay.ForbiddenTools) > 0 {
		return false
	}
	for _, call := range replay.ExpectedToolCalls {
		if len(call.InputIncludes)+len(call.InputExcludes) > 0 ||
			strings.TrimSpace(call.FixtureOutput) != "" ||
			call.FixtureError {
			return false
		}
	}
	for _, call := range replay.ForbiddenToolCalls {
		if strings.TrimSpace(call.Name) != "" ||
			len(call.InputIncludes)+len(call.InputExcludes) > 0 ||
			strings.TrimSpace(call.FixtureOutput) != "" ||
			call.FixtureError {
			return false
		}
	}
	return true
}

func (r SkillValidationCaseRecord) hasAssertions() bool {
	return len(r.RequiredSubstrings)+len(r.ForbiddenSubstrings)+len(r.RequiredHeadings) > 0 || r.Replay.hasAssertions()
}

func (r SkillReplayCaseRecord) hasAssertions() bool {
	return len(r.RequiredActions)+len(r.ForbiddenActions)+len(r.RequiredTools)+len(r.ForbiddenTools)+
		len(r.RequiredObservations)+len(r.ForbiddenObservations)+
		len(r.ExpectedToolCalls)+len(r.ForbiddenToolCalls) > 0
}

func cleanSkillReplayCase(replay SkillReplayCaseRecord) SkillReplayCaseRecord {
	replay.Input = strings.TrimSpace(truncateRunes(replay.Input, 1000))
	replay.Context = cleanValidationStrings(replay.Context)
	replay.RequiredActions = cleanValidationStrings(replay.RequiredActions)
	replay.ForbiddenActions = cleanValidationStrings(replay.ForbiddenActions)
	replay.RequiredObservations = cleanValidationStrings(replay.RequiredObservations)
	replay.ForbiddenObservations = cleanValidationStrings(replay.ForbiddenObservations)
	replay.RequiredTools = cleanValidationStrings(replay.RequiredTools)
	replay.ForbiddenTools = cleanValidationStrings(replay.ForbiddenTools)
	replay.ExpectedToolCalls = cleanSkillReplayToolCalls(replay.ExpectedToolCalls)
	replay.ForbiddenToolCalls = cleanSkillReplayToolCalls(replay.ForbiddenToolCalls)
	return replay
}

func cleanSkillReplayToolCalls(calls []SkillReplayToolCallRecord) []SkillReplayToolCallRecord {
	const maxReplayToolCalls = 20
	out := make([]SkillReplayToolCallRecord, 0, min(len(calls), maxReplayToolCalls))
	for _, call := range calls {
		call.Name = strings.TrimSpace(truncateRunes(call.Name, 120))
		call.InputIncludes = cleanValidationStrings(call.InputIncludes)
		call.InputExcludes = cleanValidationStrings(call.InputExcludes)
		call.FixtureOutput = strings.TrimSpace(truncateRunes(call.FixtureOutput, 2000))
		if call.Name == "" && len(call.InputIncludes)+len(call.InputExcludes) == 0 && call.FixtureOutput == "" {
			continue
		}
		out = append(out, call)
		if len(out) >= maxReplayToolCalls {
			break
		}
	}
	return out
}

func cleanValidationStrings(values []string) []string {
	const maxValidationStrings = 20
	out := make([]string, 0, min(len(values), maxValidationStrings))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(truncateRunes(value, 300))
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= maxValidationStrings {
			break
		}
	}
	return out
}
