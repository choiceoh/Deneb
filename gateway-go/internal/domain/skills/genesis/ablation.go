package genesis

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	defaultSkillAblationTrials = 3
	minSkillAblationTrials     = 2
	maxSkillAblationTrials     = 6
	skillAblationMaxCases      = 3
	skillAblationInterval      = 7 * 24 * time.Hour
)

// SkillAblationRecord measures the execution-grounded difference between an
// agent given a skill and the same executor given no skill. It is advisory
// evidence: a non-positive lift is a review/retirement candidate, never an
// automatic archive decision.
type SkillAblationRecord struct {
	SkillName          string  `json:"skillName"`
	Model              string  `json:"model,omitempty"`
	Evaluated          bool    `json:"evaluated"`
	CaseCount          int     `json:"caseCount"`
	Trials             int     `json:"trials"`
	WithSkillPassed    int     `json:"withSkillPassed"`
	WithSkillTotal     int     `json:"withSkillTotal"`
	WithoutSkillPassed int     `json:"withoutSkillPassed"`
	WithoutSkillTotal  int     `json:"withoutSkillTotal"`
	WithSkillScore     float64 `json:"withSkillScore"`
	WithoutSkillScore  float64 `json:"withoutSkillScore"`
	Lift               float64 `json:"lift"`
	CreatedAt          int64   `json:"createdAt"`
}

// SkillAblationSummary is the compact operator-facing projection of persisted
// ablation runs. NoLiftSkills uses each skill's latest run only.
type SkillAblationSummary struct {
	SkillName          string   `json:"skillName,omitempty"`
	Runs               int      `json:"runs"`
	SkillsMeasured     int      `json:"skillsMeasured"`
	LastRunAt          int64    `json:"lastRunAt,omitempty"`
	LatestSkill        string   `json:"latestSkill,omitempty"`
	LatestWithScore    float64  `json:"latestWithScore,omitempty"`
	LatestWithoutScore float64  `json:"latestWithoutScore,omitempty"`
	LatestLift         float64  `json:"latestLift,omitempty"`
	NoLiftSkills       []string `json:"noLiftSkills,omitempty"`
}

type skillReplayRunner func(context.Context, string, SkillValidationCaseRecord) (skillReplayTrace, error)

// EvaluateAblation runs the same held-out replay cases with and without the
// current skill body. Trials alternate AB/BA execution order to reduce drift
// bias from always running one condition first. Missing executors, cases, or a
// flaky replay is returned as an observable error; the scheduled caller fails
// open without persisting it and stops the cycle before costs can fan out.
func (v *SkillValidationEngine) EvaluateAblation(ctx context.Context, skillName, skillBody string, trials int) (SkillAblationRecord, error) {
	if v == nil || v.tracker == nil || strings.TrimSpace(skillBody) == "" {
		return SkillAblationRecord{}, nil
	}
	executor, model := v.executorSnapshot()
	if executor == nil {
		return SkillAblationRecord{}, nil
	}
	runner := func(ctx context.Context, body string, testCase SkillValidationCaseRecord) (skillReplayTrace, error) {
		return v.runReplayExecutorWith(ctx, executor, model, body, testCase.Replay)
	}
	return v.evaluateAblationWith(ctx, skillName, skillBody, model, trials, runner)
}

