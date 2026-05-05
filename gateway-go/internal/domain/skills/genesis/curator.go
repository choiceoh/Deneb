package genesis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const (
	SkillCuratorStateActive   = "active"
	SkillCuratorStateStale    = "stale"
	SkillCuratorStateArchived = "archived"

	SkillCuratorCreatedByAgent = "agent"

	defaultSkillCuratorIntervalHours = 24 * 7
	defaultSkillCuratorMinIdleHours  = 2
	defaultSkillCuratorStaleDays     = 30
	defaultSkillCuratorArchiveDays   = 90
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*SkillCuratorTask)(nil)

// SkillCuratorConfig controls the Hermes-style lifecycle window for generated
// skills. The curator only manages skills explicitly marked as agent-created.
type SkillCuratorConfig struct {
	IntervalHours    int `json:"intervalHours"`
	MinIdleHours     int `json:"minIdleHours"`
	StaleAfterDays   int `json:"staleAfterDays"`
	ArchiveAfterDays int `json:"archiveAfterDays"`
}

// SkillCuratorRecord is the persisted sidecar state for one managed skill.
type SkillCuratorRecord struct {
	SkillName     string `json:"skillName"`
	CreatedBy     string `json:"createdBy,omitempty"`
	State         string `json:"state"`
	Pinned        bool   `json:"pinned,omitempty"`
	UseCount      int    `json:"useCount,omitempty"`
	PatchCount    int    `json:"patchCount,omitempty"`
	CreatedAt     int64  `json:"createdAt,omitempty"`
	LastUsedAt    int64  `json:"lastUsedAt,omitempty"`
	LastPatchedAt int64  `json:"lastPatchedAt,omitempty"`
	ArchivedAt    int64  `json:"archivedAt,omitempty"`
}

// SkillCuratorState is the JSON sidecar stored next to genesis usage logs.
type SkillCuratorState struct {
	Version   int                           `json:"version"`
	UpdatedAt int64                         `json:"updatedAt"`
	Skills    map[string]SkillCuratorRecord `json:"skills"`
}

// SkillCuratorTransition describes one automatic state change.
type SkillCuratorTransition struct {
	SkillName string `json:"skillName"`
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason"`
}

// SkillCuratorSummary is returned by the periodic curator run.
type SkillCuratorSummary struct {
	Checked     int                      `json:"checked"`
	MarkedStale int                      `json:"markedStale,omitempty"`
	Archived    int                      `json:"archived,omitempty"`
	Reactivated int                      `json:"reactivated,omitempty"`
	Transitions []SkillCuratorTransition `json:"transitions,omitempty"`
}

// DefaultSkillCuratorConfig returns the production defaults copied from the
// proven Hermes pattern: weekly review, stale after 30 days, archived after 90.
func DefaultSkillCuratorConfig() SkillCuratorConfig {
	return SkillCuratorConfig{
		IntervalHours:    defaultSkillCuratorIntervalHours,
		MinIdleHours:     defaultSkillCuratorMinIdleHours,
		StaleAfterDays:   defaultSkillCuratorStaleDays,
		ArchiveAfterDays: defaultSkillCuratorArchiveDays,
	}
}

// SkillCuratorConfigFromEnv returns curator config with explicit env overrides.
func SkillCuratorConfigFromEnv() SkillCuratorConfig {
	cfg := DefaultSkillCuratorConfig()
	cfg.IntervalHours = envInt("DENEB_SKILL_CURATOR_INTERVAL_HOURS", cfg.IntervalHours)
	cfg.MinIdleHours = envInt("DENEB_SKILL_CURATOR_MIN_IDLE_HOURS", cfg.MinIdleHours)
	cfg.StaleAfterDays = envInt("DENEB_SKILL_CURATOR_STALE_DAYS", cfg.StaleAfterDays)
	cfg.ArchiveAfterDays = envInt("DENEB_SKILL_CURATOR_ARCHIVE_DAYS", cfg.ArchiveAfterDays)
	return cfg.withDefaults()
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

func (cfg SkillCuratorConfig) withDefaults() SkillCuratorConfig {
	def := DefaultSkillCuratorConfig()
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = def.IntervalHours
	}
	if cfg.MinIdleHours < 0 {
		cfg.MinIdleHours = def.MinIdleHours
	}
	if cfg.StaleAfterDays <= 0 {
		cfg.StaleAfterDays = def.StaleAfterDays
	}
	if cfg.ArchiveAfterDays <= 0 {
		cfg.ArchiveAfterDays = def.ArchiveAfterDays
	}
	if cfg.ArchiveAfterDays < cfg.StaleAfterDays {
		cfg.ArchiveAfterDays = cfg.StaleAfterDays
	}
	return cfg
}

// MarkSkillAgentCreated opts a generated skill into curator management.
func (t *Tracker) MarkSkillAgentCreated(skillName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.markSkillAgentCreatedLocked(skillName, time.Now().UnixMilli())
}

// MarkSkillPatched records a lifecycle-managed patch for an agent-created skill.
func (t *Tracker) MarkSkillPatched(skillName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.markSkillPatchedLocked(skillName, time.Now().UnixMilli())
}

