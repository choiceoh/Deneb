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
//
// The lane's probes form a CURRICULUM LADDER (CoEvoSkills: probe difficulty
// must track verifier strength — a corpus the judge has fully outgrown
// produces zero labels forever). Tier 2 is the drop classes below; tier 3 is
// in-place WEAKENING (imperative-weaken, scope-narrow): the line survives,
// its binding force does not — nothing is absent for a diff to catch, the
// guarantee is simply gone. The lane deploys tier 3 only after the incumbent
// judge saturates tier 2 (judge_accuracy.go weakenTierUnlocked). The honesty
// invariant holds unchanged: a hard rule downgraded to a preference is the
// same enforcement loss as the rule deleted, and a universal coverage claim
// narrowed to a partial one no longer promises what the validated original
// promised. Swaps whose harm would be debatable (tone, synonyms, emphasis)
// are excluded.

import (
	"os"
	"strings"
	"unicode"

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

// probeTargetLine reports whether a trimmed line is a legitimate probe
// target: substantive body prose — not a blank, a heading (section-drop's
// job, and the honesty invariant excludes structure loss), or a fragment too
// short to carry a load-bearing rule.
func probeTargetLine(trimmed string) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	return len([]rune(trimmed)) >= subtleDegradationMinLineRunes
}

// dropFirstLineMatching removes the first non-heading, substantive line that
// contains any token (case-insensitive). Returns the mutated body and the
// removed line; ok=false when no such line exists.
func dropFirstLineMatching(body string, tokens []string) (mutated, removed string, ok bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !probeTargetLine(trimmed) {
			continue
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

// --- Tier 3: in-place weakening ---

// imperativeWeakenSwaps dilute a hard-rule token in place: mandatory becomes
// optional while the sentence stays grammatical. English tokens keep their
// trailing space so substring traps (mustard, alway…) cannot fire.
var imperativeWeakenSwaps = [][2]string{
	{"반드시", "가급적"},
	{"필수", "권장"},
	{"절대", "되도록"},
	{"항상", "보통"},
	{"must ", "may "},
	{"always ", "usually "},
	{"never ", "rarely "},
}

// scopeNarrowSwaps shrink a universal quantifier to a partial one — coverage
// the validated skill guaranteed is silently no longer promised. Trailing
// spaces keep English tokens off substring traps (small/install/overall).
var scopeNarrowSwaps = [][2]string{
	{"모든 ", "일부 "},
	{"전부 ", "일부 "},
	{"every ", "some "},
}

// indexFold returns the byte index of the first case-insensitive occurrence
// of token in s, scanning the ORIGINAL string so splice offsets are always
// valid (strings.ToLower can shift byte offsets on exotic runes).
func indexFold(s, token string) int {
	if token == "" || len(s) < len(token) {
		return -1
	}
	for i := 0; i+len(token) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(token)], token) {
			return i
		}
	}
	return -1
}

// weakenNegationAdjacent reports whether the token at [idx, idx+tokenLen) sits
// in an ENGLISH negated context where the imperative swap would NOT weaken the
// rule: "must not commit" → "may not commit" is still a prohibition. ONLY
// English imperatives (must/always/never) can be inverted this way; Korean
// adverb swaps (반드시→가급적, 절대→되도록) weaken the adverb regardless of a
// following negative verb ending, so they are never guarded. The old code
// scanned for bare "말"/"않" substrings and false-skipped every "정말"/"얼마"
// line (3rd-review M5). Rune-safe throughout.
func weakenNegationAdjacent(line string, idx, tokenLen int) bool {
	// Korean/non-ASCII tokens are never inverted by an English negation.
	if !isASCIIWordToken(line[idx : idx+tokenLen]) {
		return false
	}
	// Right side: an English negation immediately after the token ("must NOT").
	after := strings.ToLower(strings.TrimLeft(line[idx+tokenLen:], " "))
	if strings.HasPrefix(after, "not ") || strings.HasPrefix(after, "not.") ||
		strings.HasPrefix(after, "not,") || strings.HasPrefix(after, "n't") {
		return true
	}
	// Left side: an English negation just before the token ("do not always").
	// Rune-safe: back up whole runes, not bytes, then look for a standalone
	// " not " in the lowercased window.
	before := strings.ToLower(lastRunes(line[:idx], 8))
	return strings.Contains(before, "not ") || strings.HasSuffix(strings.TrimRight(before, " "), "n't")
}

// isASCIIWordToken reports whether s is entirely ASCII letters and spaces —
// the shape of the English imperative swap tokens ("must ", "always ",
// "never "). Digits/punctuation are rejected so only genuinely English
// imperatives take the negation guard (Korean adverb tokens are non-ASCII and
// weaken regardless of negation).
func isASCIIWordToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == ' ') {
			return false
		}
	}
	return true
}

// lastRunes returns up to the last n runes of s (rune-safe tail slice).
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// weakenFirstLineMatching applies the first applicable swap to the first
// substantive line carrying a swap's token — one line, one token, in place.
// A leading capital on the matched token is preserved on the replacement so
// the mutation stays typographically clean. Returns ok=false when no
// substantive line carries any token.
func weakenFirstLineMatching(body string, swaps [][2]string) (string, bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !probeTargetLine(strings.TrimSpace(line)) {
			continue
		}
		for _, sw := range swaps {
			idx := indexFold(line, sw[0])
			if idx < 0 {
				continue
			}
			// Honesty invariant (RSI code eval M5): a weaken swap is only a real
			// enforcement loss in an AFFIRMATIVE context. In a negated one the
			// swap preserves the constraint — "must not commit" → "may not
			// commit" is still a prohibition, so a judge that passes the pair is
			// CORRECT, yet the old code ledgered it as a missed defect and fed
			// that noise to the evaluator epoch. Skip a negation-adjacent match
			// and try the next token/line.
			if weakenNegationAdjacent(line, idx, len(sw[0])) {
				continue
			}
			repl := sw[1]
			if r := []rune(line[idx:]); len(r) > 0 && unicode.IsUpper(r[0]) {
				rr := []rune(repl)
				rr[0] = unicode.ToUpper(rr[0])
				repl = string(rr)
			}
			mutated := line[:idx] + repl + line[idx+len(sw[0]):]
			if mutated == line {
				continue
			}
			kept := append(append([]string{}, lines[:i]...), mutated)
			kept = append(kept, lines[i+1:]...)
			return strings.TrimSpace(strings.Join(kept, "\n")), true
		}
	}
	return "", false
}

