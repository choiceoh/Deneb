// Package guardrails applies deterministic safety checks to generated SKILL.md edits.
package guardrails

import (
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

const (
	editBudgetMinOriginalLines       = 12
	editBudgetMaxChangedRatio        = 0.65
	editBudgetMaxAddedLines          = 80
	editBudgetMaxGrowthMultiple      = 2
	hermesMaxSkillBytes              = 15 * 1024
	MaxChangedSections               = 3
	coveredEditBudgetMaxChangedRatio = 0.85
	coveredHermesMaxChangedSections  = 5
)

// Audit describes the intended scope and behavior change of a generated edit.
type Audit struct {
	TargetSignature        string   `json:"targetSignature,omitempty"`
	EditedSurface          string   `json:"editedSurface,omitempty"`
	ExpectedBehaviorChange string   `json:"expectedBehaviorChange,omitempty"`
	RegressionRisk         string   `json:"regressionRisk,omitempty"`
	PrimaryDimension       string   `json:"primaryDimension,omitempty"`
	SecondaryDimensions    []string `json:"secondaryDimensions,omitempty"`
}

// Empty reports whether the audit carries no transition metadata.
func (a Audit) Empty() bool {
	return strings.TrimSpace(a.TargetSignature) == "" &&
		strings.TrimSpace(a.EditedSurface) == "" &&
		strings.TrimSpace(a.ExpectedBehaviorChange) == "" &&
		strings.TrimSpace(a.RegressionRisk) == "" &&
		strings.TrimSpace(a.PrimaryDimension) == "" &&
		len(a.SecondaryDimensions) == 0
}

// Ptr returns nil for an empty audit and a stable pointer otherwise.
func (a Audit) Ptr() *Audit {
	if a.Empty() {
		return nil
	}
	return &a
}

// NormalizeSignature canonicalizes a Self-Harness failure signature.
func NormalizeSignature(value string) string {
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

// SignatureMatches reports whether either normalized signature contains the other.
func SignatureMatches(target, supported string) bool {
	if target == "" || supported == "" {
		return false
	}
	return target == supported || strings.Contains(target, supported) || strings.Contains(supported, target)
}

// ValidateEditedSurface verifies that an audit names the SKILL.md sections that changed.
func ValidateEditedSurface(audit Audit, originalContent, candidateBody string) (bool, string) {
	surfaces := normalizedEditedSurfaces(audit.EditedSurface)
	if len(surfaces) == 0 {
		return false, "self-harness surface rejected: edited_surface is empty"
	}
	originalBody := bodyOnly(originalContent)
	candidateBody = bodyOnly(candidateBody)
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
		out = append(out, changedSkillSection{Display: display, Canonical: CanonicalSkillSurface(key)})
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
	lines := strings.Split(bodyOnly(content), "\n")
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
	surface = CanonicalSkillSurface(normalizeSkillSurface(surface))
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

// CanonicalSkillSurface maps common heading variants to stable editable surfaces.
func CanonicalSkillSurface(surface string) string {
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

// ValidateTextualEditBudget bounds how much of an existing skill may change at once.
func ValidateTextualEditBudget(originalContent, candidateBody string, covered bool) (bool, string) {
	if strings.TrimSpace(candidateBody) == "" {
		return false, "textual edit budget rejected empty candidate body"
	}
	originalBody := originalContent
	if _, bodyOffset := skills.ExtractFrontmatterBlock(originalContent); bodyOffset > 0 && bodyOffset < len(originalContent) {
		originalBody = originalContent[bodyOffset:]
	}

	originalLines := meaningfulSkillLines(originalBody)
	candidateLines := meaningfulSkillLines(candidateBody)
	if len(originalLines) < editBudgetMinOriginalLines {
		return true, ""
	}
	if len(candidateLines) < max(3, len(originalLines)/3) {
		return false, fmt.Sprintf("textual edit budget exceeded: candidate shrank from %d to %d meaningful lines", len(originalLines), len(candidateLines))
	}
	if len(candidateLines) > len(originalLines)*editBudgetMaxGrowthMultiple && len(candidateLines)-len(originalLines) > editBudgetMaxAddedLines {
		return false, fmt.Sprintf("textual edit budget exceeded: candidate grew from %d to %d meaningful lines", len(originalLines), len(candidateLines))
	}
	if missing := missingRequiredHeadings(originalBody, candidateBody); len(missing) > 0 {
		return false, fmt.Sprintf("textual edit budget exceeded: candidate removed required headings: %s", strings.Join(missing, ", "))
	}

	retained := countRetainedLines(originalLines, candidateLines)
	changedRatio := 1 - float64(retained)/float64(len(originalLines))
	maxRatio := editBudgetMaxChangedRatio
	if covered {
		maxRatio = coveredEditBudgetMaxChangedRatio
	}
	if changedRatio > maxRatio {
		return false, fmt.Sprintf("textual edit budget exceeded: changed %.0f%% of meaningful lines (max %.0f%%)", changedRatio*100, maxRatio*100)
	}
	return true, ""
}

// ValidateHermesEvolutionGuardrails rejects oversized or broad semantic rewrites.
func ValidateHermesEvolutionGuardrails(originalContent, candidateBody string, covered bool) (bool, string) {
	candidateBody = bodyOnly(candidateBody)
	size := candidateSkillBytes(originalContent, candidateBody)
	if size > hermesMaxSkillBytes {
		return false, fmt.Sprintf("Hermes patch-first gate rejected: candidate SKILL.md size %d bytes exceeds %d byte limit", size, hermesMaxSkillBytes)
	}

	originalBody := bodyOnly(originalContent)
	originalTitle := firstTopLevelHeading(originalBody)
	candidateTitle := firstTopLevelHeading(candidateBody)
	if originalTitle != "" && candidateTitle != "" &&
		normalizeSkillSurface(originalTitle) != normalizeSkillSurface(candidateTitle) {
		return false, fmt.Sprintf("Hermes semantic-preservation gate rejected: title changed from %q to %q", originalTitle, candidateTitle)
	}

	originalLines := meaningfulSkillLines(originalBody)
	if len(originalLines) >= editBudgetMinOriginalLines {
		maxSections := MaxChangedSections
		if covered {
			maxSections = coveredHermesMaxChangedSections
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

// NormalizedSkillHeadings returns unique normalized headings in document order.
func NormalizedSkillHeadings(content string) []string {
	headings := skillHeadings(content)
	out := make([]string, 0, len(headings))
	for _, heading := range headings {
		out = append(out, heading.normalized)
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

func bodyOnly(content string) string {
	_, bodyOffset := skills.ExtractFrontmatterBlock(content)
	if bodyOffset > 0 && bodyOffset < len(content) {
		return strings.TrimSpace(content[bodyOffset:])
	}
	return strings.TrimSpace(content)
}
