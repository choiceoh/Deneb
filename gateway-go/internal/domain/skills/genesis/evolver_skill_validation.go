// evolver_skill_validation.go — self-harness audit + SKILL.md edit-surface
// guardrails split out of evolver.go. These are the deterministic gates that
// decide whether a proposed skill evolution is admissible: the self-harness
// audit must name a target failure signature backed by mined evidence (or a
// review finding), the edited_surface it claims must actually be the section
// that changed, the textual edit budget bounds how much of the body may churn,
// and the Hermes patch-first guardrails reject broad rewrites / size blowups /
// title changes. Pure package functions over content strings — no Evolver
// state — so they live apart from the orchestration in evolver.go.
package genesis

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func validateSelfHarnessAudit(audit HarnessEditAudit, stats *UsageStats, reviewFinding string) (bool, string) {
	missing := make([]string, 0, 4)
	if strings.TrimSpace(audit.TargetSignature) == "" {
		missing = append(missing, "target_signature")
	}
	if strings.TrimSpace(audit.EditedSurface) == "" {
		missing = append(missing, "edited_surface")
	}
	if strings.TrimSpace(audit.ExpectedBehaviorChange) == "" {
		missing = append(missing, "expected_behavior_change")
	}
	if strings.TrimSpace(audit.RegressionRisk) == "" {
		missing = append(missing, "regression_risk")
	}
	if len(missing) > 0 {
		return false, "self-harness audit rejected: missing " + strings.Join(missing, ", ")
	}

	// Review findings are already scoped, externalized evidence from the review
	// fork. They may not have a mined terminal=... signature, but they still need
	// a complete audit so the transition remains queryable and rollbackable.
	if strings.TrimSpace(reviewFinding) != "" {
		return true, ""
	}

	patterns := mineSkillFailurePatterns(stats)
	if len(patterns) == 0 {
		return false, "self-harness audit rejected: no failure evidence bundle or review finding supports target_signature"
	}
	target := normalizedSelfHarnessSignature(audit.TargetSignature)
	for _, pattern := range patterns {
		if selfHarnessSignatureMatches(target, normalizedSelfHarnessSignature(pattern.Signature)) {
			return true, ""
		}
	}
	return false, fmt.Sprintf("self-harness audit rejected: target_signature %q does not match supported failure signatures: %s",
		audit.TargetSignature, strings.Join(supportedFailureSignatures(patterns), ", "))
}

func normalizedSelfHarnessSignature(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
	replacer := strings.NewReplacer(
		" = ", "=",
		"= ", "=",
		" =", "=",
		" | ", "|",
		"| ", "|",
		" |", "|",
	)
	return replacer.Replace(value)
}

func selfHarnessSignatureMatches(target, supported string) bool {
	if target == "" || supported == "" {
		return false
	}
	return target == supported || strings.Contains(target, supported) || strings.Contains(supported, target)
}

func supportedFailureSignatures(patterns []skillFailurePattern) []string {
	out := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if signature := strings.TrimSpace(pattern.Signature); signature != "" {
			out = append(out, signature)
		}
	}
	return out
}

func validateSelfHarnessEditedSurface(audit HarnessEditAudit, originalContent, candidateBody string) (bool, string) {
	surfaces := normalizedEditedSurfaces(audit.EditedSurface)
	if len(surfaces) == 0 {
		return false, "self-harness surface rejected: edited_surface is empty"
	}
	originalBody := skillBodyOnly(originalContent)
	candidateBody = skillBodyOnly(candidateBody)
	changed := changedSkillSections(originalBody, candidateBody)
	for _, surface := range surfaces {
		switch surface {
		case "metadata", "frontmatter", "support-file", "support file", "tool", "tools", "runtime", "orchestration":
			return false, fmt.Sprintf("self-harness surface rejected: edited_surface %q is not editable by SKILL.md body evolve", audit.EditedSurface)
		case "body", "skill body", "skill.md", "skill.md body":
			if normalizeSectionBody(originalBody) == normalizeSectionBody(candidateBody) {
				return false, "self-harness surface rejected: edited_surface body but candidate body did not change"
			}
			continue
		}
		if !surfaceChanged(surface, changed) {
			return false, fmt.Sprintf("self-harness surface rejected: edited_surface %q did not match changed SKILL.md sections: %s",
				audit.EditedSurface, strings.Join(changedSectionNames(changed), ", "))
		}
	}
	return true, ""
}

