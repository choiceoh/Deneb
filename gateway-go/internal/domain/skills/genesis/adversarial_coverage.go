package genesis

// Adversarial coverage probing — "harder tests, found automatically."
//
// The reproduction oracle grows validation cases from OBSERVED failures. This
// grows them from SYNTHETIC ones: it applies deterministic, clearly-breaking
// mutations to the current (confirmed-good) skill body and asks the existing
// held-out case set to catch them. A mutation that visibly guts the skill but
// slips past every case exposes a coverage GAP — the case set is too weak
// there. The probe then authors the case that WOULD have caught it, verified
// discriminative (fails on the mutated body, passes on the current body:
// exactly the reproduction-oracle contract, with the mutation playing the role
// of the defect).
//
// This is the gate-fuzz doctrine (RSI P1.5 #3436) turned on the skill body:
// stress the coverage before real regressions learn to slip through it. Pure
// deterministic Go — no LLM, no overfitting (every authored case is proven to
// distinguish good from broken).

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// adversarialCoverageMaxPerSkill bounds authored cases per probe so one
// under-covered skill cannot flood the corpus in a single run.
const (
	adversarialCoverageMaxPerSkill = 3
	adversarialCoverageCaseLimit   = 20
)

// rawSkillHeading is a body section heading with its display text preserved
// (guardrails only exposes the normalized form; authoring a RequiredHeadings
// case needs the readable text).
type rawSkillHeading struct {
	display    string
	normalized string
	line       int // 0-based line index of the heading
	level      int // number of leading '#'
}

// extractRawHeadings returns every markdown heading in order, first-seen only.
func extractRawHeadings(body string) []rawSkillHeading {
	lines := strings.Split(body, "\n")
	out := make([]rawSkillHeading, 0)
	seen := map[string]struct{}{}
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		level := len(t) - len(strings.TrimLeft(t, "#"))
		text := strings.TrimSpace(strings.TrimLeft(t, "#"))
		if text == "" {
			continue
		}
		norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, rawSkillHeading{display: text, normalized: norm, line: i, level: level})
	}
	return out
}

// dropSection returns body with the section owned by the heading at headingLine
// removed: the heading line plus every following line up to (not including) the
// next heading line, or EOF. A clearly-breaking mutation.
func dropSection(body string, headingLine int) string {
	lines := strings.Split(body, "\n")
	if headingLine < 0 || headingLine >= len(lines) {
		return body
	}
	end := len(lines)
	for i := headingLine + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			end = i
			break
		}
	}
	kept := append(append([]string{}, lines[:headingLine]...), lines[end:]...)
	return strings.Join(kept, "\n")
}

// probeStructuralCoverageGaps finds section-drop mutations the existing case
// set fails to catch and authors the RequiredHeadings case that catches each.
// Returns new, discriminative, deduped cases (empty when coverage is tight).
func probeStructuralCoverageGaps(skillName, body string, cases []SkillValidationCaseRecord) []SkillValidationCaseRecord {
	body = skillBodyOnly(body)
	headings := extractRawHeadings(body)
	if len(headings) < 2 {
		return nil // a one-section skill has nothing meaningful to drop-probe
	}

	// Headings some case already requires — already protected, skip.
	protected := map[string]struct{}{}
	for _, tc := range cases {
		for _, h := range tc.RequiredHeadings {
			protected[strings.ToLower(strings.Join(strings.Fields(h), " "))] = struct{}{}
		}
	}

	// Baseline: how many assertions the case set passes on the intact body.
	// A mutation is "caught" when it lowers this.
	basePassed := aggregatePassed(scoreSkillValidationCasesByCase(body, cases))

	var authored []SkillValidationCaseRecord
	for _, h := range headings {
		if len(authored) >= adversarialCoverageMaxPerSkill {
			break
		}
		if h.level <= 1 {
			continue // the '# Title' is preserved by the skill judge, not a drop-probe target
		}
		if _, ok := protected[h.normalized]; ok {
			continue
		}
		mutated := dropSection(body, h.line)
		if strings.TrimSpace(mutated) == strings.TrimSpace(body) {
			continue // nothing removed
		}
		if aggregatePassed(scoreSkillValidationCasesByCase(mutated, cases)) < basePassed {
			continue // the case set already catches this drop — good coverage
		}
		// GAP: gutting section h broke nothing detectable. Author the case that
		// catches it, and confirm it is discriminative (defensive; true by
		// construction).
		newCase := SkillValidationCaseRecord{
			SkillName:        skillName,
			ID:               fmt.Sprintf("adv-%s-%s", skillName, h.normalized),
			Description:      "adversarial coverage: section \"" + h.display + "\" was unprotected",
			RequiredHeadings: []string{h.display},
			Source:           "adversarial-coverage",
			FrontierTier:     "hard",
		}
		probe := []SkillValidationCaseRecord{newCase}
		if scoreSkillValidationCases(body, probe).casePasses() && !scoreSkillValidationCases(mutated, probe).casePasses() {
			authored = append(authored, newCase)
		}
	}
	return authored
}

func aggregatePassed(scores []validationCaseScore) int {
	total := 0
	for _, s := range scores {
		total += s.Passed
	}
	return total
}

// AdversarialCoverageTask is the standing lane that probes coverage gaps across
// actively-used skills and authors the missing cases. Deterministic and
// LLM-free, so it runs cheaply and idempotently; the tracker's weak-case filter
// and the discriminative check keep the corpus clean.
type AdversarialCoverageTask struct {
	Evolver *Evolver
	Tracker *Tracker
	Logger  *slog.Logger
}

// Name identifies the task in the autonomous scheduler.
func (t *AdversarialCoverageTask) Name() string { return "adversarial-coverage" }

// Interval honors DENEB_ADVERSARIAL_COVERAGE_INTERVAL_HOURS (calibration knob).
func (t *AdversarialCoverageTask) Interval() time.Duration {
	if v := strings.TrimSpace(os.Getenv("DENEB_ADVERSARIAL_COVERAGE_INTERVAL_HOURS")); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return 12 * time.Hour
}

// Run probes every catalog skill once and records the authored cases.
func (t *AdversarialCoverageTask) Run(ctx context.Context) error {
	if t.Evolver == nil || t.Tracker == nil {
		return nil
	}
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	authored := 0
	for _, entry := range t.Evolver.catalogEntries() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		skill := entry.Skill.Name
		raw, err := os.ReadFile(entry.Skill.FilePath)
		if err != nil {
			continue
		}
		cases, err := t.Tracker.RecentSkillValidationCases(skill, adversarialCoverageCaseLimit)
		if err != nil {
			continue
		}
		for _, nc := range probeStructuralCoverageGaps(skill, string(raw), cases) {
			if rerr := t.Tracker.RecordSkillValidationCase(nc); rerr != nil {
				logger.Debug("adversarial-coverage: authored case rejected by tracker filter",
					"skill", skill, "error", rerr)
				continue
			}
			authored++
		}
	}
	if authored > 0 {
		logger.Info("adversarial-coverage: authored held-out cases for uncaught section drops",
			"count", authored)
	}
	return nil
}