func (v *SkillValidationEngine) evaluateAblationWith(
	ctx context.Context,
	skillName, skillBody, model string,
	trials int,
	runner skillReplayRunner,
) (SkillAblationRecord, error) {
	if v == nil || v.tracker == nil || runner == nil {
		return SkillAblationRecord{}, nil
	}
	cases, err := v.heldOutCases(skillName)
	if err != nil {
		return SkillAblationRecord{}, err
	}
	evaluable := replayBehaviorCases(cases, skillAblationMaxCases)
	if len(evaluable) == 0 {
		return SkillAblationRecord{}, nil
	}

	trials = normalizeSkillAblationTrials(trials)
	var withSkill, withoutSkill validationCaseScore
	for caseIndex, testCase := range evaluable {
		for trial := 0; trial < trials; trial++ {
			if err := ctx.Err(); err != nil {
				return SkillAblationRecord{}, err
			}
			var withTrace, withoutTrace skillReplayTrace
			var withErr, withoutErr error
			if (caseIndex+trial)%2 == 0 {
				withTrace, withErr = runner(ctx, skillBody, testCase)
				withoutTrace, withoutErr = runner(ctx, "", testCase)
			} else {
				withoutTrace, withoutErr = runner(ctx, "", testCase)
				withTrace, withErr = runner(ctx, skillBody, testCase)
			}
			if withErr != nil || withoutErr != nil {
				if v.logger != nil {
					v.logger.Warn("genesis: skill ablation replay failed, skipping measurement",
						"skill", skillName, "withSkillError", withErr, "withoutSkillError", withoutErr)
				}
				if withErr != nil {
					return SkillAblationRecord{}, fmt.Errorf("skill ablation treatment replay: %w", withErr)
				}
				return SkillAblationRecord{}, fmt.Errorf("skill ablation control replay: %w", withoutErr)
			}
			withSkill.add(scoreReplayAgainstTrace(withTrace, testCase))
			withoutSkill.add(scoreReplayAgainstTrace(withoutTrace, testCase))
		}
	}
	if withSkill.Total == 0 || withoutSkill.Total == 0 {
		return SkillAblationRecord{}, nil
	}
	withScore := withSkill.percent()
	withoutScore := withoutSkill.percent()
	return SkillAblationRecord{
		SkillName:          strings.TrimSpace(skillName),
		Model:              strings.TrimSpace(model),
		Evaluated:          true,
		CaseCount:          len(evaluable),
		Trials:             trials,
		WithSkillPassed:    withSkill.Passed,
		WithSkillTotal:     withSkill.Total,
		WithoutSkillPassed: withoutSkill.Passed,
		WithoutSkillTotal:  withoutSkill.Total,
		WithSkillScore:     withScore,
		WithoutSkillScore:  withoutScore,
		Lift:               withScore - withoutScore,
	}, nil
}

func normalizeSkillAblationTrials(trials int) int {
	if trials == 0 {
		return defaultSkillAblationTrials
	}
	if trials < minSkillAblationTrials {
		return minSkillAblationTrials
	}
	if trials > maxSkillAblationTrials {
		return maxSkillAblationTrials
	}
	return trials
}

