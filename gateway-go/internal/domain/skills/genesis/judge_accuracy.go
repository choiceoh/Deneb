package genesis

// Judge-accuracy standing lane — the P3 food factory.
//
// Verifier co-evolution (roadmap P3) learns from labeled judge mistakes, but
// organic labels (rollback-after-accept, strong-usage-after-reject) trickle at
// real-usage speed. Two synthetic-but-honest sources run on a clock instead:
//
//  1. Planted-defect accuracy: replay degradation gold pairs (the SAME
//     mechanical defects the meta bench uses — ground truth is known because
//     WE planted it) through the LIVE judge prompt and ledger every verdict.
//     Misses are ready-made few-shot exhibits for the future judge evolution.
//  2. False-reject mining: score each buffered rejected candidate against the
//     CURRENT skill body on stored validation cases — a rejected body that
//     scores strictly better with zero flips is a suspected judge false
//     reject, the label class organic usage almost never surfaces.
//
// Everything is labeled at construction; nothing touches real-usage stats.
// LLM executes the judge prompt only; scoring and mining are deterministic Go.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	common "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	// judgeAccuracyDefaultInterval keeps the lane cheap by default; the env
	// knob accelerates calibration (each run costs one judge call per pair).
	judgeAccuracyDefaultInterval = 12 * time.Hour
	// judgeAccuracyMaxExhibits bounds the per-run miss exhibits kept in the
	// ledger record.
	judgeAccuracyMaxExhibits = 4
	// falseRejectMargin is how much better (validation percent) a rejected
	// candidate must score than the current body before it is flagged.
	falseRejectMargin = 10.0
	// falseRejectPerSkill bounds mined rejected edits per skill per run.
	falseRejectPerSkill = 3
)

// JudgeMissExhibit is one wrong verdict on a planted defect — few-shot food
// for P3 judge evolution.
type JudgeMissExhibit struct {
	Skill       string `json:"skill"`
	Degradation string `json:"degradation"`
	Verdict     string `json:"verdict"` // "passed_defect" | "score_inverted" | "error"
}

// FalseRejectExhibit is a buffered rejected candidate that outscores the
// current body on the stored validation corpus.
type FalseRejectExhibit struct {
	Skill        string  `json:"skill"`
	RejectReason string  `json:"rejectReason"`
	CurrentScore float64 `json:"currentScore"`
	RejectScore  float64 `json:"rejectScore"`
	RejectedAt   int64   `json:"rejectedAt"`
}

// JudgeAccuracyRecord is one lane run: the live judge's accuracy over planted
// defects plus mined false-reject suspects, attributed to the judge prompt
// version so P3 can segment labels per verifier revision.
type JudgeAccuracyRecord struct {
	CreatedAt    int64                `json:"createdAt"`
	JudgeVersion string               `json:"judgeVersion"`
	Pairs        int                  `json:"pairs"`
	Correct      int                  `json:"correct"`
	ByClass      map[string][2]int    `json:"byClass,omitempty"` // degradation -> [correct, total]
	Misses       []JudgeMissExhibit   `json:"misses,omitempty"`
	FalseRejects []FalseRejectExhibit `json:"falseRejects,omitempty"`
}

// judgeAccuracyLogPath mirrors the tracker's data-dir convention.
func (t *Tracker) judgeAccuracyLogPath() string {
	return filepath.Join(filepath.Dir(t.logPath), "judge_accuracy_log.jsonl")
}

// LogJudgeAccuracy appends one lane run to the P3 label ledger.
func (t *Tracker) LogJudgeAccuracy(rec JudgeAccuracyRecord) error {
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	return jsonlstore.Append(t.judgeAccuracyLogPath(), rec)
}

// RecentJudgeAccuracy returns the newest lane runs, newest first.
func (t *Tracker) RecentJudgeAccuracy(limit int) ([]JudgeAccuracyRecord, error) {
	if limit <= 0 {
		limit = 10
	}
	entries, err := jsonlstore.Load[JudgeAccuracyRecord](t.judgeAccuracyLogPath())
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load judge accuracy: %w", err)
	}
	out := make([]JudgeAccuracyRecord, 0, min(limit, len(entries)))
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, entries[i])
	}
	return out, nil
}

// JudgeAccuracyTask is the standing lane. Registered production-gated (it
// makes live judge calls and writes shared genesis state).
type JudgeAccuracyTask struct {
	Evolver *Evolver
	Meta    *generation.MetaArtifacts
	Tracker *Tracker
	Logger  *slog.Logger

	// verdictFn overrides the LLM executor in tests.
	verdictFn judgeBenchVerdictFn
}

// Name identifies the task in the autonomous scheduler.
func (t *JudgeAccuracyTask) Name() string { return "judge-accuracy" }

// Interval honors DENEB_JUDGE_ACCURACY_INTERVAL_HOURS.
func (t *JudgeAccuracyTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_JUDGE_ACCURACY_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return judgeAccuracyDefaultInterval
}