// SetSkillPinned protects or unprotects a managed skill from stale/archive
// transitions. It creates a curator row only for already agent-created skills.
func (t *Tracker) SetSkillPinned(skillName string, pinned bool) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	name := strings.TrimSpace(skillName)
	if name == "" {
		return fmt.Errorf("skill curator: skillName is required")
	}
	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return err
	}
	rec, ok := state.Skills[name]
	if !ok || rec.CreatedBy != SkillCuratorCreatedByAgent {
		return nil
	}
	rec.Pinned = pinned
	state.Skills[name] = normalizeCuratorRecord(name, rec)
	return t.saveCuratorStateLocked(state)
}

// SetSkillCuratorState manually updates the curator state for an agent-created
// skill. It is state-only: skill files are never moved or deleted here.
func (t *Tracker) SetSkillCuratorState(skillName, nextState string) (SkillCuratorRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	name := strings.TrimSpace(skillName)
	if name == "" {
		return SkillCuratorRecord{}, fmt.Errorf("skill curator: skillName is required")
	}
	nextState = strings.TrimSpace(nextState)
	switch nextState {
	case SkillCuratorStateActive, SkillCuratorStateStale, SkillCuratorStateArchived:
	default:
		return SkillCuratorRecord{}, fmt.Errorf("skill curator: invalid state %q", nextState)
	}
	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return SkillCuratorRecord{}, err
	}
	rec, ok := state.Skills[name]
	if !ok || rec.CreatedBy != SkillCuratorCreatedByAgent {
		return SkillCuratorRecord{}, fmt.Errorf("skill curator: %q is not agent-created or is not curator-managed", name)
	}
	rec = normalizeCuratorRecord(name, rec)
	rec.State = nextState
	if nextState == SkillCuratorStateArchived {
		rec.ArchivedAt = time.Now().UnixMilli()
	} else {
		rec.ArchivedAt = 0
	}
	state.Skills[name] = rec
	if err := t.saveCuratorStateLocked(state); err != nil {
		return SkillCuratorRecord{}, err
	}
	return rec, nil
}

// SkillCuratorReport returns current curator state, optionally filtered by
// skill name. It is safe for agent-facing status output.
func (t *Tracker) SkillCuratorReport(skillName string) ([]SkillCuratorRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return nil, err
	}
	filter := strings.TrimSpace(skillName)
	records := make([]SkillCuratorRecord, 0, len(state.Skills))
	for name, rec := range state.Skills {
		rec = normalizeCuratorRecord(name, rec)
		if filter != "" && rec.SkillName != filter {
			continue
		}
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].State != records[j].State {
			return curatorStateRank(records[i].State) < curatorStateRank(records[j].State)
		}
		ai, aj := curatorLastActivity(records[i]), curatorLastActivity(records[j])
		if ai != aj {
			return ai > aj
		}
		return records[i].SkillName < records[j].SkillName
	})
	return records, nil
}

// ApplySkillCuratorTransitions updates active/stale/archive state for managed
// skills. It never deletes skill files and never touches user-authored skills.
func (t *Tracker) ApplySkillCuratorTransitions(now time.Time, cfg SkillCuratorConfig) (SkillCuratorSummary, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if now.IsZero() {
		now = time.Now()
	}
	cfg = cfg.withDefaults()
	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return SkillCuratorSummary{}, err
	}

	var summary SkillCuratorSummary
	changed := false
	minIdle := time.Duration(cfg.MinIdleHours) * time.Hour
	staleAfter := time.Duration(cfg.StaleAfterDays) * 24 * time.Hour
	archiveAfter := time.Duration(cfg.ArchiveAfterDays) * 24 * time.Hour

	for name, rec := range state.Skills {
		rec = normalizeCuratorRecord(name, rec)
		if rec.CreatedBy != SkillCuratorCreatedByAgent {
			continue
		}
		summary.Checked++
		if rec.Pinned || rec.State == SkillCuratorStateArchived {
			state.Skills[name] = rec
			continue
		}

		last := curatorLastActivity(rec)
		if last == 0 {
			last = rec.CreatedAt
		}
		idle := now.Sub(time.UnixMilli(last))
		if idle < minIdle {
			state.Skills[name] = rec
			continue
		}

		from := rec.State
		to := from
		reason := ""
		switch {
		case idle >= archiveAfter:
			to = SkillCuratorStateArchived
			rec.ArchivedAt = now.UnixMilli()
			reason = fmt.Sprintf("no use or patch for %d days", cfg.ArchiveAfterDays)
			summary.Archived++
		case idle >= staleAfter:
			to = SkillCuratorStateStale
			reason = fmt.Sprintf("no use or patch for %d days", cfg.StaleAfterDays)
			summary.MarkedStale++
		case from == SkillCuratorStateStale:
			to = SkillCuratorStateActive
			rec.ArchivedAt = 0
			reason = "recent use or patch reactivated the skill"
			summary.Reactivated++
		}
		if to != from {
			rec.State = to
			summary.Transitions = append(summary.Transitions, SkillCuratorTransition{
				SkillName: rec.SkillName,
				From:      from,
				To:        to,
				Reason:    reason,
			})
			changed = true
		}
		state.Skills[name] = rec
	}
	if changed {
		state.UpdatedAt = now.UnixMilli()
		if err := t.saveCuratorStateLocked(state); err != nil {
			return SkillCuratorSummary{}, err
		}
	}
	return summary, nil
}

