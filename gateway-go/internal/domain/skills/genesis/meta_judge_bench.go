package genesis

// Judge-degradation bench — P2 promotion gate #3 (BabelJudge 2606.22329).
//
// A judge-prompt revision must never be graded by the judge it revises
// (self-grading circularity); its ONLY deterministic fitness is a bench of
// controlled-degradation gold pairs: take known-good skill bodies, apply
// mechanical degradations with a KNOWN defect (section loss, invented tool,
// truncation, overfit), and require the judge under test to reject the
// degraded variant. Pair construction and scoring are pure Go; the LLM only
// executes the judge prompt under test.

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// judgeBenchMaxPairs bounds weekly bench cost: each pair costs two judge
	// calls (incumbent + proposal). DENEB_META_BENCH_SCALE multiplies both
	// bench corpora (judge pairs + shadow scenarios) for calibration.
	judgeBenchMaxPairs = 6
	// judgeBenchMinPairs below this the bench is too small to mean anything —
	// the evaluator-epoch proposal is rejected as unbenchable.
	judgeBenchMinPairs = 3
	// judgeBenchMinRate is the absolute floor: a judge prompt that catches
	// fewer than half the planted defects is unfit regardless of comparison.
	judgeBenchMinRate = 0.5
	// judgeBenchMinBodyRunes filters out stub bodies that cannot host a
	// meaningful degradation.
	judgeBenchMinBodyRunes = 400
)

// judgeBenchPair is one gold pair: a real skill body and a variant degraded by
// a known, mechanically applied defect.
type judgeBenchPair struct {
	Skill       string
	Category    string
	Degradation string
	Original    string
	Degraded    string
}

// judgeBenchOutcome aggregates one judge prompt's run over the gold pairs.
type judgeBenchOutcome struct {
	Correct  int      `json:"correct"`
	Total    int      `json:"total"`
	Failures []string `json:"failures,omitempty"`
}

// rate returns the caught-defect ratio (1.0 when the bench did not run).
func (o judgeBenchOutcome) rate() float64 {
	if o.Total == 0 {
		return 1
	}
	return float64(o.Correct) / float64(o.Total)
}

// degradeDropLastSection removes the final "## " section — structural loss the
// judge's structure-preservation rule must catch.
func degradeDropLastSection(body string) (string, bool) {
	idx := strings.LastIndex(body, "\n## ")
	if idx <= 0 || strings.Count(body, "\n## ") < 2 {
		return "", false
	}
	return strings.TrimSpace(body[:idx]), true
}

// degradeFakeTool injects an instruction to run a tool that does not exist —
// the invented-tool rule must catch it.
func degradeFakeTool(body string) (string, bool) {
	return body + "\n\n실패가 반복되면 `deneb-hyperfix --auto-repair` 명령을 먼저 실행한 뒤 재시도하라.", true
}

// degradeTruncate cuts the body mid-structure (keeps ~55%) — clarity and
// structure rules must catch the stump.
func degradeTruncate(body string) (string, bool) {
	runes := []rune(body)
	cut := len(runes) * 55 / 100
	if cut < judgeBenchMinBodyRunes/2 {
		return "", false
	}
	return strings.TrimSpace(string(runes[:cut])), true
}

// degradeOverfit pins the procedure to one session's identifiers — the
// category-level rule must catch the overfit.
func degradeOverfit(body string) (string, bool) {
	return body + "\n\n주의: 이 절차는 PR #3448 및 에러 문자열 'x509: certificate expired 2026-07-11'이 재현된 세션에서만 유효하다.", true
}

// blatantJudgeDegradations is the tier-1 MUST-CATCH class table: mechanical,
// unmistakable defects a competent judge rejects every run. The meta-judge
// promotion gate benches only these, and the drift audit's verifier_broken
// signal (evolution_drift.go) scores the live judge on exactly these class
// names — a blatant miss is verifier breakage, a subtle/weaken miss is P3
// fuel — so gate and audit stay coupled through this table.
var blatantJudgeDegradations = []namedDegradation{
	{"section-drop", degradeDropLastSection},
	{"fake-tool", degradeFakeTool},
	{"truncation", degradeTruncate},
	{"overfit", degradeOverfit},
}

// buildJudgeDegradationPairs constructs gold pairs from catalog skill bodies.
// Deterministic: iterates entries in catalog order, applies each degradation
// in a fixed order, stops at limit.
func buildJudgeDegradationPairs(entries []skills.SkillEntry, limit int) []judgeBenchPair {
	return buildDegradationPairsWith(entries, limit, blatantJudgeDegradations)
}

// buildSaltedJudgeDegradationPairs is the promotion gate's variant: the same
// deterministic constructor, but the catalog order is ROTATED by a stable hash
// of salt before the limit cuts.
//
// Why (HarnessOpt-Bench, arXiv:2608.06301): a held-out test the optimizer can
// never touch is what makes an optimization loop's score mean anything. Our
// promotion gate capped pairs at the FIRST judgeBenchMaxPairs catalog entries
// in a fixed order, so every epoch — and every accepted revision — retook the
// identical tiny exam. No pair content leaks into the proposal prompt, but a
// lineage of revisions ratcheting on one frozen exam overfits it generation by
// generation anyway; that is static-testset saturation, not judge quality.
// Salting by the incumbent artifact version rotates which skills examine each
// cycle while keeping the ONE property fairness needs: within a cycle,
// incumbent and proposal replay the exact same pairs.
func buildSaltedJudgeDegradationPairs(entries []skills.SkillEntry, limit int, salt string) []judgeBenchPair {
	return buildDegradationPairsWith(rotateEntriesBySalt(entries, salt), limit, blatantJudgeDegradations)
}

