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
	// judgeAccuracyAbortAfterErrors stops a cold-start/outage storm from
	// burning the whole corpus: after this many consecutive verdict call
	// failures the lane exits without ledgering infra noise as judge misses.
	judgeAccuracyAbortAfterErrors = 3
	// falseRejectMargin is how much better (validation percent) a rejected
	// candidate must score than the current body before it is flagged.
	falseRejectMargin = 10.0
	// falseRejectPerSkill bounds mined rejected edits per skill per run.
	falseRejectPerSkill = 3
	// organicFalseAcceptWindow bounds how far back real-usage rollback labels
	// are mined. Rollbacks are scarce at organic cadence (a handful per month),
	// so the window is deliberately wider than the 7d health window.
	organicFalseAcceptWindow = 30 * 24 * time.Hour
	// judgeEscalationWindow is how many ledgered runs of the INCUMBENT judge,
	// each carrying drop-tier subtle pairs with zero drop-tier misses, unlock
	// the harder in-place weaken tier (probe curriculum ladder — CoEvoSkills:
	// a probe corpus the judge has fully outgrown produces zero labels
	// forever, so difficulty must track judge strength). A drop-tier miss
	// anywhere in the window re-locks the tier — the frontier moved back down,
	// so the probe budget returns there. A judge revision resets the
	// curriculum: version scoping means a fresh judge re-earns tier 3.
	judgeEscalationWindow = 5
)

// organicFalseAccept is one REAL-usage judge false-accept label: the judge
// passed a candidate, the evolve shipped, and the post-evolve watch rolled it
// back. Counted only when the baseline-aware e-process AGREED the failure
// rate rose (BaselineTest.Reject) — the deterministic filter for the PACE
// precondition that baseline-blind rollbacks mislabel (roadmap P3 #1): a
// threshold-only rollback with a quiet e-process stays a disagreement label,
// never P3 food.
type organicFalseAccept struct {
	Skill        string `json:"skill"`
	JudgeVersion string `json:"judgeVersion,omitempty"`
	RolledBackAt int64  `json:"rolledBackAt"`
}