func (t *Tracker) markSkillAgentCreatedLocked(skillName string, createdAt int64) error {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return nil
	}
	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return err
	}
	rec := state.Skills[name]
	rec.SkillName = name
	rec.CreatedBy = SkillCuratorCreatedByAgent
	if rec.CreatedAt == 0 {
		rec.CreatedAt = createdAt
	}
	rec.State = SkillCuratorStateActive
	rec.ArchivedAt = 0
	state.Skills[name] = normalizeCuratorRecord(name, rec)
	return t.saveCuratorStateLocked(state)
}

func (t *Tracker) markSkillPatchedLocked(skillName string, patchedAt int64) error {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return nil
	}
	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return err
	}
	rec, ok := state.Skills[name]
	if !ok || rec.CreatedBy != SkillCuratorCreatedByAgent {
		return nil
	}
	rec.PatchCount++
	rec.LastPatchedAt = patchedAt
	rec.State = SkillCuratorStateActive
	rec.ArchivedAt = 0
	state.Skills[name] = normalizeCuratorRecord(name, rec)
	return t.saveCuratorStateLocked(state)
}

func (t *Tracker) touchCuratorUsageLocked(skillName string, usedAt int64) error {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return nil
	}
	state, err := t.loadCuratorStateLocked()
	if err != nil {
		return err
	}
	rec, ok := state.Skills[name]
	if !ok || rec.CreatedBy != SkillCuratorCreatedByAgent {
		return nil
	}
	rec.UseCount++
	rec.LastUsedAt = usedAt
	rec.State = SkillCuratorStateActive
	rec.ArchivedAt = 0
	state.Skills[name] = normalizeCuratorRecord(name, rec)
	return t.saveCuratorStateLocked(state)
}

func (t *Tracker) loadCuratorStateLocked() (*SkillCuratorState, error) {
	state := &SkillCuratorState{
		Version: 1,
		Skills:  make(map[string]SkillCuratorRecord),
	}
	if t.curatorPath == "" {
		return state, nil
	}
	data, err := os.ReadFile(t.curatorPath)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skill curator: read state: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("skill curator: parse state: %w", err)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Skills == nil {
		state.Skills = make(map[string]SkillCuratorRecord)
	}
	for name, rec := range state.Skills {
		state.Skills[name] = normalizeCuratorRecord(name, rec)
	}
	return state, nil
}

func (t *Tracker) saveCuratorStateLocked(state *SkillCuratorState) error {
	if t.curatorPath == "" {
		return nil
	}
	if state == nil {
		state = &SkillCuratorState{}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Skills == nil {
		state.Skills = make(map[string]SkillCuratorRecord)
	}
	state.UpdatedAt = time.Now().UnixMilli()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("skill curator: encode state: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.WriteFile(t.curatorPath, data, &atomicfile.Options{
		Perm:  0o600,
		Fsync: true,
	}); err != nil {
		return fmt.Errorf("skill curator: write state: %w", err)
	}
	return nil
}

func normalizeCuratorRecord(name string, rec SkillCuratorRecord) SkillCuratorRecord {
	if rec.SkillName == "" {
		rec.SkillName = name
	}
	if rec.State == "" {
		rec.State = SkillCuratorStateActive
	}
	return rec
}

func curatorLastActivity(rec SkillCuratorRecord) int64 {
	last := rec.CreatedAt
	if rec.LastUsedAt > last {
		last = rec.LastUsedAt
	}
	if rec.LastPatchedAt > last {
		last = rec.LastPatchedAt
	}
	return last
}

func curatorStateRank(state string) int {
	switch state {
	case SkillCuratorStateActive:
		return 0
	case SkillCuratorStateStale:
		return 1
	case SkillCuratorStateArchived:
		return 2
	default:
		return 3
	}
}

// SkillCuratorTask runs the lifecycle transitions on the autonomous scheduler.
type SkillCuratorTask struct {
	Tracker *Tracker
	Logger  *slog.Logger
	Config  SkillCuratorConfig
}

func (t *SkillCuratorTask) Name() string { return "skill-curator" }

func (t *SkillCuratorTask) Interval() time.Duration {
	cfg := t.Config.withDefaults()
	return time.Duration(cfg.IntervalHours) * time.Hour
}

func (t *SkillCuratorTask) Run(ctx context.Context) error {
	if t.Tracker == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := t.Config.withDefaults()
	summary, err := t.Tracker.ApplySkillCuratorTransitions(time.Now(), cfg)
	if err != nil {
		return err
	}
	if len(summary.Transitions) > 0 && t.Logger != nil {
		t.Logger.Info("skill-curator: cycle complete",
			"checked", summary.Checked,
			"stale", summary.MarkedStale,
			"archived", summary.Archived,
			"reactivated", summary.Reactivated,
		)
	}
	return nil
}