// Run executes one lane cycle: planted-defect replay + false-reject mining.
func (t *JudgeAccuracyTask) Run(ctx context.Context) error {
	if t.Evolver == nil || t.Tracker == nil || t.Meta == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	verdict := t.verdictFn
	if verdict == nil {
		mt := &MetaEvolutionTask{Evolver: t.Evolver}
		verdict = mt.judgeBenchExecutor()
	}
	if verdict == nil {
		return nil // no judge model wired
	}

	judgeFallback := generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt]
	judgePrompt := t.Meta.Load(generation.MetaSkillJudgeSystemPrompt, judgeFallback)
	rec := JudgeAccuracyRecord{
		JudgeVersion: t.Meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback),
		ByClass:      map[string][2]int{},
	}

	pairs := buildJudgeDegradationPairs(t.Evolver.catalogEntries(), judgeBenchMaxPairs*metaBenchScale())
	for _, pair := range pairs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rec.Pairs++
		cls := rec.ByClass[pair.Degradation]
		cls[1]++
		v, err := verdict(ctx, judgePrompt, pair.Original, pair.Degraded)
		switch {
		case err != nil:
			rec.Misses = append(rec.Misses, JudgeMissExhibit{Skill: pair.Skill, Degradation: pair.Degradation, Verdict: "error"})
		case v.Pass:
			rec.Misses = append(rec.Misses, JudgeMissExhibit{Skill: pair.Skill, Degradation: pair.Degradation, Verdict: "passed_defect"})
		case v.OriginalScore != nil && v.CandidateScore != nil && *v.CandidateScore > *v.OriginalScore:
			rec.Misses = append(rec.Misses, JudgeMissExhibit{Skill: pair.Skill, Degradation: pair.Degradation, Verdict: "score_inverted"})
		default:
			rec.Correct++
			cls[0]++
		}
		rec.ByClass[pair.Degradation] = cls
	}
	if len(rec.Misses) > judgeAccuracyMaxExhibits {
		rec.Misses = rec.Misses[:judgeAccuracyMaxExhibits]
	}

	rec.FalseRejects = t.mineFalseRejects()

	if rec.Pairs == 0 && len(rec.FalseRejects) == 0 {
		return nil // nothing to ledger — corpus too thin this run
	}
	if err := t.Tracker.LogJudgeAccuracy(rec); err != nil {
		logger.Warn("judge-accuracy: ledger write failed", "error", err)
		return nil
	}
	logger.Info("judge-accuracy: lane run ledgered (P3 label food)",
		"pairs", rec.Pairs, "correct", rec.Correct, "misses", len(rec.Misses),
		"falseRejects", len(rec.FalseRejects), "judgeVersion", rec.JudgeVersion)
	return nil
}

// mineFalseRejects scores buffered rejected candidates against the CURRENT
// skill body on stored validation cases. Deterministic; flags only a strict,
// flip-free improvement beyond the margin.
func (t *JudgeAccuracyTask) mineFalseRejects() []FalseRejectExhibit {
	var out []FalseRejectExhibit
	for _, entry := range t.Evolver.catalogEntries() {
		skill := entry.Skill.Name
		cases, err := t.Tracker.RecentSkillValidationCases(skill, producerBenchCaseLimit)
		if err != nil || !hasScorableValidationCase(cases) {
			continue
		}
		raw, err := os.ReadFile(entry.Skill.FilePath)
		if err != nil {
			continue
		}
		currentBody := skillBodyOnly(string(raw))
		rejected, err := t.Tracker.RecentRejectedSkillEdits(skill, falseRejectPerSkill)
		if err != nil {
			continue
		}
		curByCase := scoreSkillValidationCasesByCase(currentBody, cases)
		var cur validationCaseScore
		for i := range curByCase {
			cur.add(curByCase[i])
		}
		for _, rej := range rejected {
			body := strings.TrimSpace(rej.CandidateBody)
			if body == "" {
				continue
			}
			rejByCase := scoreSkillValidationCasesByCase(body, cases)
			var rjs validationCaseScore
			flipped := false
			for i := range rejByCase {
				rjs.add(rejByCase[i])
				if curByCase[i].Total > 0 && curByCase[i].casePasses() && !rejByCase[i].casePasses() {
					flipped = true
					break
				}
			}
			if flipped || rjs.Total == 0 {
				continue
			}
			if rjs.Percent() >= cur.Percent()+falseRejectMargin {
				out = append(out, FalseRejectExhibit{
					Skill:        skill,
					RejectReason: common.TruncateRunes(rej.Reason, 160),
					CurrentScore: cur.Percent(),
					RejectScore:  rjs.Percent(),
					RejectedAt:   rej.CreatedAt,
				})
			}
		}
	}
	if len(out) > judgeAccuracyMaxExhibits {
		out = out[:judgeAccuracyMaxExhibits]
	}
	return out
}
