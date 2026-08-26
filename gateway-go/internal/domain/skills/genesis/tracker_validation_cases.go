package genesis

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strings"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// ErrWeakAutomaticValidationCase reports an automatic validation case without sufficient evidence.
var ErrWeakAutomaticValidationCase = errors.New("weak automatic validation case")

// SkillValidationCaseRecord is a lightweight held-out selection fixture for a
// skill. It does not replay a full agent session yet; it encodes invariants
// distilled from real failures so candidate skill bodies can be rejected if
// they regress known requirements before they reach the LLM judge.
type SkillValidationCaseRecord struct {
	SkillName           string                `json:"skillName"`
	ID                  string                `json:"id,omitempty"`
	Description         string                `json:"description,omitempty"`
	FrontierTier        string                `json:"frontierTier,omitempty"`
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
	UniqueEasyAnchorCases    int    `json:"uniqueEasyAnchorCases"`
	UniqueMixedFrontierCases int    `json:"uniqueMixedFrontierCases"`
	UniqueHardFrontierCases  int    `json:"uniqueHardFrontierCases"`
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
	record.ID = strings.TrimSpace(genesiscommon.TruncateRunes(record.ID, 120))
	record.Description = strings.TrimSpace(genesiscommon.TruncateRunes(record.Description, 400))
	record.FrontierTier = normalizedValidationFrontierTier(record.FrontierTier)
	record.Source = strings.TrimSpace(genesiscommon.TruncateRunes(record.Source, 120))
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
	return t.recentValidationCases(skillName, limit, nil)
}

// validationCaseBlindHeldOut deterministically partitions cases into a visible
// "contract" pool (may be shown to producer/judge/teacher prompts as the
// behavior contract to preserve) and a blind held-out pool (scored by the
// selection/behavior gates only), so a candidate cannot satisfy the gate by
// echoing assertions it was shown (SkillHone-style split,
// docs/research/skillhone-2606.08671.md §3-1). Hashing the stable dedupe
// identity means a case never migrates pools across runs. Roughly 2/3 of cases
// land blind: gate integrity is worth more than contract visibility.
func validationCaseBlindHeldOut(rec SkillValidationCaseRecord) bool {
	h := fnv.New32a()
	_, _ = io.WriteString(h, validationCaseDedupeKey(rec))
	return h.Sum32()%3 != 0
}

// isCharterCase reports whether a validation case belongs to the FROZEN
// charter subset (P3 precondition #3, SkillAudit 2606.14239): a deterministic
// ~25% slice of the corpus, hashed on the same stable dedupe identity as the
// pool split but on an independent salt, that verifier co-evolution MUST
// EXCLUDE from its training/mutation surface. The charter is the structural
// hedge against false-accept drift: whatever the co-evolved judge learns, this
// slice keeps measuring it with cases it never touched. Frozen from birth —
// membership never migrates.
//
// CONTRACT for P3 implementers: any path that mutates, regenerates, or feeds
// validation cases into judge/producer evolution must skip records where
// isCharterCase is true; benches and gates may still SCORE against them.
func isCharterCase(rec SkillValidationCaseRecord) bool {
	h := fnv.New32a()
	_, _ = io.WriteString(h, "charter|")
	_, _ = io.WriteString(h, validationCaseDedupeKey(rec))
	return h.Sum32()%4 == 0
}