// RecordSkillAblation persists one completed comparison outside skill usage
// accounting. Synthetic counterfactuals must never affect real success rates.
func (t *Tracker) RecordSkillAblation(record SkillAblationRecord) error {
	if t == nil {
		return fmt.Errorf("genesis-tracker: tracker is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	record.SkillName = strings.TrimSpace(record.SkillName)
	record.Model = strings.TrimSpace(record.Model)
	if record.SkillName == "" || !record.Evaluated {
		return fmt.Errorf("genesis-tracker: evaluated ablation with skillName is required")
	}
	if record.CaseCount <= 0 || record.Trials < minSkillAblationTrials || record.Trials > maxSkillAblationTrials ||
		record.WithSkillTotal <= 0 || record.WithoutSkillTotal <= 0 {
		return fmt.Errorf("genesis-tracker: invalid ablation sample counts")
	}
	if record.WithSkillPassed < 0 || record.WithSkillPassed > record.WithSkillTotal ||
		record.WithoutSkillPassed < 0 || record.WithoutSkillPassed > record.WithoutSkillTotal ||
		record.WithSkillScore < 0 || record.WithSkillScore > 100 ||
		record.WithoutSkillScore < 0 || record.WithoutSkillScore > 100 ||
		math.Abs(record.Lift-(record.WithSkillScore-record.WithoutSkillScore)) > 0.001 {
		return fmt.Errorf("genesis-tracker: inconsistent ablation scores")
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().UnixMilli()
	}
	if err := jsonlstore.Append(t.ablationPath, record); err != nil {
		return fmt.Errorf("genesis-tracker: append skill ablation: %w", err)
	}
	return nil
}

// RecentSkillAblations returns newest-first persisted comparisons.
func (t *Tracker) RecentSkillAblations(skillName string, limit int) ([]SkillAblationRecord, error) {
	if t == nil {
		return nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[SkillAblationRecord](t.ablationPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load skill ablations: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}
	filter := strings.TrimSpace(skillName)
	out := make([]SkillAblationRecord, 0, min(limit, len(entries)))
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		if filter != "" && entries[i].SkillName != filter {
			continue
		}
		out = append(out, entries[i])
	}
	return out, nil
}

// SkillAblationSummary reports latest lift and the skills whose latest run did
// not beat the no-skill baseline. It does not prescribe retirement.
func (t *Tracker) SkillAblationSummary(skillName string) (SkillAblationSummary, error) {
	if t == nil {
		return SkillAblationSummary{}, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[SkillAblationRecord](t.ablationPath)
	if err != nil {
		return SkillAblationSummary{}, fmt.Errorf("genesis-tracker: load skill ablations: %w", err)
	}
	filter := strings.TrimSpace(skillName)
	summary := SkillAblationSummary{SkillName: filter}
	latestBySkill := make(map[string]SkillAblationRecord)
	for _, record := range entries {
		if filter != "" && record.SkillName != filter {
			continue
		}
		summary.Runs++
		if previous, ok := latestBySkill[record.SkillName]; !ok || record.CreatedAt >= previous.CreatedAt {
			latestBySkill[record.SkillName] = record
		}
		if record.CreatedAt >= summary.LastRunAt {
			summary.LastRunAt = record.CreatedAt
			summary.LatestSkill = record.SkillName
			summary.LatestWithScore = record.WithSkillScore
			summary.LatestWithoutScore = record.WithoutSkillScore
			summary.LatestLift = record.Lift
		}
	}
	summary.SkillsMeasured = len(latestBySkill)
	for name, record := range latestBySkill {
		if record.Lift <= 0 {
			summary.NoLiftSkills = append(summary.NoLiftSkills, name)
		}
	}
	sort.Strings(summary.NoLiftSkills)
	return summary, nil
}

func (t *Tracker) skillAblationLastAt() (map[string]int64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[SkillAblationRecord](t.ablationPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load skill ablations: %w", err)
	}
	lastAt := make(map[string]int64)
	for _, record := range entries {
		if record.CreatedAt > lastAt[record.SkillName] {
			lastAt[record.SkillName] = record.CreatedAt
		}
	}
	return lastAt, nil
}

// SkillAblationTask measures one least-recently-covered skill per weekly cycle.
// One skill × three cases × three trials × two conditions caps a default run at
// 18 lightweight-model calls.
type SkillAblationTask struct {
	Engine  *SkillValidationEngine
	Tracker *Tracker
	Catalog *skills.Catalog
	Logger  *slog.Logger

	evaluate func(context.Context, string, string, int) (SkillAblationRecord, error)
}

func (t *SkillAblationTask) Name() string { return "skill-ablation" }

func (t *SkillAblationTask) Interval() time.Duration {
	if value := strings.TrimSpace(os.Getenv("DENEB_SKILL_ABLATION_INTERVAL_HOURS")); value != "" {
		if hours, err := strconv.Atoi(value); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return skillAblationInterval
}

func (t *SkillAblationTask) Run(ctx context.Context) error {
	if t == nil || t.Tracker == nil || t.Catalog == nil {
		return nil
	}
	evaluate := t.evaluate
	if evaluate == nil {
		if t.Engine == nil {
			return nil
		}
		evaluate = t.Engine.EvaluateAblation
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	lastAt, err := t.Tracker.skillAblationLastAt()
	if err != nil {
		logger.Warn("skill-ablation: rotation state unavailable", "error", err)
		return nil
	}
	entries := t.Catalog.List()
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := lastAt[entries[i].Skill.Name], lastAt[entries[j].Skill.Name]
		if left != right {
			return left < right
		}
		return entries[i].Skill.Name < entries[j].Skill.Name
	})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSpace(entry.Skill.Name)
		path := strings.TrimSpace(entry.Skill.FilePath)
		if name == "" || path == "" {
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			logger.Warn("skill-ablation: body read failed", "skill", name, "error", readErr)
			continue
		}
		result, evalErr := evaluate(ctx, name, skillBodyOnly(string(content)), defaultSkillAblationTrials)
		if evalErr != nil {
			logger.Warn("skill-ablation: evaluation failed", "skill", name, "error", evalErr)
			return nil
		}
		if !result.Evaluated {
			continue
		}
		result.SkillName = name
		if recordErr := t.Tracker.RecordSkillAblation(result); recordErr != nil {
			logger.Warn("skill-ablation: record failed", "skill", name, "error", recordErr)
			return nil
		}
		logger.Info("skill-ablation: comparison recorded", "skill", name,
			"withSkillScore", result.WithSkillScore, "withoutSkillScore", result.WithoutSkillScore,
			"lift", result.Lift, "trials", result.Trials, "cases", result.CaseCount)
		return nil
	}
	return nil
}
