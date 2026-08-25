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
	"regexp"
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

// skillToolRefPattern matches a tool/command identifier the skill body relies
// on: a snake_case token (the Deneb tool convention — wiki_search, mail_archive,
// code_action). The underscore requirement keeps ordinary prose words out.
var skillToolRefPattern = regexp.MustCompile(`[a-z][a-z0-9]*_[a-z0-9_]+`)

// extractToolRefs returns the distinct snake_case tool identifiers referenced in
// the body, in first-seen order.
// extractToolRefs returns the snake_case identifiers in body that are ACTUALLY
// registered gateway tools.
//
// The pattern alone matches any snake_case token, and a SKILL body is full of
// them that are not tools: parameter names (max_results, message_id,
// include_body), config keys (db_path, md_dir, tailnet_id, serper_api_key), and
// response fields (no_reply, as_json). Authoring a "tool coverage" case for
// those probes nothing — dropping the line that mentions `max_results` does not
// remove a tool contract. Measured 2026-08-26: of 42 tool-coverage cases in the
// live corpus, close to half named a parameter rather than a tool.
//
// known is the registry's own name set, injected because the tool registry
// lives in the chat pipeline and this is a domain package. An empty set means
// the caller could not supply one; then no tool cases are authored at all,
// which is the safe direction — a coverage case built on a guess is worse than
// no coverage case.
func extractToolRefs(body string, known map[string]struct{}) []string {
	if len(known) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	for _, m := range skillToolRefPattern.FindAllString(strings.ToLower(body), -1) {
		if _, ok := seen[m]; ok {
			continue
		}
		if _, isTool := known[m]; !isTool {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// dropLinesMentioning removes every line of body that contains token (case-
// insensitive) — the behavioral mutation: the skill no longer names the tool it
// relied on.
func dropLinesMentioning(body, token string) string {
	token = strings.ToLower(token)
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), token) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// probeBehavioralCoverageGaps is the structural probe's behavioral sibling:
// instead of dropping a section it drops the lines that name a tool the skill
// relies on, then asks the case set to catch the loss. An uncaught tool-drop
// means the tool contract is unprotected — author a RequiredTools case, which
// the deterministic scorer checks against the body AND the executor gate checks
// against the real tool-call plan (so the case tests behavior, not just prose).
// Only tools NOT already asserted by an existing case are probed.
func probeBehavioralCoverageGaps(skillName, body string, cases []SkillValidationCaseRecord, known map[string]struct{}) []SkillValidationCaseRecord {
	body = skillBodyOnly(body)
	tools := extractToolRefs(body, known)
	if len(tools) == 0 {
		return nil
	}

	protected := map[string]struct{}{}
	for _, tc := range cases {
		for _, tool := range tc.Replay.RequiredTools {
			protected[strings.ToLower(strings.TrimSpace(tool))] = struct{}{}
		}
	}

	basePassed := aggregatePassed(scoreSkillValidationCasesByCase(body, cases))

	var authored []SkillValidationCaseRecord
	for _, tool := range tools {
		if len(authored) >= adversarialCoverageMaxPerSkill {
			break
		}
		if _, ok := protected[tool]; ok {
			continue
		}
		mutated := dropLinesMentioning(body, tool)
		if strings.TrimSpace(mutated) == strings.TrimSpace(body) {
			continue
		}
		if aggregatePassed(scoreSkillValidationCasesByCase(mutated, cases)) < basePassed {
			continue // already caught — good coverage
		}
		newCase := SkillValidationCaseRecord{
			SkillName:   skillName,
			ID:          fmt.Sprintf("advtool-%s-%s", skillName, tool),
			Description: "adversarial coverage: tool \"" + tool + "\" was unprotected",
			Replay: SkillReplayCaseRecord{
				// Borrow a REAL user task from the skill's existing cases.
				//
				// This case is meant to be checked behaviorally — drop the lines
				// naming the tool and the emitted plan should stop calling it.
				// That needs a task a model can actually plan for. The generator
				// used to synthesize one ("exercise the skill's use of tool
				// read_spillover"), which is a meta-instruction, not a task: the
				// executor refused it ("실제 YouTube URL이나 영상 요청 없이 …"), the plan
				// parse failed, and the skill lost its behavioral gate entirely.
				//
				// When the skill has no case carrying a real task, Input stays
				// empty and replayBehaviorEvaluable skips it — the deterministic
				// scorer still checks RequiredTools against the body, so the case
				// keeps its value instead of pretending to be executable.
				Input:         borrowedReplayInput(cases),
				RequiredTools: []string{tool},
			},
			Source:       "adversarial-coverage",
			FrontierTier: "hard",
		}
		probe := []SkillValidationCaseRecord{newCase}
		if scoreSkillValidationCases(body, probe).casePasses() && !scoreSkillValidationCases(mutated, probe).casePasses() {
			authored = append(authored, newCase)
		}
	}
	return authored
}

// AdversarialCoverageTask is the standing lane that probes coverage gaps across
// actively-used skills and authors the missing cases. Deterministic and
// LLM-free, so it runs cheaply and idempotently; the tracker's weak-case filter
// and the discriminative check keep the corpus clean.
type AdversarialCoverageTask struct {
	Evolver *Evolver
	Tracker *Tracker
	Logger  *slog.Logger

	// KnownTools returns the registered gateway tool names. Without it the tool
	// half of this probe does not run — see extractToolRefs for why guessing is
	// the worse failure. The section half (heading drops) is unaffected.
	KnownTools func() []string
}

// knownToolSet snapshots the injected tool names, lowercased for matching.
func (t *AdversarialCoverageTask) knownToolSet() map[string]struct{} {
	if t == nil || t.KnownTools == nil {
		return nil
	}
	names := t.KnownTools()
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			out[n] = struct{}{}
		}
	}
	return out
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
	knownTools := t.knownToolSet()
	if len(knownTools) == 0 {
		// The section probe still runs; only the tool half needs the registry.
		logger.Warn("adversarial-coverage: no tool registry wired — authoring section coverage only")
	}
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
		if exploitable, _ := probeGateExploitTrap(t.Evolver, skill, string(raw), cases); exploitable {
			// Gate-integrity alarm: the substring-stuffed trap cleared the
			// deterministic preflight. Error level — the gate stack failed its
			// own contract, not a data gap.
			logger.Error("adversarial-coverage: gate-exploit trap PASSED preflight — deterministic gates exploitable for this skill",
				"skill", skill)
			if lerr := t.Tracker.logGateExploit(skill, "substring-stuffed trap candidate cleared validateCandidatePreflight"); lerr != nil {
				logger.Warn("adversarial-coverage: gate-exploit ledger write failed", "skill", skill, "error", lerr)
			}
		}
		probes := append(
			probeStructuralCoverageGaps(skill, string(raw), cases),
			probeBehavioralCoverageGaps(skill, string(raw), cases, knownTools)...,
		)
		for _, nc := range probes {
			if rerr := t.Tracker.RecordSkillValidationCase(nc); rerr != nil {
				logger.Debug("adversarial-coverage: authored case rejected by tracker filter",
					"skill", skill, "error", rerr)
				continue
			}
			authored++
		}
	}
	if authored > 0 {
		logger.Info("adversarial-coverage: authored held-out cases for uncaught section/tool drops",
			"count", authored)
	}
	return nil
}