func normalizedEditedSurfaces(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(";", ",", "|", ",", "/", ",", "&", ",", " and ", ",").Replace(value)
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.Join(strings.Fields(strings.TrimSpace(part)), " ")
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

type changedSkillSection struct {
	Display   string
	Canonical string
}

func changedSkillSections(originalBody, candidateBody string) []changedSkillSection {
	original := skillSections(originalBody)
	candidate := skillSections(candidateBody)
	keys := map[string]string{}
	for key, section := range original {
		keys[key] = section.Display
	}
	for key, section := range candidate {
		keys[key] = section.Display
	}
	out := make([]changedSkillSection, 0, len(keys))
	for key, display := range keys {
		if normalizeSectionBody(original[key].Body) == normalizeSectionBody(candidate[key].Body) {
			continue
		}
		out = append(out, changedSkillSection{Display: display, Canonical: canonicalSkillSurface(key)})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Display < out[j].Display
	})
	return out
}

type skillSection struct {
	Display string
	Body    string
}

func skillSections(content string) map[string]skillSection {
	lines := strings.Split(skillBodyOnly(content), "\n")
	sections := map[string]skillSection{}
	currentKey := "body"
	currentDisplay := "body"
	var current []string
	flush := func() {
		if len(current) == 0 && currentKey != "body" {
			return
		}
		sections[currentKey] = skillSection{Display: currentDisplay, Body: strings.Join(current, "\n")}
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			flush()
			text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if text == "" {
				text = "body"
			}
			currentKey = normalizeSkillSurface(text)
			currentDisplay = text
			current = []string{trimmed}
			continue
		}
		current = append(current, line)
	}
	flush()
	return sections
}

func surfaceChanged(surface string, changed []changedSkillSection) bool {
	surface = canonicalSkillSurface(normalizeSkillSurface(surface))
	for _, section := range changed {
		if section.Canonical == surface || section.Display == surface {
			return true
		}
	}
	return false
}

func changedSectionNames(changed []changedSkillSection) []string {
	if len(changed) == 0 {
		return []string{"(none)"}
	}
	out := make([]string, 0, len(changed))
	for _, section := range changed {
		if section.Display != "" {
			out = append(out, section.Display)
		}
	}
	return out
}