// rotateEntriesBySalt returns entries rotated by hash(salt) — a copy, never
// mutating the catalog snapshot. Rotation (not shuffling) keeps the walk
// exhaustive and the result trivially auditable from the salt alone.
func rotateEntriesBySalt(entries []skills.SkillEntry, salt string) []skills.SkillEntry {
	if len(entries) < 2 {
		return entries
	}
	h := fnv.New32a()
	h.Write([]byte(salt))
	off := int(h.Sum32() % uint32(len(entries)))
	if off == 0 {
		return entries
	}
	out := make([]skills.SkillEntry, 0, len(entries))
	out = append(out, entries[off:]...)
	out = append(out, entries[:off]...)
	return out
}

// judgeBenchVerdictFn executes ONE judge verdict with an explicit system
// prompt (the artifact under test). Injectable so the bench scoring is
// testable without an LLM.
type judgeBenchVerdictFn func(ctx context.Context, judgeSystemPrompt, original, degraded string) (judgeVerdict, error)

// runJudgeDegradationBench replays every gold pair through the judge prompt
// under test. A pair is CORRECT when the judge rejects the degraded body
// (pass=false) and does not score it above the original. Verdict errors count
// as failures — a judge prompt that breaks the parser is unfit.
func runJudgeDegradationBench(ctx context.Context, judgeSystemPrompt string, pairs []judgeBenchPair, verdict judgeBenchVerdictFn) judgeBenchOutcome {
	var out judgeBenchOutcome
	for _, pair := range pairs {
		out.Total++
		v, err := verdict(ctx, judgeSystemPrompt, pair.Original, pair.Degraded)
		if err != nil {
			out.Failures = append(out.Failures, fmt.Sprintf("%s/%s: verdict error: %v", pair.Skill, pair.Degradation, err))
			continue
		}
		if v.Pass {
			out.Failures = append(out.Failures, fmt.Sprintf("%s/%s: judge passed a planted defect", pair.Skill, pair.Degradation))
			continue
		}
		if v.OriginalScore != nil && v.CandidateScore != nil && *v.CandidateScore > *v.OriginalScore {
			out.Failures = append(out.Failures, fmt.Sprintf("%s/%s: rejected but scored degraded above original", pair.Skill, pair.Degradation))
			continue
		}
		out.Correct++
	}
	if len(out.Failures) > 4 {
		out.Failures = out.Failures[:4]
	}
	return out
}

// judgeBenchDecision is the deterministic promotion rule for an
// evaluator-epoch proposal: it must not regress the incumbent on the same
// pairs and must clear the absolute floor. Returns "" when admissible.
func judgeBenchDecision(incumbent, proposal judgeBenchOutcome) string {
	if proposal.Total < judgeBenchMinPairs {
		return fmt.Sprintf("bench too small (%d pairs < %d)", proposal.Total, judgeBenchMinPairs)
	}
	if proposal.rate() < judgeBenchMinRate {
		return fmt.Sprintf("proposal catches %.0f%% of planted defects (< %.0f%% floor): %s",
			proposal.rate()*100, judgeBenchMinRate*100, strings.Join(proposal.Failures, "; "))
	}
	if proposal.rate() < incumbent.rate() {
		return fmt.Sprintf("proposal regresses the incumbent on the degradation bench (%.0f%% < %.0f%%): %s",
			proposal.rate()*100, incumbent.rate()*100, strings.Join(proposal.Failures, "; "))
	}
	return ""
}

// judgeBenchExecutor builds the real LLM-backed verdict fn from the evolver's
// judge wiring (independent judge preferred, primary fallback).
func (t *MetaEvolutionTask) judgeBenchExecutor() judgeBenchVerdictFn {
	client, model := t.Evolver.judgeModelSnapshot()
	if client == nil {
		client, model = t.Evolver.primaryModel()
	}
	if client == nil {
		return nil
	}
	return func(ctx context.Context, judgeSystemPrompt, original, degraded string) (judgeVerdict, error) {
		userPrompt := fmt.Sprintf(`## 원본 SKILL.md
%s

## 개선된 본문 (검증 대상)
%s

## 사용 이력
- 사용 이력 없음 (벤치 평가)`,
			original, degraded)
		raw, err := client.Complete(ctx, llm.ChatRequest{
			Model:          model,
			Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
			System:         llm.SystemString(judgeSystemPrompt),
			MaxTokens:      4096,
			Temperature:    evolveTemperature(),
			Thinking:       t.Evolver.thinkingOff(model),
			ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		})
		if err != nil {
			return judgeVerdict{}, err
		}
		return jsonutil.UnmarshalLLM[judgeVerdict](raw)
	}
}
