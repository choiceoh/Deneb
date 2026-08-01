package genesis

import (
	"fmt"
	"sort"
	"strings"
)

// Structural preflight for the failure the evolve ledger reports most often.
//
// Over 30 days the held-out gate rejected candidates with "replay missing
// required tool" seven times — the single largest quality bucket. Every one of
// those cost a producer rewrite plus a judge call before a replay discovered
// that the rewrite had simply dropped a tool the skill is contracted to use.
// That is detectable from the text, with no model and no replay.
//
// The check is deliberately one-directional: a tool is enforced only when the
// ORIGINAL body mentioned it. A validation case can name a tool the skill never
// documented (the 2026-07-21 weekly-report rejection was exactly that — a case
// requiring a tool absent from the corpus), and enforcing those would block
// every future evolve of that skill on a defect in the case rather than in the
// candidate. Preserving what exists is the contract; supplying what was never
// there is not this gate's business.
func missingRequiredToolMentions(cases []SkillValidationCaseRecord, originalContent, candidateBody string) []string {
	original := strings.ToLower(originalContent)
	candidate := strings.ToLower(candidateBody)
	seen := map[string]bool{}
	var dropped []string
	for _, tc := range cases {
		for _, tool := range tc.Replay.RequiredTools {
			name := strings.ToLower(strings.TrimSpace(tool))
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			// Only a tool the original actually documented can be "dropped".
			if !strings.Contains(original, name) {
				continue
			}
			if !strings.Contains(candidate, name) {
				dropped = append(dropped, strings.TrimSpace(tool))
			}
		}
	}
	sort.Strings(dropped) // deterministic reason text
	return dropped
}

// requiredToolPreflight rejects a rewrite that dropped a contracted tool
// mention, with the tool named so the next attempt has something to act on.
func requiredToolPreflight(cases []SkillValidationCaseRecord, originalContent, candidateBody string) (bool, string) {
	dropped := missingRequiredToolMentions(cases, originalContent, candidateBody)
	if len(dropped) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf(
		"required-tool preflight: candidate dropped tool mention(s) the skill's held-out cases contract: %s",
		strings.Join(dropped, ", "),
	)
}
