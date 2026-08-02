package genesis

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Held-out tie disclosure — the narrow exception to SkillHone blindness.
//
// The held-out pool is blind by design (gate-echo prevention) and the flip gate
// demands strict improvement, while the Hermes guardrails force candidates to
// preserve everything that already passes. Put together, a skill whose VISIBLE
// gaps are exhausted can only improve its blind score by luck: the producer
// cannot see what is missing, and cannot lose what it has. The observed result
// (2026-08-02) was exact ties forever — 35.8 vs 35.8, 28.9 vs 28.9, 59.2 vs
// 59.2 across three mature skills, 14 attempts in one night, zero completions.
//
// After two consecutive tie rejections the stall is proven, and the trade
// flips: perpetual zero output costs more than a narrow disclosure. The third
// attempt's prompt names the ORIGINAL body's unmet blind requirements (capped),
// so the producer finally has a target; if it still ties, the rejection backoff
// engages at three and the lane goes quiet. Blindness stays intact for every
// assertion the body already passes and for every future case — only
// requirements the skill has provenly failed to meet, repeatedly, are named.
const (
	heldOutTieDisclosureStreak = 2
	heldOutTieDisclosureCap    = 3
)

// heldOutTiePattern matches the flip-gate rejection reason's score pair. The
// reason string is stable wire (parsed here and by operators reading the
// ledger); this parses rather than re-plumbing a typed field through the
// lifecycle log.
var heldOutTiePattern = regexp.MustCompile(`\(([0-9]+(?:\.[0-9]+)?) vs original ([0-9]+(?:\.[0-9]+)?)\)`)

// isHeldOutTieRejection reports whether a rejection reason records an EXACT
// score tie — the candidate changed nothing the blind pool can see. A tie is
// not evidence the candidate was bad; it is evidence the measuring stick
// cannot see the change, and every consumer that reads it as candidate quality
// is being told something the gate never measured.
func isHeldOutTieRejection(reason string) bool {
	m := heldOutTiePattern.FindStringSubmatch(reason)
	return m != nil && m[1] == m[2]
}

// RecentHeldOutTies counts the skill's tie rejections inside the window.
func (t *Tracker) RecentHeldOutTies(skillName string, window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath)
	if err != nil {
		return 0
	}
	cutoff := time.Now().Add(-window).UnixMilli()
	n := 0
	for _, e := range entries {
		if e.CreatedAt < cutoff || e.Type != "evolve_rejected" || e.SkillName != skillName {
			continue
		}
		if isHeldOutTieRejection(e.Reason) {
			n++
		}
	}
	return n
}

// failingRequiredSubstrings returns required substrings the body does NOT
// contain, deduped in case order, capped. Pure over its inputs so the
// disclosure is testable without a tracker.
func failingRequiredSubstrings(cases []SkillValidationCaseRecord, body string, limit int) []string {
	normalizedBody := normalizedValidationText(body)
	seen := map[string]bool{}
	var out []string
	for _, tc := range cases {
		for _, required := range tc.RequiredSubstrings {
			norm := normalizedValidationText(required)
			if norm == "" || seen[norm] {
				continue
			}
			seen[norm] = true
			if containsNormalizedValidationText(normalizedBody, required) {
				continue
			}
			out = append(out, required)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

// unmetBlindDisclosure builds the targeted-disclosure prompt section, or ""
// while the skill has not proven a stall (fewer than two recent ties).
func (e *Evolver) unmetBlindDisclosure(skillName, currentContent string) string {
	if e == nil || e.tracker == nil {
		return ""
	}
	if e.tracker.RecentHeldOutTies(skillName, evolutionRejectionBackoffWindow) < heldOutTieDisclosureStreak {
		return ""
	}
	cases, err := e.tracker.recentSkillValidationCasesPool(skillName, skillEvolutionPromptCaseLimit, true)
	if err != nil || len(cases) == 0 {
		return ""
	}
	missing := failingRequiredSubstrings(cases, skillBodyOnly(currentContent), heldOutTieDisclosureCap)
	if len(missing) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nUnmet requirements (targeted disclosure after repeated no-change verdicts — ")
	b.WriteString("the current body does not satisfy these; address them directly):\n")
	for _, m := range missing {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	return b.String()
}
