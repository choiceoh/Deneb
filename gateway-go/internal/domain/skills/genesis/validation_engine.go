package genesis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/guardrails"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

const (
	defaultSkillValidationCaseLimit = 20
	skillHeldOutMinScoreDelta       = 1.0
)

// SkillValidationEngine is the selector-side held-out gate for skill evolution.
// It is deliberately separate from Evolver so deterministic invariants and
// dry-run replay checks can evolve without changing candidate generation.
type SkillValidationEngine struct {
	tracker       *Tracker
	logger        *slog.Logger
	caseLimit     int
	minScoreDelta float64

	// executor is the optional behavioral-replay model (set via SetExecutor).
	// nil → the behavioral gate is disabled and EvaluateBehavior fails open.
	// Guarded because SetExecutor (startup/reconfig) can race an in-flight evolve.
	mu            sync.RWMutex
	executor      *llm.Client
	executorModel string
}

// replayBehaviorMaxCases bounds how many replay cases the behavioral gate runs
// per evolve. Each case costs three executor calls (the original twice plus the
// candidate), so the cap keeps a background evolve cycle from ballooning into
// many LLM calls. The original runs twice on purpose — see EvaluateBehavior's
// self-consistency baseline.
const replayBehaviorMaxCases = 5

// SkillBehaviorResult reports the execution-grounded comparison of a candidate
// rewrite against the original on stored replay cases. Evaluated is false when
// the gate did not run (no executor / no cases / executor error) — callers must
// treat that as a pass (fail-open), never a block.
type SkillBehaviorResult struct {
	Evaluated       bool     `json:"evaluated"`
	Pass            bool     `json:"pass"`
	Reason          string   `json:"reason,omitempty"`
	CaseCount       int      `json:"caseCount,omitempty"`
	OriginalPassed  int      `json:"originalPassed,omitempty"`
	OriginalTotal   int      `json:"originalTotal,omitempty"`
	CandidatePassed int      `json:"candidatePassed,omitempty"`
	CandidateTotal  int      `json:"candidateTotal,omitempty"`
	Failures        []string `json:"failures,omitempty"`
	// OriginalRepeatPassed is the second scoring of the UNCHANGED original body,
	// and ReplayNoise the gap between the two. They measure the instrument, not
	// the candidate: anything inside that gap is simulation variance.
	OriginalRepeatPassed int `json:"originalRepeatPassed,omitempty"`
	ReplayNoise          int `json:"replayNoise,omitempty"`
}

// SkillValidationResult describes original-vs-candidate performance on
// persisted held-out validation cases.
type SkillValidationResult struct {
	Evaluated       bool     `json:"evaluated"`
	Pass            bool     `json:"pass"`
	Reason          string   `json:"reason,omitempty"`
	CaseCount       int      `json:"caseCount,omitempty"`
	OriginalPassed  int      `json:"originalPassed,omitempty"`
	OriginalTotal   int      `json:"originalTotal,omitempty"`
	CandidatePassed int      `json:"candidatePassed,omitempty"`
	CandidateTotal  int      `json:"candidateTotal,omitempty"`
	OriginalScore   float64  `json:"originalScore,omitempty"`
	CandidateScore  float64  `json:"candidateScore,omitempty"`
	Failures        []string `json:"failures,omitempty"`
	// FlippedCases lists held-out cases the original passed but the candidate
	// fails — promotion-blocking regardless of aggregate score (flip gate).
	FlippedCases []string `json:"flippedCases,omitempty"`
}

// NewSkillValidationEngine constructs the deterministic skill validation engine.
func NewSkillValidationEngine(tracker *Tracker, logger *slog.Logger) *SkillValidationEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &SkillValidationEngine{
		tracker:       tracker,
		logger:        logger,
		caseLimit:     defaultSkillValidationCaseLimit,
		minScoreDelta: skillHeldOutMinScoreDelta,
	}
}

// SetExecutor wires the optional behavioral-replay executor: a model that
// simulates the production agent following a skill so EvaluateBehavior can score
// the candidate's tool-call behavior. Passing nil disables the behavioral gate.
func (v *SkillValidationEngine) SetExecutor(client *llm.Client, model string) {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.executor = client
	v.executorModel = strings.TrimSpace(model)
}