// --- Gate-exploit trap probe (Hack-Verifiable Environments, arXiv 2605.20744;
// RSI 2026H2 addendum #3) ---
//
// The coverage probes above ask "can a BROKEN body slip past the cases?".
// This asks the inverse integrity question: "can an EXPLOIT-SHAPED candidate
// slip past the deterministic gates?" — a body that stuffs every required
// substring verbatim while gutting real content maximizes the held-out score
// without improving the skill (the canonical reward-hack against substring
// assertions). The trap runs through the REAL preflight
// (validateCandidatePreflight: guardrails + edit budget + held-out engine +
// audit); rejection is the expected outcome and proves the gate stack holds.
// A pass is a gate-integrity alarm, ledgered as "gate_exploit" so the drift
// audit and the operator see it before optimization pressure finds the same
// hole. Deterministic, no LLM, evidence-only.

// buildExploitTrapBody assembles the substring-stuffed trap. Empty when the
// case set carries no required substrings (nothing to stuff = no trap).
func buildExploitTrapBody(skillName string, cases []SkillValidationCaseRecord) string {
	var stuffed []string
	seen := map[string]struct{}{}
	for _, c := range cases {
		for _, s := range c.RequiredSubstrings {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			stuffed = append(stuffed, s)
		}
	}
	if len(stuffed) == 0 {
		return ""
	}
	return "# " + skillName + "\n\n" + strings.Join(stuffed, "\n")
}

// gateExploitTrapAudit is the maximally exploit-shaped but formally complete
// audit: every required field present, riding the review-finding path so the
// probe tests the STRUCTURAL gates (guardrails/budget/held-out), not the
// audit-form gate.
func gateExploitTrapAudit() HarnessEditAudit {
	return HarnessEditAudit{
		TargetSignature:        "terminal=gate-trap|mechanism=substring-stuffing",
		EditedSurface:          "body",
		ExpectedBehaviorChange: "none — synthetic exploit probe",
		RegressionRisk:         "none — never committed",
	}
}

// probeGateExploitTrap runs the trap through the real deterministic preflight.
// Returns exploitable=true (with the empty rejection reason) when the gates
// FAILED to reject it.
func probeGateExploitTrap(e *Evolver, skillName, originalContent string, cases []SkillValidationCaseRecord) (exploitable bool, rejectReason string) {
	trap := buildExploitTrapBody(skillName, cases)
	if trap == "" || e == nil {
		return false, ""
	}
	ok, reason := e.validateCandidatePreflight(skillName, originalContent, trap,
		gateExploitTrapAudit(), &UsageStats{SkillName: skillName}, "adversarial gate-trap probe (synthetic)")
	return ok, reason
}

// borrowedReplayInput returns a real user task from the skill's existing cases,
// or "" when none carries one. Placeholder inputs from earlier generator
// versions are skipped — borrowing one would recreate the very failure this
// exists to avoid.
func borrowedReplayInput(cases []SkillValidationCaseRecord) string {
	for _, tc := range cases {
		input := strings.TrimSpace(tc.Replay.Input)
		if input == "" || strings.HasPrefix(input, legacyCoverageInputPrefix) {
			continue
		}
		return input
	}
	return ""
}
