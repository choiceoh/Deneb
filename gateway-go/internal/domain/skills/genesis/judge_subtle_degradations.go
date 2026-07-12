package genesis

// Subtle judge-degradation pairs — the P3 label food the blatant bench can't
// produce.
//
// The judge-accuracy lane (judge_accuracy.go) measures the LIVE judge against
// planted defects and ledgers its misses as few-shot exhibits for P3 verifier
// co-evolution. But the meta-bench degradations (section-drop, fake-tool,
// truncation, overfit) are BLATANT — a competent judge rejects them every run,
// so the miss ledger stays empty and P3 has nothing to learn from. These
// degradations are the opposite: each is an UNAMBIGUOUS regression (ground
// truth = the degraded body is strictly worse) delivered as a SINGLE-LINE
// removal of load-bearing guidance, subtle enough that the live judge sometimes
// overlooks it. An overlooked one is an HONEST miss — a real defect the judge
// failed to reject — which is exactly the label P3 needs.
//
// Honesty invariant (parallels judge_accuracy.go's construction-time labeling
// and the runtime-health bench's fault accounting): every degradation here
// removes guidance whose loss is a defect BY DEFINITION — an imperative rule
// the skill can no longer enforce, a safety warning it can no longer give. A
// "pass" verdict on one is therefore genuinely wrong, never a debatable
// judgment call, so the resulting label carries no ground-truth noise.
// Degradations whose harm is arguable (step reordering, example loss) are
// deliberately EXCLUDED — a judge that passes those has not necessarily erred.
//
// Used ONLY by the judge-accuracy lane, never by the meta-judge promotion gate
// (buildJudgeDegradationPairs): the gate needs blatant, must-catch pairs with a
// hard floor, and mixing harder-difficulty pairs into it would raise the bar on
// legitimate judge revisions. The separation keeps the gate stable while the
// lane gets harder food.

import (
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// imperativeRuleTokens mark a line that states a hard rule the skill enforces.
// Removing such a line silently weakens the skill without touching structure.
var imperativeRuleTokens = []string{"반드시", "필수", "절대", "항상", "must ", "must:", "always ", "never "}

// safetyNoteTokens mark a caution/warning line. Removing it strips a guardrail.
var safetyNoteTokens = []string{"주의", "경고", "위험", "⚠️", "warning", "caution", "danger"}

// subtleDegradationMinLineRunes filters out lines too short to carry a
// load-bearing rule (a bare "주의:" heading fragment, a list bullet marker).
const subtleDegradationMinLineRunes = 12

// dropFirstLineMatching removes the first non-heading, substantive line that
// contains any token (case-insensitive). Returns the mutated body and the
// removed line; ok=false when no such line exists.
func dropFirstLineMatching(body string, tokens []string) (mutated, removed string, ok bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue // blanks and headings are section-drop's job, not this
		}
		if len([]rune(trimmed)) < subtleDegradationMinLineRunes {
			continue // too short to be a load-bearing rule
		}
		low := strings.ToLower(line)
		for _, tok := range tokens {
			if strings.Contains(low, strings.ToLower(tok)) {
				kept := append(append([]string{}, lines[:i]...), lines[i+1:]...)
				return strings.Join(kept, "\n"), trimmed, true
			}
		}
	}
	return "", "", false
}

// degradeDropImperative removes one imperative-rule line — the skill can no
// longer enforce a rule it used to. Unambiguous regression, single line.
func degradeDropImperative(body string) (string, bool) {
	mutated, _, ok := dropFirstLineMatching(body, imperativeRuleTokens)
	if !ok || strings.TrimSpace(mutated) == strings.TrimSpace(body) {
		return "", false
	}
	return strings.TrimSpace(mutated), true
}

// degradeDropSafetyNote removes one caution/warning line — the skill can no
// longer warn about a hazard it used to. Unambiguous regression, single line.
func degradeDropSafetyNote(body string) (string, bool) {
	mutated, _, ok := dropFirstLineMatching(body, safetyNoteTokens)
	if !ok || strings.TrimSpace(mutated) == strings.TrimSpace(body) {
		return "", false
	}
	return strings.TrimSpace(mutated), true
}

// buildSubtleJudgeDegradationPairs mirrors buildJudgeDegradationPairs but with
// the subtle single-line degradations above. Deterministic, catalog order,
// stops at limit. Shares the judgeBenchPair shape so the lane replays it
// through the identical verdict path.
func buildSubtleJudgeDegradationPairs(entries []skills.SkillEntry, limit int) []judgeBenchPair {
	if limit <= 0 {
		limit = judgeBenchMaxPairs
	}
	degradations := []struct {
		name  string
		apply func(string) (string, bool)
	}{
		{"imperative-drop", degradeDropImperative},
		{"safety-drop", degradeDropSafetyNote},
	}
	pairs := make([]judgeBenchPair, 0, limit)
	for _, entry := range entries {
		if len(pairs) >= limit {
			break
		}
		raw, err := os.ReadFile(entry.Skill.FilePath)
		if err != nil {
			continue
		}
		body := strings.TrimSpace(skillBodyOnly(string(raw)))
		if len([]rune(body)) < judgeBenchMinBodyRunes {
			continue
		}
		for _, d := range degradations {
			if len(pairs) >= limit {
				break
			}
			degraded, ok := d.apply(body)
			if !ok || strings.TrimSpace(degraded) == body {
				continue
			}
			pairs = append(pairs, judgeBenchPair{
				Skill:       entry.Skill.Name,
				Category:    entry.Skill.Category,
				Degradation: d.name,
				Original:    body,
				Degraded:    degraded,
			})
		}
	}
	return pairs
}