// heldOutCases loads the cases both gates score: the blind held-out pool the
// producer prompt never sees, so a candidate cannot pass by echoing assertions
// it was shown (docs/research/skillhone-2606.08671.md §3-1). Tiny-corpus
// fallback: while a skill has no blind-pool case yet, gate on all cases —
// contract-compliance scoring beats an inert gate, and matches the pre-split
// behavior.
func (v *SkillValidationEngine) heldOutCases(skillName string) ([]SkillValidationCaseRecord, error) {
	limit := v.caseLimit
	if limit <= 0 {
		limit = defaultSkillValidationCaseLimit
	}
	cases, err := v.tracker.recentSkillValidationCasesPool(skillName, limit, true)
	if err != nil || len(cases) > 0 {
		return cases, err
	}
	return v.tracker.RecentSkillValidationCases(skillName, limit)
}

func (v *SkillValidationEngine) executorSnapshot() (*llm.Client, string) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.executor, v.executorModel
}

// EvaluateBehavior replays the original and candidate skill bodies through the
// executor model on stored replay cases and rejects a candidate that REGRESSES
// the proven tool-call behavior. It is a do-no-harm safety net orthogonal to the
// LLM self-test/judge (which assesses overall improvement): a rewrite that drops
// or reorders a required tool call is caught even if it "reads" better, because
// the executor is asked what the skill would make the agent DO, not how it looks.
//
// Fail-open: no executor, no behavior-evaluable cases, or any executor/parse
// error returns an un-evaluated pass (Evaluated=false). A flaky simulation must
// never block a real improvement — the same doctrine as the goal-loop judge.
func (v *SkillValidationEngine) EvaluateBehavior(ctx context.Context, skillName, originalBody, candidateBody string) (SkillBehaviorResult, error) {
	if v == nil || v.tracker == nil {
		return SkillBehaviorResult{}, nil
	}
	executor, model := v.executorSnapshot()
	if executor == nil {
		return SkillBehaviorResult{}, nil
	}
	cases, err := v.heldOutCases(skillName)
	if err != nil {
		return SkillBehaviorResult{}, err
	}
	evaluable := replayBehaviorCases(cases, replayBehaviorMaxCases)
	if len(evaluable) == 0 {
		return SkillBehaviorResult{}, nil
	}

	runScored := func(body string, tc SkillValidationCaseRecord, who string) (validationCaseScore, bool) {
		trace, err := v.runReplayExecutorWith(ctx, executor, model, body, tc.Replay)
		if err != nil {
			if v.logger != nil {
				// Name the CASE, not just the skill. The failure is almost always
				// a property of one case (an input the skill cannot act on), and
				// without an id nobody can find which one to repair — the 2026-08
				// journal carried eight of these across five skills with no way to
				// tell them apart.
				v.logger.Warn("genesis: replay case not executable",
					"skill", skillName, "case", replayCaseLabel(tc), "role", who, "error", err)
			}
			return validationCaseScore{}, false
		}
		return scoreReplayAgainstTrace(trace, tc), true
	}

	// The original is scored TWICE on the same body. The two runs differ only by
	// executor nondeterminism, so their gap is this instrument's own precision —
	// measured per evolve rather than assumed.
	// A case that cannot be executed is dropped from ALL THREE runs and the rest
	// of the set still scores.
	//
	// This used to abort the whole gate — one unusable case returned an empty,
	// un-evaluated result for the skill. That is how a single ill-posed replay
	// case silently disabled a skill's behavioral gate for good: the executor
	// answered "실제 YouTube URL이나 영상 요청 없이 …" (the case input names no
	// video), the plan parse failed, and youtube-summary-cards stopped being
	// gate-checked entirely. Measured 2026-08-26 the lifecycle ledger read
	// evolved=0 across every skill.
	//
	// Dropping per case, not per skill, keeps the comparison honest: original,
	// original-repeat, and candidate must score the SAME case set or their
	// delta means nothing, so a failure in any of the three drops that case from
	// all of them. If every case drops, cand.Total stays 0 and the result is
	// un-evaluated exactly as before — a gate with no cases still must not pass.
	var origA, origB, cand validationCaseScore
	var skipped int
	for _, tc := range evaluable {
		a, aok := runScored(originalBody, tc, "original")
		b, bok := runScored(originalBody, tc, "original-repeat")
		c, cok := runScored(candidateBody, tc, "candidate")
		if !aok || !bok || !cok {
			skipped++
			continue
		}
		origA.add(a)
		origB.add(b)
		cand.add(c)
	}
	if skipped > 0 && v.logger != nil {
		v.logger.Warn("genesis: replay cases unusable, scored the remainder",
			"skill", skillName, "skipped", skipped, "scored", len(evaluable)-skipped)
	}

	noise := origA.Passed - origB.Passed
	if noise < 0 {
		noise = -noise
	}
	// The bar is the original's own WORST observed run. A candidate that lands
	// at or above it has not been shown to break anything: the unchanged body
	// scored there too.
	floor := min(origA.Passed, origB.Passed)

	result := SkillBehaviorResult{
		Evaluated:            cand.Total > 0,
		Pass:                 true,
		CaseCount:            len(evaluable),
		OriginalPassed:       origA.Passed,
		OriginalTotal:        origA.Total,
		OriginalRepeatPassed: origB.Passed,
		ReplayNoise:          noise,
		CandidatePassed:      cand.Passed,
		CandidateTotal:       cand.Total,
		Failures:             cand.Failures,
	}
	if cand.Total == 0 {
		result.Evaluated = false
		return result, nil
	}
	// Regression-only gate: the candidate must not match FEWER tool-call
	// assertions than the original's worst run. Requiring strict improvement
	// here would wrongly block legitimate non-behavioral edits (a clarified
	// pitfall, a fixed path) that preserve the same correct tool plan — the LLM
	// judge owns the "is it better" question; this owns "did it break what
	// worked".
	//
	// Comparing against a SINGLE original run is what jammed this gate shut:
	// every rejection between 2026-07-12 and 08-02 was a 1-3 assertion gap on a
	// 30-45 assertion set whose original itself only satisfied 15-63%, i.e.
	// inside the executor's own run-to-run spread. Zero evolutions landed for
	// two weeks, which starved the PACE/CoVerRL resolved-lifecycle samples and
	// dropped RSI Process onto its bootstrap floor.
	if cand.Passed < floor {
		result.Pass = false
		result.Reason = fmt.Sprintf(
			"behavioral replay regressed: candidate matched %d/%d tool-call assertions, below the original's worst of two runs (%d and %d of %d, replay noise %d): %s",
			cand.Passed, cand.Total, origA.Passed, origB.Passed, origA.Total, noise,
			formatValidationFailures(cand.Failures),
		)
	}
	return result, nil
}

