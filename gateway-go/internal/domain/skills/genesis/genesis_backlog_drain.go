// genesis_backlog_drain.go — deterministic drain of route=genesis skill
// opportunities into actual skill creation (RSI P5 workstream 1).
//
// Diagnosis (2026-07-18): route=genesis opportunities pile up in
// skill_opportunities.jsonl with NO consumer that creates skills from them —
// the ledger's readers are status display and curriculum evidence assembly,
// and actual creation relied on a nudged LLM session executing genesis inline
// (observed follow-through: 1 of 10 routed proposals in 7d, 51 signals
// accumulated). This task closes the loop deterministically: each run it
// picks the most-recurring undrained demand pattern and pushes it through the
// EXISTING generation gates.
//
// Invariants preserved (self-improvement.md):
//   - LLM produces, deterministic Go decides: the draft goes through
//     gateGenerated (specificity gate) + Persist (name validation, dedup,
//     daily cap) — this task adds no new acceptance authority.
//   - Propose-real-demand only: signals come from Propus-reviewed REAL
//     sessions (and curriculum-filed demand, which authored validation cases
//     first); the drain never invents demand.
//   - Bounded: one creation attempt per run; drained patterns never retry on
//     a terminal outcome (created/deduped), and back off on skip/error.
package genesis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

const (
	genesisDrainDefaultInterval = 24 * time.Hour
	// genesisDrainMinRecurrence: a pattern seen once is a single session's
	// opinion; recurrence is the demand signal (mirrors the recurrence
	// promotion and runtime-error mining floors, scaled to this ledger's
	// volume).
	genesisDrainMinRecurrence = 2
	// genesisDrainRetryCooldown is how long a skip/error outcome parks a
	// pattern before it may be attempted again.
	genesisDrainRetryCooldown = 7 * 24 * time.Hour
	// genesisDrainBacklogScan bounds the ledger read per run.
	genesisDrainBacklogScan = 300
)

// backlogGenesis is the narrow generation port the drain consumes —
// interface-typed so tests need no live LLM service.
type backlogGenesis interface {
	GenerateFromBacklog(ctx context.Context, brief string) (*generation.GeneratedSkill, error)
	Persist(skill *generation.GeneratedSkill) error
}

// GenesisBacklogDrainTask is the standing lane. Registered production-gated
// (live LLM call + shared genesis writes).
type GenesisBacklogDrainTask struct {
	Tracker *Tracker
	Genesis backlogGenesis
	Logger  *slog.Logger
	// StatePath overrides the drained-pattern ledger location (tests). Empty
	// resolves to ~/.deneb/data/genesis_backlog_drain_state.json.
	StatePath string
}

// Name identifies the task in the autonomous scheduler.
func (t *GenesisBacklogDrainTask) Name() string { return "genesis-backlog-drain" }

// Interval honors DENEB_GENESIS_DRAIN_INTERVAL_HOURS.
func (t *GenesisBacklogDrainTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_GENESIS_DRAIN_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return genesisDrainDefaultInterval
}

// genesisDrainOutcome is one pattern's drain verdict.
type genesisDrainOutcome struct {
	Outcome   string `json:"outcome"` // created | deduped | skip | error
	SkillName string `json:"skillName,omitempty"`
	At        int64  `json:"atMs"`
}

// terminal reports whether the pattern must never be attempted again:
// created (demand met) and deduped (an existing skill already covers it).
func (o genesisDrainOutcome) terminal() bool {
	return o.Outcome == "created" || o.Outcome == "deduped"
}

func (t *GenesisBacklogDrainTask) statePath() string {
	if strings.TrimSpace(t.StatePath) != "" {
		return t.StatePath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "data", "genesis_backlog_drain_state.json")
}