func normalizeSkillSurface(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func canonicalSkillSurface(surface string) string {
	switch {
	case surface == "procedure" || strings.Contains(surface, "procedure") || strings.Contains(surface, "workflow") || strings.Contains(surface, "step") || strings.Contains(surface, "절차") || strings.Contains(surface, "흐름"):
		return "procedure"
	case surface == "pitfalls" || strings.Contains(surface, "pitfall") || strings.Contains(surface, "gotcha") || strings.Contains(surface, "caution") || strings.Contains(surface, "주의") || strings.Contains(surface, "위험"):
		return "pitfalls"
	case surface == "verification" || strings.Contains(surface, "verification") || strings.Contains(surface, "verify") || strings.Contains(surface, "검증") || strings.Contains(surface, "확인"):
		return "verification"
	case surface == "when to use" || strings.Contains(surface, "when to use") || strings.Contains(surface, "usage") || strings.Contains(surface, "사용"):
		return "when to use"
	default:
		return surface
	}
}

func normalizeSectionBody(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func validateTextualEditBudget(originalContent, candidateBody string, covered bool) (bool, string) {
	if strings.TrimSpace(candidateBody) == "" {
		return false, "textual edit budget rejected empty candidate body"
	}
	originalBody := originalContent
	if _, bodyOffset := skills.ExtractFrontmatterBlock(originalContent); bodyOffset > 0 && bodyOffset < len(originalContent) {
		originalBody = originalContent[bodyOffset:]
	}

	originalLines := meaningfulSkillLines(originalBody)
	candidateLines := meaningfulSkillLines(candidateBody)
	if len(originalLines) < skillEditBudgetMinOriginalLines {
		return true, ""
	}
	if len(candidateLines) < max(3, len(originalLines)/3) {
		return false, fmt.Sprintf("textual edit budget exceeded: candidate shrank from %d to %d meaningful lines", len(originalLines), len(candidateLines))
	}
	if len(candidateLines) > len(originalLines)*skillEditBudgetMaxGrowthMultiple && len(candidateLines)-len(originalLines) > skillEditBudgetMaxAddedLines {
		return false, fmt.Sprintf("textual edit budget exceeded: candidate grew from %d to %d meaningful lines", len(originalLines), len(candidateLines))
	}
	if missing := missingRequiredHeadings(originalBody, candidateBody); len(missing) > 0 {
		return false, fmt.Sprintf("textual edit budget exceeded: candidate removed required headings: %s", strings.Join(missing, ", "))
	}

	retained := countRetainedLines(originalLines, candidateLines)
	changedRatio := 1 - float64(retained)/float64(len(originalLines))
	maxRatio := skillEditBudgetMaxChangedRatio
	if covered {
		maxRatio = skillCoveredEditBudgetMaxChangedRatio
	}
	if changedRatio > maxRatio {
		return false, fmt.Sprintf("textual edit budget exceeded: changed %.0f%% of meaningful lines (max %.0f%%)", changedRatio*100, maxRatio*100)
	}
	return true, ""
}

func validateHermesEvolutionGuardrails(originalContent, candidateBody string, covered bool) (bool, string) {
	candidateBody = skillBodyOnly(candidateBody)
	size := candidateSkillBytes(originalContent, candidateBody)
	if size > skillHermesMaxSkillBytes {
		return false, fmt.Sprintf("Hermes patch-first gate rejected: candidate SKILL.md size %d bytes exceeds %d byte limit", size, skillHermesMaxSkillBytes)
	}

	originalBody := skillBodyOnly(originalContent)
	originalTitle := firstTopLevelHeading(originalBody)
	candidateTitle := firstTopLevelHeading(candidateBody)
	if originalTitle != "" && candidateTitle != "" &&
		normalizeSkillSurface(originalTitle) != normalizeSkillSurface(candidateTitle) {
		return false, fmt.Sprintf("Hermes semantic-preservation gate rejected: title changed from %q to %q", originalTitle, candidateTitle)
	}

	originalLines := meaningfulSkillLines(originalBody)
	if len(originalLines) >= skillEditBudgetMinOriginalLines {
		maxSections := skillHermesMaxChangedSections
		if covered {
			maxSections = skillCoveredHermesMaxChangedSections
		}
		changed := changedSkillSections(originalBody, candidateBody)
		if len(changed) > maxSections {
			return false, fmt.Sprintf("Hermes patch-first gate rejected broad rewrite: changed %d sections (%s), max %d",
				len(changed), strings.Join(changedSectionNames(changed), ", "), maxSections)
		}
	}
	return true, ""
}

func candidateSkillBytes(originalContent, candidateBody string) int {
	header, bodyOffset := skills.ExtractFrontmatterBlock(originalContent)
	if bodyOffset <= 0 || header == "" {
		return len([]byte(candidateBody))
	}
	return len([]byte(header)) + 2 + len([]byte(candidateBody))
}

func firstTopLevelHeading(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "# "))
	}
	return ""
}

func meaningfulSkillLines(content string) []string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		out = append(out, strings.Join(strings.Fields(line), " "))
	}
	return out
}

func missingRequiredHeadings(originalBody, candidateBody string) []string {
	originalHeadings := skillHeadings(originalBody)
	if len(originalHeadings) == 0 {
		return nil
	}
	candidateHeadings := map[string]struct{}{}
	for _, heading := range skillHeadings(candidateBody) {
		candidateHeadings[heading.normalized] = struct{}{}
	}
	var missing []string
	for _, heading := range originalHeadings {
		if _, ok := candidateHeadings[heading.normalized]; ok {
			continue
		}
		missing = append(missing, heading.display)
		if len(missing) >= 3 {
			break
		}
	}
	return missing
}

type skillHeading struct {
	display    string
	normalized string
}

func skillHeadings(content string) []skillHeading {
	lines := strings.Split(content, "\n")
	out := make([]skillHeading, 0)
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(line, "#"))
		if text == "" {
			continue
		}
		normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, skillHeading{display: text, normalized: normalized})
	}
	return out
}