// replayBehaviorEvaluable reports whether a replay case can be executed: it needs
// a user task to simulate and at least one assertion to score the resulting plan.
func replayBehaviorEvaluable(r SkillReplayCaseRecord) bool {
	return strings.TrimSpace(r.Input) != "" && r.hasAssertions()
}

func replayBehaviorCases(cases []SkillValidationCaseRecord, limit int) []SkillValidationCaseRecord {
	if limit <= 0 {
		limit = len(cases)
	}
	evaluable := make([]SkillValidationCaseRecord, 0, min(limit, len(cases)))
	for _, testCase := range cases {
		if !replayBehaviorEvaluable(testCase.Replay) {
			continue
		}
		evaluable = append(evaluable, testCase)
		if len(evaluable) >= limit {
			break
		}
	}
	return evaluable
}

// ValidateCandidate runs selector-side held-out validation. No stored cases is a
// pass with Evaluated=false; unavailable storage returns an error so the caller
// can decide fail-open/fail-closed.
func (v *SkillValidationEngine) ValidateCandidate(skillName, originalContent, candidateBody string) (SkillValidationResult, error) {
	if v == nil || v.tracker == nil {
		return SkillValidationResult{Pass: true}, nil
	}
	cases, err := v.heldOutCases(skillName)
	if err != nil {
		return SkillValidationResult{}, err
	}
	if len(cases) == 0 {
		return SkillValidationResult{Pass: true}, nil
	}

	origByCase := scoreSkillValidationCasesByCase(skillBodyOnly(originalContent), cases)
	candByCase := scoreSkillValidationCasesByCase(candidateBody, cases)
	var orig, cand validationCaseScore
	for i := range origByCase {
		orig.add(origByCase[i])
		cand.add(candByCase[i])
	}
	if len(orig.Failures) > 3 {
		orig.Failures = orig.Failures[:3]
	}
	if len(cand.Failures) > 3 {
		cand.Failures = cand.Failures[:3]
	}
	if cand.Skipped > 0 && v.logger != nil {
		v.logger.Warn("skill validation: non-discriminative assertions isolated from scoring",
			"skill", skillName, "skipped", cand.Skipped)
	}
	result := SkillValidationResult{
		Evaluated:       cand.Total > 0,
		Pass:            true,
		CaseCount:       len(cases),
		OriginalPassed:  orig.Passed,
		OriginalTotal:   orig.Total,
		CandidatePassed: cand.Passed,
		CandidateTotal:  cand.Total,
		OriginalScore:   orig.percent(),
		CandidateScore:  cand.percent(),
		Failures:        cand.Failures,
	}
	if cand.Total == 0 {
		return result, nil
	}
	// Flip gate (RSI P1.5, AgentDevel 2601.04620): any case the original passes
	// and the candidate fails blocks promotion outright — aggregate gains
	// elsewhere must not buy back a regression on proven behavior.
	var flipped []string
	for i := range origByCase {
		if origByCase[i].Total > 0 && origByCase[i].casePasses() && !candByCase[i].casePasses() {
			flipped = append(flipped, validationCaseLabel(cases[i]))
		}
	}
	if len(flipped) > 0 {
		result.FlippedCases = flipped
		result.Pass = false
		shown := flipped
		if len(shown) > 3 {
			shown = shown[:3]
		}
		result.Reason = fmt.Sprintf("flip gate rejected: candidate regressed %d previously-passing held-out case(s) (%s): %s",
			len(flipped), strings.Join(shown, ", "), formatValidationFailures(cand.Failures))
		return result, nil
	}
	if cand.Passed < orig.Passed {
		result.Pass = false
		result.Reason = fmt.Sprintf("held-out selection rejected: candidate regressed validation cases (%d/%d vs original %d/%d): %s",
			cand.Passed, cand.Total, orig.Passed, orig.Total, formatValidationFailures(cand.Failures))
		return result, nil
	}
	minDelta := v.minScoreDelta
	if minDelta <= 0 {
		minDelta = skillHeldOutMinScoreDelta
	}
	if result.OriginalScore < 100 && result.CandidateScore-result.OriginalScore < minDelta {
		result.Pass = false
		result.Reason = fmt.Sprintf("held-out selection rejected: candidate did not improve validation score enough (%.1f vs original %.1f): %s",
			result.CandidateScore, result.OriginalScore, formatValidationFailures(cand.Failures))
		return result, nil
	}
	return result, nil
}

