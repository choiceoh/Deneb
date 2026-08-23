package genesis

// Producer shadow-replay bench — P2 promotion gates #1/#2 (CPE frozen-anchor
// preservation + AgentDevel shadow-replay flip) for PRODUCER-prompt revisions.
//
// A producer-prompt revision changes how every future candidate is written, so
// its fitness is measured by what it actually produces: replay the same evolve
// scenario (same skill, same user prompt) through the incumbent and the
// proposed system prompt, score both generated candidates against the skill's
// stored validation cases, and reject the proposal when its candidate flips a
// case the incumbent's candidate passes (anchors included — they live in the
// same corpus) or meaningfully regresses the mean score. Scenario construction
// and scoring are deterministic Go; the LLM only executes the two prompts
// under comparison.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// producerBenchMaxSkills bounds weekly bench cost: each scenario costs two
	// producer generations (incumbent + proposal).
	producerBenchMaxSkills = 3
	// producerBenchCaseLimit is how many stored validation cases score each
	// generated candidate.
	producerBenchCaseLimit = 20
	// producerBenchScoreEpsilon tolerates generation noise on the mean score;
	// flips have zero tolerance.
	producerBenchScoreEpsilon = 5.0
)

// producerShadowScenario is one replayable evolve scenario.
type producerShadowScenario struct {
	Skill      string
	UserPrompt string
	Cases      []SkillValidationCaseRecord
}

// producerBenchOutcome aggregates the shadow replay over all scenarios.
type producerBenchOutcome struct {
	Skills         int      `json:"skills"` // scenarios where BOTH prompts produced a scorable candidate
	Flips          int      `json:"flips"`
	IncumbentScore float64  `json:"incumbentScore"` // mean held-out percent
	ProposalScore  float64  `json:"proposalScore"`
	Notes          []string `json:"notes,omitempty"`
}

// buildProducerShadowScenarios picks up to limit skills that have scorable
// validation cases and assembles a compact, deterministic evolve prompt for
// each. Catalog order keeps selection stable across runs.
func buildProducerShadowScenarios(entries []skills.SkillEntry, tracker *Tracker, limit int) []producerShadowScenario {
	if limit <= 0 {
		limit = producerBenchMaxSkills
	}
	scenarios := make([]producerShadowScenario, 0, limit)
	for _, entry := range entries {
		if len(scenarios) >= limit {
			break
		}
		if tracker == nil {
			break
		}
		cases, err := tracker.RecentSkillValidationCases(entry.Skill.Name, producerBenchCaseLimit)
		if err != nil || !hasScorableValidationCase(cases) {
			continue
		}
		raw, err := os.ReadFile(entry.Skill.FilePath)
		if err != nil {
			continue
		}
		stats, _ := tracker.evolutionEvidenceStats(entry.Skill.Name)
		if stats == nil {
			stats = &UsageStats{SkillName: entry.Skill.Name}
		}
		userPrompt := fmt.Sprintf(`## 현재 SKILL.md
%s

## 사용 통계
- 총 사용: %d회
- 성공: %d회 (%.0f%%)
- 실패: %d회
- 최근 에러: %s%s%s`,
			string(raw),
			stats.TotalUses, stats.SuccessCount, stats.SuccessRate*100,
			stats.FailureCount,
			formatRecentErrors(stats.RecentErrors),
			formatFailurePatternsForPrompt(stats),
			formatValidationCasesForPrompt(cases))
		scenarios = append(scenarios, producerShadowScenario{
			Skill:      entry.Skill.Name,
			UserPrompt: userPrompt,
			Cases:      cases,
		})
	}
	return scenarios
}

// producerShadowGenFn generates ONE candidate response with an explicit system
// prompt. Injectable so the bench is testable without an LLM.
type producerShadowGenFn func(ctx context.Context, systemPrompt, userPrompt string) (string, error)

// shadowCandidateBody parses a producer response and returns the candidate
// body ("" for skip / parse failure / empty body).
func shadowCandidateBody(text string) string {
	resp, err := jsonutil.UnmarshalLLM[evolveResp](text)
	if err != nil || resp.Skip || resp.Changes == nil {
		return ""
	}
	return strings.TrimSpace(stripEchoedFrontmatter(resp.Changes.Body))
}