func countRetainedLines(originalLines, candidateLines []string) int {
	candidateCounts := make(map[string]int, len(candidateLines))
	for _, line := range candidateLines {
		candidateCounts[line]++
	}
	retained := 0
	for _, line := range originalLines {
		if candidateCounts[line] == 0 {
			continue
		}
		retained++
		candidateCounts[line]--
	}
	return retained
}

// --- validation flow (moved from evolver.go, pure move): self-test +
// escalation calling the deterministic gates above. ---

type acceptedSkillCandidate struct {
	Body        string
	Description string
	Audit       HarnessEditAudit
}

// selfTestAndMaybeEscalate judges a candidate rewrite. On pass it returns the
// candidate. On fail it escalates to the teacher model (if wired) for one more
// attempt, then re-judges. ok=false means the caller must keep the original
// skill untouched.
func (e *Evolver) selfTestAndMaybeEscalate(ctx context.Context, entry *skills.SkillEntry, originalContent, candidateBody string, stats *UsageStats, audit HarnessEditAudit, reviewFinding string) (acceptedSkillCandidate, bool, string) {
	teacherClient, teacherModel := e.teacherModelSnapshot()
	hasTeacher := teacherClient != nil && teacherModel != ""

	// Judge != producer. The candidate came from the lightweight model, so a
	// lightweight judge would be grading its own output — same-family /
	// self-preference bias skews toward accepting it (LLM-judge survey
	// arXiv:2508.02994). pickCandidateJudge routes to the teacher when wired.
	judgeClient, judgeModel := e.pickCandidateJudge()
	pass, reason, err := e.validateCandidate(ctx, entry.Skill.Name, judgeClient, judgeModel, originalContent, candidateBody, stats, audit, reviewFinding)
	if err != nil {
		e.logger.Warn("evolver: self-test errored, keeping original",
			"skill", entry.Skill.Name, "error", err)
		return acceptedSkillCandidate{}, false, "judge error"
	}
	if pass {
		return acceptedSkillCandidate{Body: candidateBody, Audit: audit}, true, reason
	}
	e.logger.Info("evolver: self-test rejected lightweight rewrite",
		"skill", entry.Skill.Name, "reason", reason)

	// Teacher-escalation: let the stronger model rewrite once.
	if !hasTeacher {
		return acceptedSkillCandidate{}, false, reason
	}
	teacherCandidate, terr := e.teacherRewrite(ctx, teacherClient, teacherModel, entry.Skill.Name, originalContent, candidateBody, reason, stats)
	if terr != nil || strings.TrimSpace(teacherCandidate.Body) == "" {
		e.logger.Warn("evolver: teacher escalation failed",
			"skill", entry.Skill.Name, "error", terr)
		return acceptedSkillCandidate{}, false, "teacher escalation failed"
	}
	// This rewrite came from the teacher, so judge it with the lightweight model
	// — again keeping judge != producer rather than letting the teacher rubber-
	// stamp its own rewrite. A weaker judge may false-reject a good rewrite, but
	// the loop is fail-closed (keeps the original), so that errs safe.
	primaryClient, primaryModel := e.primaryModel()
	tpass, treason, tjerr := e.validateCandidate(ctx, entry.Skill.Name, primaryClient, primaryModel, originalContent, teacherCandidate.Body, stats, teacherCandidate.Audit, reviewFinding)
	if tjerr != nil || !tpass {
		e.logger.Info("evolver: teacher rewrite still failed self-test",
			"skill", entry.Skill.Name, "reason", treason)
		return acceptedSkillCandidate{}, false, "teacher: " + treason
	}
	e.logger.Info("evolver: teacher escalation succeeded", "skill", entry.Skill.Name)
	return teacherCandidate, true, treason
}

func (e *Evolver) validateCandidate(ctx context.Context, skillName string, client *llm.Client, model, originalContent, candidateBody string, stats *UsageStats, audit HarnessEditAudit, reviewFinding string) (pass bool, reason string, err error) {
	if ok, reason := e.validateCandidatePreflight(skillName, originalContent, candidateBody, audit, stats, reviewFinding); !ok {
		return false, reason, nil
	}
	return e.judgeCandidate(ctx, skillName, client, model, originalContent, candidateBody, stats)
}