// skillCrossRegressionResult reports a neighbor skill scored against the evolved
// skill's held-out forbidden/required assertions (#4). Failed is true when the
// neighbor body violates at least one of those assertions — a coupling signal
// surfaced for observability, never a rollback trigger.
type skillCrossRegressionResult struct {
	NeighborSkill string   `json:"neighborSkill"`
	Failed        bool     `json:"failed"`
	Passed        int      `json:"passed"`
	Total         int      `json:"total"`
	Failures      []string `json:"failures,omitempty"`
}

// crossSkillRegression scores a neighbor skill's body against the evolved skill's
// held-out validation cases (#4 cross-skill regression detection). It is the
// deterministic, non-LLM scorer behind the post-commit neighbor sweep: the same
// forbidden-substring / forbidden-tool / required-assertion contract distilled
// from the evolved skill's real failures is replayed against a similar neighbor,
// so an edit that, say, newly forbids `eval` can flag a neighbor that still
// relies on it. Pure function of (cases, neighborBody) — caller owns neighbor
// selection and the no-cases / no-neighbors no-op.
func crossSkillRegression(neighborSkill, neighborBody string, cases []SkillValidationCaseRecord) skillCrossRegressionResult {
	score := scoreSkillValidationCases(skillBodyOnly(neighborBody), cases)
	return skillCrossRegressionResult{
		NeighborSkill: neighborSkill,
		Failed:        score.Total > 0 && score.Passed < score.Total,
		Passed:        score.Passed,
		Total:         score.Total,
		Failures:      score.Failures,
	}
}

type validationCaseScore struct {
	Passed   int
	Total    int
	Failures []string
	// Skipped counts non-discriminative assertions isolated from Total: an
	// assertion that normalizes to empty text is body-independent (an empty
	// forbidden/heading always fails, an empty required always passes), so
	// counting it either wedges the min-delta gate permanently — original
	// score pinned below 100 by an unfixable assertion rejects every future
	// candidate — or inflates both scores for free (RSI P1.5 ④, verifier
	// fuzzing 2606.01066).
	Skipped int
}