// runProducerShadowBench replays every scenario through both prompts and
// scores the generated candidates on the scenario's validation cases. A
// scenario counts only when BOTH prompts yielded a candidate — a one-sided
// skip says nothing about relative quality. Flips are counted per case:
// incumbent candidate passes, proposal candidate fails.
func runProducerShadowBench(ctx context.Context, incumbentPrompt, proposalPrompt string, scenarios []producerShadowScenario, gen producerShadowGenFn) producerBenchOutcome {
	var out producerBenchOutcome
	var incSum, propSum float64
	// Flip notes are the verdict's evidence and must never be crowded out of
	// the capped Notes by skip/error notes from earlier scenarios — the ledger
	// showed "flipped 1 case(s): <skill>: one-sided skip/unparsable" verdicts
	// whose cited note had nothing to do with the flip.
	var flipNotes, otherNotes []string
	for _, sc := range scenarios {
		incText, err := gen(ctx, incumbentPrompt, sc.UserPrompt)
		if err != nil {
			otherNotes = append(otherNotes, fmt.Sprintf("%s: incumbent generation error: %v", sc.Skill, err))
			continue
		}
		propText, err := gen(ctx, proposalPrompt, sc.UserPrompt)
		if err != nil {
			otherNotes = append(otherNotes, fmt.Sprintf("%s: proposal generation error: %v", sc.Skill, err))
			continue
		}
		incBody, propBody := shadowCandidateBody(incText), shadowCandidateBody(propText)
		if incBody == "" && propBody == "" {
			// Neither side produced a candidate: the scenario says nothing about
			// relative quality (it was mislabelled "one-sided" before).
			otherNotes = append(otherNotes, fmt.Sprintf("%s: both sides skip/unparsable", sc.Skill))
			continue
		}
		if incBody == "" || propBody == "" {
			otherNotes = append(otherNotes, fmt.Sprintf("%s: one-sided skip/unparsable (incumbent=%v proposal=%v)", sc.Skill, incBody != "", propBody != ""))
			continue
		}
		incByCase := scoreSkillValidationCasesByCase(incBody, sc.Cases)
		propByCase := scoreSkillValidationCasesByCase(propBody, sc.Cases)
		var inc, prop validationCaseScore
		for i := range incByCase {
			inc.add(incByCase[i])
			prop.add(propByCase[i])
			if incByCase[i].Total > 0 && incByCase[i].casePasses() && !propByCase[i].casePasses() {
				out.Flips++
				flipNotes = append(flipNotes, fmt.Sprintf("%s: flip on %s", sc.Skill, validationCaseLabel(sc.Cases[i])))
			}
		}
		out.Skills++
		incSum += inc.percent()
		propSum += prop.percent()
	}
	if out.Skills > 0 {
		out.IncumbentScore = incSum / float64(out.Skills)
		out.ProposalScore = propSum / float64(out.Skills)
	}
	out.Notes = append(append(out.Notes, flipNotes...), otherNotes...)
	if len(out.Notes) > 4 {
		out.Notes = out.Notes[:4]
	}
	return out
}

// producerFlipNotes returns the flip notes only — the evidence a flip verdict
// must cite (skip/error notes describe scenarios the verdict did not score).
func producerFlipNotes(out producerBenchOutcome) []string {
	var flips []string
	for _, n := range out.Notes {
		if strings.Contains(n, ": flip on ") {
			flips = append(flips, n)
		}
	}
	return flips
}

// producerBenchDecision is the deterministic promotion rule for a
// producer-epoch proposal. Zero scenarios means the corpus cannot bench a
// producer revision yet — the proposal stays propose-only surfaced (return
// ""), because manual adoption is the adjudicator in that regime. With bench
// data: any flip rejects; a mean regression beyond the noise epsilon rejects.
func producerBenchDecision(out producerBenchOutcome) string {
	if out.Skills == 0 {
		return ""
	}
	if out.Flips > 0 {
		return fmt.Sprintf("shadow replay flipped %d previously-passing case(s): %s",
			out.Flips, strings.Join(producerFlipNotes(out), "; "))
	}
	if out.ProposalScore < out.IncumbentScore-producerBenchScoreEpsilon {
		return fmt.Sprintf("shadow replay mean score regressed (%.1f < incumbent %.1f - %.1f)",
			out.ProposalScore, out.IncumbentScore, producerBenchScoreEpsilon)
	}
	return ""
}

// producerShadowExecutor builds the real LLM-backed generator from the
// evolver's primary (producer) wiring.
func (t *MetaEvolutionTask) producerShadowExecutor() producerShadowGenFn {
	client, model := t.Evolver.primaryModel()
	if client == nil {
		return nil
	}
	return func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
		return client.Complete(ctx, llm.ChatRequest{
			Model:          model,
			Messages:       []llm.Message{llm.NewTextMessage("user", userPrompt)},
			System:         llm.SystemString(systemPrompt),
			MaxTokens:      12288,
			Temperature:    evolveTemperature(),
			Thinking:       t.Evolver.thinkingOff(model),
			ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
		})
	}
}
