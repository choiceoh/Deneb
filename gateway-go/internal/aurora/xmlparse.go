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
	// Narrative is the concise summary text (from <summary>).
	Narrative string `json:"narrative,omitempty"`
	// Decisions is a JSON array of decision strings (from <decisions>).
	Decisions string `json:"decisions,omitempty"`
	// Pending is a JSON array of pending items (from <pending>).
	Pending string `json:"pending,omitempty"`
	// Refs is a JSON array of reference strings (from <references>).
	Refs string `json:"refs,omitempty"`
	// Content is the full original text (stored for backward compatibility).
	Content string `json:"-"`
}

// ParseStructuredSummary extracts structured sections from XML-formatted
// LLM output. If the text doesn't contain XML tags, it returns the full
// text as the narrative (backward compatibility with plain-text summaries).
func ParseStructuredSummary(text string) StructuredSummary {
	result := StructuredSummary{Content: text}

	summary := extractTag(text, "summary")
	decisions := extractTag(text, "decisions")
	pending := extractTag(text, "pending")
	refs := extractTag(text, "references")

	// If no XML tags found, treat as plain-text summary.
	if summary == "" && decisions == "" && pending == "" && refs == "" {
		result.Narrative = strings.TrimSpace(text)
		return result
	}

	result.Narrative = strings.TrimSpace(summary)
	if decisions != "" {
		result.Decisions = bulletListToJSON(decisions)
	}
	if pending != "" {
		result.Pending = bulletListToJSON(pending)
	}
	if refs != "" {
		result.Refs = bulletListToJSON(refs)
	}

	return result
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