func loadGenesisDrainState(path string) map[string]genesisDrainOutcome {
	st := map[string]genesisDrainOutcome{}
	if path == "" {
		return st
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	loaded := map[string]genesisDrainOutcome{}
	if json.Unmarshal(raw, &loaded) != nil {
		return st
	}
	return loaded
}

func saveGenesisDrainState(path string, st map[string]genesisDrainOutcome) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// genesisDemandGroup is one undrained recurring demand pattern.
type genesisDemandGroup struct {
	key     string
	count   int
	newest  int64
	records []SkillOpportunityRecord
}

// collectGenesisDemand groups route=genesis opportunities by pattern key and
// filters by the drain state: terminal outcomes never retry, skip/error
// outcomes retry after the cooldown.
func collectGenesisDemand(
	records []SkillOpportunityRecord,
	state map[string]genesisDrainOutcome,
	now time.Time,
) []genesisDemandGroup {
	groups := map[string]*genesisDemandGroup{}
	for _, rec := range records {
		if rec.Route != "genesis" {
			continue
		}
		key := opportunityPatternKey(rec.Candidate)
		if key == "" {
			continue
		}
		if prev, ok := state[key]; ok {
			if prev.terminal() || now.UnixMilli()-prev.At < genesisDrainRetryCooldown.Milliseconds() {
				continue
			}
		}
		g := groups[key]
		if g == nil {
			g = &genesisDemandGroup{key: key}
			groups[key] = g
		}
		g.count++
		g.records = append(g.records, rec)
		if rec.CreatedAt > g.newest {
			g.newest = rec.CreatedAt
		}
	}
	out := make([]genesisDemandGroup, 0, len(groups))
	for _, g := range groups {
		if g.count >= genesisDrainMinRecurrence {
			out = append(out, *g)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		if out[i].newest != out[j].newest {
			return out[i].newest > out[j].newest
		}
		return out[i].key < out[j].key
	})
	return out
}

// composeDrainBrief renders one demand group as the generation brief:
// recurrence framing plus each signal's candidate prose, evidence, and reason.
func composeDrainBrief(g genesisDemandGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "동일 수요가 %d개 세션에서 독립적으로 관측됨.\n", g.count)
	for i, rec := range g.records {
		if i >= 4 {
			break // the brief needs shape, not every duplicate verbatim
		}
		fmt.Fprintf(&b, "\n### 신호 %d\n%s\n", i+1, common.TruncateRunes(strings.TrimSpace(rec.Candidate), 1200))
		if e := strings.TrimSpace(rec.Evidence); e != "" {
			fmt.Fprintf(&b, "근거: %s\n", common.TruncateRunes(e, 600))
		}
		if r := strings.TrimSpace(rec.Reason); r != "" {
			fmt.Fprintf(&b, "이유: %s\n", common.TruncateRunes(r, 400))
		}
	}
	return b.String()
}

// Run executes one drain cycle: pick the strongest undrained demand pattern
// and push it through the existing generation gates.
func (t *GenesisBacklogDrainTask) Run(ctx context.Context) error {
	if t.Tracker == nil || t.Genesis == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	records, err := t.Tracker.RecentSkillOpportunities("", genesisDrainBacklogScan)
	if err != nil {
		logger.Warn("genesis-backlog-drain: opportunity scan failed", "error", err)
		return nil
	}
	path := t.statePath()
	state := loadGenesisDrainState(path)
	now := time.Now()

	demand := collectGenesisDemand(records, state, now)
	if len(demand) == 0 {
		return nil
	}
	g := demand[0]

	record := func(outcome genesisDrainOutcome) {
		outcome.At = now.UnixMilli()
		state[g.key] = outcome
		if err := saveGenesisDrainState(path, state); err != nil {
			logger.Warn("genesis-backlog-drain: state save failed", "error", err)
		}
	}

	skill, genErr := t.Genesis.GenerateFromBacklog(ctx, composeDrainBrief(g))
	if genErr != nil {
		logger.Warn("genesis-backlog-drain: generate failed", "pattern", g.key, "error", genErr)
		record(genesisDrainOutcome{Outcome: "error"})
		return nil
	}
	if skill == nil {
		logger.Info("genesis-backlog-drain: producer skipped pattern", "pattern", g.key, "recurrence", g.count)
		record(genesisDrainOutcome{Outcome: "skip"})
		return nil
	}
	if err := t.Genesis.Persist(skill); err != nil {
		if errors.Is(err, generation.ErrSkillDeduped) {
			logger.Info("genesis-backlog-drain: existing skill already covers demand",
				"pattern", g.key, "skill", skill.Name)
			record(genesisDrainOutcome{Outcome: "deduped", SkillName: skill.Name})
			return nil
		}
		logger.Warn("genesis-backlog-drain: persist failed", "pattern", g.key, "error", err)
		record(genesisDrainOutcome{Outcome: "error"})
		return nil
	}

	sessionKey := ""
	if len(g.records) > 0 {
		sessionKey = g.records[0].SessionKey
	}
	if err := t.Tracker.LogGenesis(skill.Name, "backlog-drain", sessionKey, skill.Category, skill.Description); err != nil {
		logger.Warn("genesis-backlog-drain: genesis log failed", "skill", skill.Name, "error", err)
	}
	logger.Info("genesis-backlog-drain: skill created from recurring demand",
		"skill", skill.Name, "pattern", g.key, "recurrence", g.count)
	record(genesisDrainOutcome{Outcome: "created", SkillName: skill.Name})
	return nil
}