func (e *Evolver) validateCandidatePreflight(skillName, originalContent, candidateBody string, audit HarnessEditAudit, stats *UsageStats, reviewFinding string) (bool, string) {
	// covered means a REAL regression check is active: at least one case the
	// held-out gate can score (hasAssertions). Mere case existence let an
	// assertion-less corpus grant the relaxed caps while the gate failed open.
	covered := hasScorableValidationCase(e.validationCasesForCoverage(skillName))
	if ok, reason := validateHermesEvolutionGuardrails(originalContent, candidateBody, covered); !ok {
		return false, reason
	}
	if ok, reason := validateTextualEditBudget(originalContent, candidateBody, covered); !ok {
		return false, reason
	}
	if engine := e.skillValidationEngine(); engine != nil {
		result, err := engine.ValidateCandidate(skillName, originalContent, candidateBody)
		if err != nil {
			if e.logger != nil {
				e.logger.Warn("evolver: held-out validation engine unavailable",
					"skill", skillName, "error", err)
			}
		} else if result.Evaluated && !result.Pass {
			return false, result.Reason
		}
	}
	if ok, reason := validateSelfHarnessAudit(audit, stats, reviewFinding); !ok {
		return false, reason
	}
	if ok, reason := validateSelfHarnessEditedSurface(audit, originalContent, candidateBody); !ok {
		return false, reason
	}
	return true, ""
}

func (e *Evolver) skillValidationEngine() *SkillValidationEngine {
	if e == nil || e.tracker == nil {
		return nil
	}
	if e.validationEngine != nil {
		return e.validationEngine
	}
	return NewSkillValidationEngine(e.tracker, e.logger)
}

// heldOutSelectionMargin scores a candidate body's held-out validation
// improvement over the original (candidate score - original score) for the
// K-candidate selector's ranking (#3). It is the deterministic, judge-free
// margin: a candidate that satisfies more held-out forbidden/required assertions
// ranks higher. Returns 0 when there is no engine, no cases, or the gate could
// not evaluate — so uncovered skills tie and fall back to first-committable
// order. Never blocks: an engine error is logged and treated as a 0 margin.
func (e *Evolver) heldOutSelectionMargin(skillName, originalContent, candidateBody string) float64 {
	engine := e.skillValidationEngine()
	if engine == nil {
		return 0
	}
	result, err := engine.ValidateCandidate(skillName, originalContent, candidateBody)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: held-out selection margin unavailable",
				"skill", skillName, "error", err)
		}
		return 0
	}
	if !result.Evaluated {
		return 0
	}
	return result.CandidateScore - result.OriginalScore
}

// hasScorableValidationCase reports whether any case carries an assertion the
// held-out/behavioral gate can actually score — the honest test for "covered".
func hasScorableValidationCase(cases []SkillValidationCaseRecord) bool {
	for _, tc := range cases {
		if tc.hasAssertions() {
			return true
		}
	}
	return false
}

// validationCasesForPrompt returns recent VISIBLE-pool cases only — what the
// producer/judge/teacher prompts may show as the behavior contract. Blind
// held-out cases never reach an optimization-side prompt, so the gates keep
// scoring assertions the candidate was never shown (gate-echo prevention).
func (e *Evolver) validationCasesForPrompt(skillName string) []SkillValidationCaseRecord {
	if e == nil || e.tracker == nil {
		return nil
	}
	cases, err := e.tracker.RecentSkillValidationCasesPool(skillName, skillEvolutionPromptCaseLimit, false)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: validation cases unavailable for prompt",
				"skill", skillName, "error", err)
		}
		return nil
	}
	return cases
}

// validationCasesForCoverage spans BOTH pools: coverage ("does a scorable case
// exist for this skill?") must count blind cases the prompt deliberately hides,
// otherwise a blind-only skill would wrongly take the uncovered relaxed caps.
func (e *Evolver) validationCasesForCoverage(skillName string) []SkillValidationCaseRecord {
	if e == nil || e.tracker == nil {
		return nil
	}
	cases, err := e.tracker.RecentSkillValidationCases(skillName, skillEvolutionPromptCaseLimit)
	if err != nil {
		if e.logger != nil {
			e.logger.Warn("evolver: validation cases unavailable for coverage",
				"skill", skillName, "error", err)
		}
		return nil
	}
	return cases
}
