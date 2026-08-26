// skill_output_evidence.go — evidence that a reasoning-shaped skill's procedure
// ran, read from the answer instead of the tool log (ADR 0006).
//
// exercise_tools can only measure skills whose procedure calls a tool. A full
// read of the catalog on 2026-08-26 found that most cannot: deneb-ui-authoring
// must emit a card, decision-premortem must reach a recommendation through
// refutation, skill-factory must produce a SKILL.md. Their procedure produces a
// SHAPE, so they stayed Exercised=unknown forever — and the skills whose
// instructions are easiest to silently ignore were exactly the unmeasured ones.
package chat

import (
	"strings"
)

// matchesSkillOutputEvidence reports whether the answer carries any declared
// output evidence. Any-of, like the tool set: one observed pattern proves the
// procedure ran, and demanding all of them would fail correct runs that took a
// documented branch.
func matchesSkillOutputEvidence(answer string, patterns []string) bool {
	if strings.TrimSpace(answer) == "" {
		return false
	}
	for _, pattern := range patterns {
		if matchesSkillOutputPattern(answer, pattern) {
			return true
		}
	}
	return false
}

// matchesSkillOutputPattern evaluates one pattern. The vocabulary is
// deliberately three narrow forms — a regex dialect would invite patterns whose
// false-positive behavior nobody can predict, and a false positive here writes a
// correct run into the validation corpus as a failure.
func matchesSkillOutputPattern(answer, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	switch {
	case pattern == "":
		return false
	case strings.HasPrefix(pattern, "fence:"):
		return answerHasFence(answer, strings.TrimPrefix(pattern, "fence:"))
	case strings.HasPrefix(pattern, "heading:"):
		return answerHasHeading(answer, strings.TrimPrefix(pattern, "heading:"))
	default:
		return strings.Contains(normalizeEvidenceText(answer), normalizeEvidenceText(pattern))
	}
}

// answerHasFence reports whether the answer opens a code fence whose info
// string names lang. The opener must start a line: a fence mentioned inside
// prose ("```deneb-ui 로 감싸라") is instruction, not output.
func answerHasFence(answer, lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return false
	}
	for line := range strings.SplitSeq(answer, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "```") && !strings.HasPrefix(trimmed, "~~~") {
			continue
		}
		info := strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "`~")))
		// The info string may carry attributes after the language.
		if info == lang || strings.HasPrefix(info, lang+" ") {
			return true
		}
	}
	return false
}

// answerHasHeading reports whether any markdown heading matches text after
// whitespace/case normalization.
func answerHasHeading(answer, text string) bool {
	want := normalizeEvidenceText(text)
	if want == "" {
		return false
	}
	for line := range strings.SplitSeq(answer, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if normalizeEvidenceText(strings.TrimLeft(trimmed, "# ")) == want {
			return true
		}
	}
	return false
}

// normalizeEvidenceText collapses whitespace and case so a declared pattern
// survives the model's line wrapping and spacing choices.
func normalizeEvidenceText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
