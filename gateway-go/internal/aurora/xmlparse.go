// XML section parser for structured compaction summaries.
//
// Extracts <summary>, <decisions>, <pending>, and <references> sections
// from LLM-generated XML summaries. Tolerant of malformed XML — falls
// back to treating the entire text as the summary narrative.
package aurora

import (
	"encoding/json"
	"strings"
)

// StructuredSummary holds the parsed sections of a structured XML summary.
type StructuredSummary struct {
	// Goal is the user's overarching objective (from <goal>).
	Goal string `json:"goal,omitempty"`
	// Narrative is the progress/work narrative (from <progress> or legacy <summary>).
	Narrative string `json:"narrative,omitempty"`
	// Decisions is a JSON array of decision strings (from <key_decisions> or legacy <decisions>).
	Decisions string `json:"decisions,omitempty"`
	// Pending is a JSON array of pending items (from legacy <pending>).
	// Superseded by NextSteps in the structured template.
	Pending string `json:"pending,omitempty"`
	// Refs is a JSON array of reference strings (from <relevant_files> or legacy <references>).
	Refs string `json:"refs,omitempty"`
	// NextSteps is a JSON array of next actions (from <next_steps>).
	NextSteps string `json:"nextSteps,omitempty"`
	// CriticalContext is verbatim values that must be preserved (from <critical_context>).
	CriticalContext string `json:"criticalContext,omitempty"`
	// Content is the full original text (stored for backward compatibility).
	Content string `json:"-"`
}

// ParseStructuredSummary extracts structured sections from XML-formatted
// LLM output. Supports both the new structured template (<goal>, <progress>,
// <key_decisions>, <relevant_files>, <next_steps>, <critical_context>) and
// the legacy format (<summary>, <timeline>, <decisions>, <pending>, <references>).
// If the text doesn't contain XML tags, it returns the full text as the narrative.
func ParseStructuredSummary(text string) StructuredSummary {
	result := StructuredSummary{Content: text}

	// Try new structured template sections first.
	goal := extractTag(text, "goal")
	progress := extractTag(text, "progress")
	keyDecisions := extractTag(text, "key_decisions")
	relevantFiles := extractTag(text, "relevant_files")
	nextSteps := extractTag(text, "next_steps")
	criticalContext := extractTag(text, "critical_context")

	// Fall back to legacy sections if new ones are absent.
	summary := extractTag(text, "summary")
	decisions := extractTag(text, "decisions")
	pending := extractTag(text, "pending")
	refs := extractTag(text, "references")

	hasNew := goal != "" || progress != "" || nextSteps != ""
	hasLegacy := summary != "" || decisions != "" || pending != "" || refs != ""

	// If no XML tags found at all, treat as plain-text summary.
	if !hasNew && !hasLegacy {
		result.Narrative = strings.TrimSpace(text)
		return result
	}

	// Goal (new template only).
	result.Goal = strings.TrimSpace(goal)

	// Narrative: prefer <progress> (new), fall back to <summary> (legacy).
	if progress != "" {
		result.Narrative = strings.TrimSpace(progress)
	} else {
		result.Narrative = strings.TrimSpace(summary)
	}

	// Decisions: prefer <key_decisions> (new), fall back to <decisions> (legacy).
	if d := firstNonEmpty(keyDecisions, decisions); d != "" {
		result.Decisions = bulletListToJSON(d)
	}

	// Pending: from legacy <pending> only (new template uses <next_steps>).
	if pending != "" {
		result.Pending = bulletListToJSON(pending)
	}

	// Refs: prefer <relevant_files> (new), fall back to <references> (legacy).
	if r := firstNonEmpty(relevantFiles, refs); r != "" {
		result.Refs = bulletListToJSON(r)
	}

	// Next steps (new template only).
	if nextSteps != "" {
		result.NextSteps = bulletListToJSON(nextSteps)
	}

	// Critical context (new template only) — stored as-is, not bullet-parsed.
	result.CriticalContext = strings.TrimSpace(criticalContext)

	return result
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// extractTag extracts content between <tag> and </tag>.
// Returns empty string if the tag is not found.
func extractTag(text, tag string) string {
	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"

	start := strings.Index(text, openTag)
	if start == -1 {
		return ""
	}
	start += len(openTag)

	end := strings.Index(text[start:], closeTag)
	if end == -1 {
		// Unclosed tag — take everything after the open tag.
		return text[start:]
	}

	return text[start : start+end]
}

// bulletListToJSON converts a bullet-point list (lines starting with "- ")
// into a JSON array of strings.
func bulletListToJSON(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var items []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip bullet prefix.
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	if len(items) == 0 {
		return ""
	}
	b, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(b)
}