// excludeCharterCases drops the frozen charter slice from a case set bound for
// a verifier co-evolution TRAINING surface (false-reject mining, few-shot
// exhibit selection). Scoring/bench paths keep the full corpus — only training
// inputs are filtered, which is the whole point of the held-out charter.
func excludeCharterCases(cases []SkillValidationCaseRecord) []SkillValidationCaseRecord {
	out := cases[:0:0]
	for _, c := range cases {
		if isCharterCase(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// recentSkillValidationCasesPool is RecentSkillValidationCases restricted to
// one partition pool: blind=true → held-out gate pool, blind=false → visible
// contract pool.
func (t *Tracker) recentSkillValidationCasesPool(skillName string, limit int, blind bool) ([]SkillValidationCaseRecord, error) {
	return t.recentValidationCases(skillName, limit, func(rec SkillValidationCaseRecord) bool {
		return validationCaseBlindHeldOut(rec) == blind
	})
}

func (t *Tracker) recentValidationCases(skillName string, limit int, keep func(SkillValidationCaseRecord) bool) ([]SkillValidationCaseRecord, error) {
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
		if keep != nil && !keep(rec) {
			continue
		}
		key := validationCaseDedupeKey(rec)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rec)
	}
	return reserveRealUseCase(entries, filter, keep, seen, out, limit), nil
}

// isRealUseValidationCase reports whether a case was minted from an actual
// skill invocation rather than synthesized by a coverage/curriculum lane.
// Only these carry evidence of how the skill behaves against live input.
func isRealUseValidationCase(rec SkillValidationCaseRecord) bool {
	switch strings.ToLower(strings.TrimSpace(rec.Source)) {
	case "auto-failed-skill-use", "auto-successful-skill-use":
		return true
	default:
		return false
	}
}

// reserveRealUseCase keeps one real-use case inside a recency window that
// synthetic lanes would otherwise flush.
//
// The window is small (5) and strictly newest-first, while adversarial and
// curriculum lanes mint cases continuously — so a real-use case ages out within
// days of being recorded. Measured 2026-08-26: of the 7 skills holding any
// real-use case, 3 had it already evicted, and the corpus was 1.9% real-use
// overall. A candidate scored only against synthesized cases ties its original,
// which is exactly the "did not improve (5.9 vs 5.9)" rejection that stalled
// acceptance. Trading the oldest synthetic slot for the newest real one costs
// nothing when no real-use case exists.
func reserveRealUseCase(
	entries []SkillValidationCaseRecord,
	filter string,
	keep func(SkillValidationCaseRecord) bool,
	seen map[string]struct{},
	out []SkillValidationCaseRecord,
	limit int,
) []SkillValidationCaseRecord {
	if limit <= 1 || len(out) == 0 {
		return out
	}
	for _, rec := range out {
		if isRealUseValidationCase(rec) {
			return out
		}
	}
	for i := len(entries) - 1; i >= 0; i-- {
		rec := entries[i]
		if filter != "" && rec.SkillName != filter {
			continue
		}
		if !isRealUseValidationCase(rec) {
			continue
		}
		if keep != nil && !keep(rec) {
			continue
		}
		if _, ok := seen[validationCaseDedupeKey(rec)]; ok {
			return out
		}
		if len(out) < limit {
			return append(out, rec)
		}
		// Displace the oldest synthetic case, keeping recency order intact.
		out[len(out)-1] = rec
		return out
	}
	return out
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
		switch normalizedValidationFrontierTier(rec.FrontierTier) {
		case "easy":
			summary.UniqueEasyAnchorCases++
		case "mixed":
			summary.UniqueMixedFrontierCases++
		case "hard":
			summary.UniqueHardFrontierCases++
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
		FrontierTier        string                `json:"frontierTier,omitempty"`
		RequiredSubstrings  []string              `json:"requiredSubstrings,omitempty"`
		ForbiddenSubstrings []string              `json:"forbiddenSubstrings,omitempty"`
		RequiredHeadings    []string              `json:"requiredHeadings,omitempty"`
		Replay              SkillReplayCaseRecord `json:"replay,omitempty"`
	}{
		SkillName:           skillName,
		FrontierTier:        normalizedValidationFrontierTier(rec.FrontierTier),
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

func normalizedValidationFrontierTier(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "easy", "easy_anchor", "easy-anchor", "anchor":
		return "easy"
	case "mixed", "mixed_frontier", "mixed-frontier", "frontier":
		return "mixed"
	case "hard", "hard_frontier", "hard-frontier":
		return "hard"
	default:
		return ""
	}
}

func isAutomaticValidationCase(rec SkillValidationCaseRecord) bool {
	src := strings.ToLower(strings.TrimSpace(rec.Source))
	switch src {
	case "review-session", "review-finding", "self-review", "session-backfill",
		"auto-failed-skill-use", "auto-successful-skill-use", "auto-backfill-lane":
		return true
	default:
		// Any auto-* or review* source is machine-authored and must face the
		// weak-case guard; only operator-entered cases skip it.
		return strings.HasPrefix(src, "review") || strings.HasPrefix(src, "auto-")
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
	replay.Input = strings.TrimSpace(genesiscommon.TruncateRunes(replay.Input, 1000))
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
		call.Name = strings.TrimSpace(genesiscommon.TruncateRunes(call.Name, 120))
		call.InputIncludes = cleanValidationStrings(call.InputIncludes)
		call.InputExcludes = cleanValidationStrings(call.InputExcludes)
		call.FixtureOutput = strings.TrimSpace(genesiscommon.TruncateRunes(call.FixtureOutput, 2000))
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
		value = strings.TrimSpace(genesiscommon.TruncateRunes(value, 300))
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

// RecentRealUseSessionsBySkill returns, per skill, the newest-first distinct
// session keys of real uses inside window, capped at perSkill. The autonomous
// backfill lane replays these transcripts into held-out validation cases so
// corpus growth no longer depends on an LLM turn remembering to call
// validation_backfill.
func (t *Tracker) RecentRealUseSessionsBySkill(window time.Duration, perSkill int) map[string][]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if perSkill <= 0 {
		perSkill = 1
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	records, err := jsonlstore.Load[UsageRecord](t.usagePath)
	if err != nil {
		return nil
	}
	type use struct {
		key string
		at  int64
	}
	bySkill := map[string][]use{}
	for _, r := range records {
		if r.UsedAt < cutoff || !isRealUsageRecord(r) {
			continue
		}
		key := strings.TrimSpace(r.SessionKey)
		name := strings.TrimSpace(r.SkillName)
		if key == "" || name == "" {
			continue
		}
		bySkill[name] = append(bySkill[name], use{key: key, at: r.UsedAt})
	}
	out := map[string][]string{}
	for name, uses := range bySkill {
		sort.Slice(uses, func(i, j int) bool { return uses[i].at > uses[j].at })
		seen := map[string]bool{}
		for _, u := range uses {
			if seen[u.key] {
				continue
			}
			seen[u.key] = true
			out[name] = append(out[name], u.key)
			if len(out[name]) >= perSkill {
				break
			}
		}
	}
	return out
}