// degradeWeakenImperative dilutes one hard-rule token in place — the rule
// text survives but no longer binds. Unambiguous regression, single token.
func degradeWeakenImperative(body string) (string, bool) {
	return weakenFirstLineMatching(body, imperativeWeakenSwaps)
}

// degradeNarrowScope shrinks one universal quantifier in place — coverage the
// skill guaranteed becomes partial. Unambiguous regression, single token.
func degradeNarrowScope(body string) (string, bool) {
	return weakenFirstLineMatching(body, scopeNarrowSwaps)
}

// --- Tier 4: exclusivity removal ---

// exclusivityDropSwaps delete the marker that made a rule EXCLUSIVE, leaving a
// sentence that still reads perfectly and still says something true — it just
// no longer forbids the other cases. "X할 때만 Y해라" → "X할 때 Y해라": the
// original ruled out Y everywhere else, the mutant permits it. That is the
// same enforcement loss tier 3 produces, but with nothing swapped IN — the
// residue is a strictly shorter, entirely natural line, so a diff-shaped read
// of the pair shows only a deleted syllable.
//
// Anchors are multi-character on purpose. Bare "만" is a common syllable
// (만들다·만약·얼마만) and matching it would mutate unrelated prose — the same
// substring trap that made the old negation guard false-skip every 정말/얼마
// line (3rd-review M5). Each anchor here carries its own preceding context
// (때/경우에/오직) or a trailing space (English), so no shorter word can match.
var exclusivityDropSwaps = [][2]string{
	{"경우에만", "경우에"},
	{"때만", "때"},
	{"오직 ", ""},
	{"only when ", "when "},
	{"only if ", "if "},
	{"exclusively ", ""},
}

// degradeDropExclusivity removes one exclusivity marker in place — a rule that
// applied ONLY in some case now no longer excludes the others. Unambiguous
// regression, single token, grammatical residue.
func degradeDropExclusivity(body string) (string, bool) {
	return weakenFirstLineMatching(body, exclusivityDropSwaps)
}

// namedDegradation pairs a ByClass label with its body mutator.
type namedDegradation struct {
	name  string
	apply func(string) (string, bool)
}

// subtleJudgeDegradations is the tier-2 (drop) class table. The escalation
// gate (judge_accuracy.go weakenTierUnlocked) keys saturation on exactly
// these class names, so the two stay coupled through this table.
var subtleJudgeDegradations = []namedDegradation{
	{"imperative-drop", degradeDropImperative},
	{"safety-drop", degradeDropSafetyNote},
}

// weakenJudgeDegradations is the tier-3 (in-place weaken) class table,
// deployed only on tier-2 saturation.
var weakenJudgeDegradations = []namedDegradation{
	{"imperative-weaken", degradeWeakenImperative},
	{"scope-narrow", degradeNarrowScope},
}

// exclusivityJudgeDegradations is the tier-4 class table, deployed only on
// tier-3 saturation. Added 2026-07-25 because the incumbent judge had reached
// 36/36 on tier 3 with zero organic labels in 30 days — a fully outgrown
// corpus produces no misses, so L3 read DATA-GATED with nothing left to learn
// from. The ladder's premise (CoEvoSkills) is that probe difficulty must keep
// tracking verifier strength; a ceiling is a measurement failure, not a result.
var exclusivityJudgeDegradations = []namedDegradation{
	{"exclusivity-drop", degradeDropExclusivity},
}

// buildSubtleJudgeDegradationPairs mirrors buildJudgeDegradationPairs but with
// the subtle single-line drop degradations. Deterministic, catalog order,
// stops at limit. Shares the judgeBenchPair shape so the lane replays it
// through the identical verdict path.
func buildSubtleJudgeDegradationPairs(entries []skills.SkillEntry, limit int) []judgeBenchPair {
	return buildDegradationPairsWith(entries, limit, subtleJudgeDegradations)
}

// buildWeakenJudgeDegradationPairs builds the escalated tier-3 pairs.
func buildWeakenJudgeDegradationPairs(entries []skills.SkillEntry, limit int) []judgeBenchPair {
	return buildDegradationPairsWith(entries, limit, weakenJudgeDegradations)
}

// buildExclusivityJudgeDegradationPairs builds the escalated tier-4 pairs.
func buildExclusivityJudgeDegradationPairs(entries []skills.SkillEntry, limit int) []judgeBenchPair {
	return buildDegradationPairsWith(entries, limit, exclusivityJudgeDegradations)
}

// buildDegradationPairsWith is the shared pair constructor both tiers use:
// deterministic catalog order, min-body floor, no-op mutations skipped.
func buildDegradationPairsWith(entries []skills.SkillEntry, limit int, degradations []namedDegradation) []judgeBenchPair {
	if limit <= 0 {
		limit = judgeBenchMaxPairs
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