// organicFalseAccepts mines the lifecycle ledger for baseline-confirmed
// rollbacks within the window, attributing each to the judge-artifact version
// that accepted the evolve (the preceding "evolved" entry's provenance
// certificate, RSI P1.5). Newest first, capped at limit. A rollback whose
// accepting evolve carried no provenance yields an empty JudgeVersion — the
// consumer's incumbent-version filter then excludes it (unattributable labels
// are not actionable evidence).
func (t *Tracker) organicFalseAccepts(window time.Duration, limit int) []organicFalseAccept {
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	judgeBySkill := map[string]string{}
	var out []organicFalseAccept
	for _, e := range entries { // chronological
		switch e.Type {
		case "evolved":
			v := ""
			if e.Provenance != nil {
				v = e.Provenance.JudgeArtifactVersion
			}
			judgeBySkill[e.SkillName] = v
		case "evolve_rolled_back":
			if e.CreatedAt < cutoff || e.BaselineTest == nil || !e.BaselineTest.Reject {
				continue
			}
			out = append(out, organicFalseAccept{
				Skill:        e.SkillName,
				JudgeVersion: judgeBySkill[e.SkillName],
				RolledBackAt: e.CreatedAt,
			})
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { // newest first
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// judgeMissExhibit is one wrong verdict on a planted defect — few-shot food
// for P3 judge evolution. Verdict "error" is legacy (pre-infra-filter ledger
// rows); new runs only record passed_defect / score_inverted.
type judgeMissExhibit struct {
	Skill       string `json:"skill"`
	Degradation string `json:"degradation"`
	Verdict     string `json:"verdict"` // "passed_defect" | "score_inverted" | "error" (legacy)
}

// judgeAccuracyProbeUsable reports whether a ledger row is judge-quality
// evidence. Restart/warmup storms ledgered Correct==0 with only Verdict=error
// exhibits are infrastructure noise — they must not inflate L3 "판정 놓침",
// re-lock the weaken tier, trip verifier_broken, or feed evaluator epochs.
func judgeAccuracyProbeUsable(rec judgeAccuracyRecord) bool {
	if rec.Pairs == 0 {
		return len(rec.FalseRejects) > 0
	}
	if rec.Correct > 0 {
		return true
	}
	if len(rec.Misses) == 0 {
		return false
	}
	for _, m := range rec.Misses {
		if m.Verdict != "error" {
			return true
		}
	}
	return false
}

// judgeMissCountsAsFuel reports whether an exhibit is real P3 label food
// (judge too lenient), not an infra call failure.
func judgeMissCountsAsFuel(m judgeMissExhibit) bool {
	return m.Verdict == "passed_defect" || m.Verdict == "score_inverted"
}

// falseRejectExhibit is a buffered rejected candidate that outscores the
// current body on the stored validation corpus.
type falseRejectExhibit struct {
	Skill        string  `json:"skill"`
	RejectReason string  `json:"rejectReason"`
	CurrentScore float64 `json:"currentScore"`
	RejectScore  float64 `json:"rejectScore"`
	RejectedAt   int64   `json:"rejectedAt"`
}

const (
	OperatorJudgeVerdictConfirm  = "confirm"
	OperatorJudgeVerdictRollback = "rollback"
)

// OperatorJudgeVerdict is a real human label on a borderline accepted evolve.
// DecisionID makes action retries idempotent; JudgeVersion keeps P3 evidence
// scoped to the evaluator artifact that made the original decision.
type OperatorJudgeVerdict struct {
	DecisionID   string  `json:"decisionId"`
	Skill        string  `json:"skill"`
	Version      string  `json:"version"`
	Verdict      string  `json:"verdict"`
	JudgeVersion string  `json:"judgeVersion"`
	JudgeMargin  float64 `json:"judgeMargin"`
	CreatedAt    int64   `json:"createdAt"`
}

// judgeAccuracyRecord is one lane run: the live judge's accuracy over planted
// defects plus mined false-reject suspects, attributed to the judge prompt
// version so P3 can segment labels per verifier revision.
type judgeAccuracyRecord struct {
	CreatedAt    int64             `json:"createdAt"`
	JudgeVersion string            `json:"judgeVersion"`
	Pairs        int               `json:"pairs"`
	Correct      int               `json:"correct"`
	ByClass      map[string][2]int `json:"byClass,omitempty"` // degradation -> [correct, total]
	// ByCategory segments accuracy by skill CATEGORY (evaluator preference
	// collapse, arXiv 2606.16682 — a category-local bias hides in the
	// aggregate; segmenting makes it visible before it corrupts selection).
	ByCategory       map[string][2]int      `json:"byCategory,omitempty"` // category -> [correct, total]
	Misses           []judgeMissExhibit     `json:"misses,omitempty"`
	FalseRejects     []falseRejectExhibit   `json:"falseRejects,omitempty"`
	OperatorVerdicts []OperatorJudgeVerdict `json:"operatorVerdicts,omitempty"`
}

// judgeAccuracyLogPath mirrors the tracker's data-dir convention.
func (t *Tracker) judgeAccuracyLogPath() string {
	return filepath.Join(filepath.Dir(t.logPath), "judge_accuracy_log.jsonl")
}

// logJudgeAccuracy appends one lane run to the P3 label ledger.
func (t *Tracker) logJudgeAccuracy(rec judgeAccuracyRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = time.Now().UnixMilli()
	}
	return jsonlstore.Append(t.judgeAccuracyLogPath(), rec)
}

// LogOperatorJudgeVerdict appends one idempotent human label to the same P3
// ledger as synthetic judge-accuracy runs, so meta-evolution reads one
// version-attributed evidence stream.
func (t *Tracker) LogOperatorJudgeVerdict(verdict OperatorJudgeVerdict) error {
	verdict.DecisionID = strings.TrimSpace(verdict.DecisionID)
	verdict.Skill = strings.TrimSpace(verdict.Skill)
	verdict.Version = strings.TrimSpace(verdict.Version)
	verdict.JudgeVersion = strings.TrimSpace(verdict.JudgeVersion)
	if verdict.DecisionID == "" || verdict.Skill == "" || verdict.Version == "" {
		return fmt.Errorf("genesis-tracker: operator judge verdict missing identity")
	}
	if verdict.Verdict != OperatorJudgeVerdictConfirm && verdict.Verdict != OperatorJudgeVerdictRollback {
		return fmt.Errorf("genesis-tracker: invalid operator judge verdict %q", verdict.Verdict)
	}
	if verdict.CreatedAt == 0 {
		verdict.CreatedAt = time.Now().UnixMilli()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[judgeAccuracyRecord](t.judgeAccuracyLogPath())
	if err != nil {
		return fmt.Errorf("genesis-tracker: load judge accuracy: %w", err)
	}
	for _, entry := range entries {
		for _, existing := range entry.OperatorVerdicts {
			if existing.DecisionID == verdict.DecisionID {
				return nil
			}
		}
	}
	return jsonlstore.Append(t.judgeAccuracyLogPath(), judgeAccuracyRecord{
		CreatedAt:        verdict.CreatedAt,
		JudgeVersion:     verdict.JudgeVersion,
		OperatorVerdicts: []OperatorJudgeVerdict{verdict},
	})
}

// recentJudgeAccuracy returns the newest lane runs, newest first.
func (t *Tracker) recentJudgeAccuracy(limit int) ([]judgeAccuracyRecord, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	entries, err := jsonlstore.Load[judgeAccuracyRecord](t.judgeAccuracyLogPath())
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load judge accuracy: %w", err)
	}
	out := make([]judgeAccuracyRecord, 0, min(limit, len(entries)))
	for i := len(entries) - 1; i >= 0 && len(out) < limit; i-- {
		if entries[i].Pairs == 0 && len(entries[i].ByClass) == 0 && len(entries[i].FalseRejects) == 0 {
			continue // operator-only labels have their own query below
		}
		out = append(out, entries[i])
	}
	return out, nil
}

// RecentOperatorJudgeVerdicts returns human labels in newest-first order.
func (t *Tracker) RecentOperatorJudgeVerdicts(window time.Duration, limit int) []OperatorJudgeVerdict {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[judgeAccuracyRecord](t.judgeAccuracyLogPath())
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	var out []OperatorJudgeVerdict
	for i := len(entries) - 1; i >= 0; i-- {
		for j := len(entries[i].OperatorVerdicts) - 1; j >= 0; j-- {
			verdict := entries[i].OperatorVerdicts[j]
			if verdict.CreatedAt < cutoff {
				continue
			}
			out = append(out, verdict)
			if limit > 0 && len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// OperatorJudgeVerdictByDecisionID finds an already-settled card decision.
// Action handlers use it before any rollback side effect, so tapping the other
// choice later cannot reverse a verdict that already won.
func (t *Tracker) OperatorJudgeVerdictByDecisionID(decisionID string) (OperatorJudgeVerdict, bool) {
	decisionID = strings.TrimSpace(decisionID)
	if decisionID == "" {
		return OperatorJudgeVerdict{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[judgeAccuracyRecord](t.judgeAccuracyLogPath())
	if err != nil {
		return OperatorJudgeVerdict{}, false
	}
	for i := len(entries) - 1; i >= 0; i-- {
		for j := len(entries[i].OperatorVerdicts) - 1; j >= 0; j-- {
			if entries[i].OperatorVerdicts[j].DecisionID == decisionID {
				return entries[i].OperatorVerdicts[j], true
			}
		}
	}
	return OperatorJudgeVerdict{}, false
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
	rec := judgeAccuracyRecord{
		JudgeVersion: t.Meta.Version(generation.MetaSkillJudgeSystemPrompt, judgeFallback),
		ByClass:      map[string][2]int{},
		ByCategory:   map[string][2]int{},
	}

	entries := t.Evolver.catalogEntries()
	pairs := buildJudgeDegradationPairs(entries, judgeBenchMaxPairs*metaBenchScale())
	// Subtle single-line degradations produce the honest judge misses the
	// blatant meta-bench pairs never will — the actual P3 label food. Kept out
	// of the meta-judge promotion gate on purpose (see judge_subtle_degradations.go).
	pairs = append(pairs, buildSubtleJudgeDegradationPairs(entries, judgeBenchMaxPairs*metaBenchScale())...)
	// Probe curriculum ladder: once the incumbent judge fully outgrows the
	// drop tier, escalate to in-place weakening — otherwise the miss ledger
	// flatlines at zero and the evaluator epochs starve (P3 fuel).
	escalated := t.weakenTierUnlocked(rec.JudgeVersion)
	if escalated {
		pairs = append(pairs, buildWeakenJudgeDegradationPairs(entries, judgeBenchMaxPairs*metaBenchScale())...)
	}
	verdictErrors, consecutiveErrors := 0, 0
	for _, pair := range pairs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		v, err := verdict(ctx, judgePrompt, pair.Original, pair.Degraded)
		if err != nil {
			// Infra/parse call failures are not judge-quality labels — do not
			// count them in Pairs/ByClass or exhibit them as P3 fuel.
			verdictErrors++
			consecutiveErrors++
			if consecutiveErrors >= judgeAccuracyAbortAfterErrors {
				logger.Warn("judge-accuracy: aborting after consecutive verdict errors",
					"errors", verdictErrors, "scored", rec.Pairs, "error", err)
				break
			}
			continue
		}
		consecutiveErrors = 0
		rec.Pairs++
		cls := rec.ByClass[pair.Degradation]
		cls[1]++
		category := pair.Category
		if category == "" {
			category = "(uncategorized)"
		}
		cat := rec.ByCategory[category]
		cat[1]++
		switch {
		case v.Pass:
			rec.Misses = append(rec.Misses, judgeMissExhibit{Skill: pair.Skill, Degradation: pair.Degradation, Verdict: "passed_defect"})
		case v.OriginalScore != nil && v.CandidateScore != nil && *v.CandidateScore > *v.OriginalScore:
			rec.Misses = append(rec.Misses, judgeMissExhibit{Skill: pair.Skill, Degradation: pair.Degradation, Verdict: "score_inverted"})
		default:
			rec.Correct++
			cls[0]++
			cat[0]++
		}
		rec.ByClass[pair.Degradation] = cls
		rec.ByCategory[category] = cat
	}
	if len(rec.Misses) > judgeAccuracyMaxExhibits {
		rec.Misses = rec.Misses[:judgeAccuracyMaxExhibits]
	}

	rec.FalseRejects = t.mineFalseRejects()

	if rec.Pairs == 0 && len(rec.FalseRejects) == 0 {
		if verdictErrors > 0 {
			logger.Warn("judge-accuracy: skipped ledger — verdict calls failed before any scored pair",
				"errors", verdictErrors)
		}
		return nil // nothing to ledger — corpus too thin or infra-only outage
	}
	if err := t.Tracker.logJudgeAccuracy(rec); err != nil {
		logger.Warn("judge-accuracy: ledger write failed", "error", err)
		return nil
	}
	logger.Info("judge-accuracy: lane run ledgered (P3 label food)",
		"pairs", rec.Pairs, "correct", rec.Correct, "misses", len(rec.Misses),
		"falseRejects", len(rec.FalseRejects), "judgeVersion", rec.JudgeVersion,
		"weakenTier", escalated, "verdictErrors", verdictErrors)
	return nil
}

// weakenTierUnlocked reports drop-tier saturation for the incumbent judge:
// the newest judgeEscalationWindow lane runs attributed to judgeVersion each
// carried at least one drop-tier pair and recorded zero drop-tier misses.
// Fewer incumbent runs (including right after a judge revision) or any
// drop-tier miss keeps the harder tier locked. Uses ByClass counts, which are
// complete — the Misses exhibit list is capped and unusable for this.
func (t *JudgeAccuracyTask) weakenTierUnlocked(judgeVersion string) bool {
	records, err := t.Tracker.recentJudgeAccuracy(judgeEscalationWindow * 4)
	if err != nil || judgeVersion == "" {
		return false
	}
	saturated := 0
	for _, rec := range records { // newest first
		if rec.JudgeVersion != judgeVersion {
			continue
		}
		if rec.Pairs == 0 && len(rec.ByClass) == 0 {
			continue // operator-only label, not a probe-curriculum run
		}
		if !judgeAccuracyProbeUsable(rec) {
			continue // infra outage rows must not re-lock the soften ladder
		}
		pairsSeen, missed := 0, 0
		for _, cls := range subtleJudgeDegradations {
			ct := rec.ByClass[cls.name]
			pairsSeen += ct[1]
			missed += ct[1] - ct[0]
		}
		if pairsSeen == 0 || missed > 0 {
			return false
		}
		saturated++
		if saturated >= judgeEscalationWindow {
			return true
		}
	}
	return false
}

// mineFalseRejects scores buffered rejected candidates against the CURRENT
// skill body on stored validation cases. Deterministic; flags only a strict,
// flip-free improvement beyond the margin.
func (t *JudgeAccuracyTask) mineFalseRejects() []falseRejectExhibit {
	var out []falseRejectExhibit
	for _, entry := range t.Evolver.catalogEntries() {
		skill := entry.Skill.Name
		cases, err := t.Tracker.RecentSkillValidationCases(skill, producerBenchCaseLimit)
		if err != nil {
			continue
		}
		// Charter exclusion (P3 precondition #3): false-reject exhibits are a
		// verifier co-evolution training surface, so the frozen charter slice
		// must NOT feed them — it stays a held-out measuring stick the judge
		// never trains against. This is the first live consumer to honor the
		// isCharterCase contract (RSI code eval M5). Benches still SCORE charter
		// cases elsewhere; only this training-surface read excludes them.
		cases = excludeCharterCases(cases)
		if !hasScorableValidationCase(cases) {
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
			if rjs.percent() >= cur.percent()+falseRejectMargin {
				out = append(out, falseRejectExhibit{
					Skill:        skill,
					RejectReason: common.TruncateRunes(rej.Reason, 160),
					CurrentScore: cur.percent(),
					RejectScore:  rjs.percent(),
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