// percent returns the score normalized as a percentage.
func (s validationCaseScore) percent() float64 {
	if s.Total == 0 {
		return 100
	}
	return float64(s.Passed) * 100 / float64(s.Total)
}

func scoreSkillValidationCases(body string, cases []SkillValidationCaseRecord) validationCaseScore {
	var score validationCaseScore
	for _, caseScore := range scoreSkillValidationCasesByCase(body, cases) {
		score.add(caseScore)
	}
	if len(score.Failures) > 3 {
		score.Failures = score.Failures[:3]
	}
	return score
}

// scoreSkillValidationCasesByCase scores each case independently, parallel to
// cases. Per-case granularity is what the flip gate compares: a case "passes"
// when every scorable assertion in it passes (vacuously if none are scorable).
// Assertion Totals depend only on the case, never the body, so original and
// candidate outcomes for the same index are directly comparable.
func scoreSkillValidationCasesByCase(body string, cases []SkillValidationCaseRecord) []validationCaseScore {
	scores := make([]validationCaseScore, 0, len(cases))
	normalizedBody := normalizedValidationText(body)
	headings := map[string]struct{}{}
	for _, heading := range guardrails.NormalizedSkillHeadings(body) {
		headings[heading] = struct{}{}
	}
	for _, tc := range cases {
		var score validationCaseScore
		label := validationCaseLabel(tc)
		for _, required := range tc.RequiredSubstrings {
			if normalizedValidationText(required) == "" {
				score.Skipped++
				continue
			}
			score.Total++
			if containsNormalizedValidationText(normalizedBody, required) {
				score.Passed++
				continue
			}
			score.Failures = append(score.Failures, fmt.Sprintf("%s missing required substring %q", label, genesiscommon.TruncateRunes(required, 80)))
		}
		for _, forbidden := range tc.ForbiddenSubstrings {
			if normalizedValidationText(forbidden) == "" {
				score.Skipped++
				continue
			}
			score.Total++
			if !containsNormalizedValidationText(normalizedBody, forbidden) {
				score.Passed++
				continue
			}
			score.Failures = append(score.Failures, fmt.Sprintf("%s contains forbidden substring %q", label, genesiscommon.TruncateRunes(forbidden, 80)))
		}
		for _, required := range tc.RequiredHeadings {
			normalizedHeading := strings.ToLower(strings.Join(strings.Fields(required), " "))
			if normalizedHeading == "" {
				score.Skipped++
				continue
			}
			score.Total++
			if _, ok := headings[normalizedHeading]; ok {
				score.Passed++
				continue
			}
			score.Failures = append(score.Failures, fmt.Sprintf("%s missing required heading %q", label, genesiscommon.TruncateRunes(required, 80)))
		}
		score.add(scoreSkillReplayCase(body, tc))
		scores = append(scores, score)
	}
	return scores
}

func (s validationCaseScore) casePasses() bool {
	return s.Passed == s.Total // vacuous pass when no scorable assertion
}

func (s *validationCaseScore) add(other validationCaseScore) {
	s.Passed += other.Passed
	s.Total += other.Total
	s.Skipped += other.Skipped
	s.Failures = append(s.Failures, other.Failures...)
}

func validationCaseLabel(tc SkillValidationCaseRecord) string {
	label := strings.TrimSpace(tc.ID)
	if label == "" {
		label = strings.TrimSpace(tc.Description)
	}
	if label == "" {
		label = strings.TrimSpace(tc.SkillName)
	}
	return label
}

func formatValidationFailures(failures []string) string {
	if len(failures) == 0 {
		return "no failing assertion, but score did not improve"
	}
	return strings.Join(failures, "; ")
}

func skillBodyOnly(content string) string {
	if _, bodyOffset := skills.ExtractFrontmatterBlock(content); bodyOffset > 0 && bodyOffset < len(content) {
		return content[bodyOffset:]
	}
	return content
}

// replayCaseLabel identifies a validation case in a log line: its id when it
// has one, else a bounded head of the replay input so the case is still
// findable in the corpus.
func replayCaseLabel(tc SkillValidationCaseRecord) string {
	if id := strings.TrimSpace(tc.ID); id != "" {
		return id
	}
	head := strings.TrimSpace(tc.Replay.Input)
	r := []rune(strings.ReplaceAll(head, "\n", " "))
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	if len(r) == 0 {
		return "(빈 입력)"
	}
	return string(r)
}
