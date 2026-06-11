package denebui

import (
	"encoding/json"
	"strings"
)

// CollapsedReportFence wraps a long proactive report (e.g. a mail analysis) in
// a deneb-ui accordion so the native chat shows a collapsed title-only card
// that expands in place. The body rides inside a "markdown" child node, so the
// JSON string escaping (newlines become \n) guarantees the outer fence can
// never be terminated early by code fences inside the report.
//
// This is server-side deterministic assembly — not LLM-emitted — so the output
// always validates against the node schema. Returns the body unchanged when
// title or body is blank (callers fall back to plain delivery).
func CollapsedReportFence(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" || strings.TrimSpace(body) == "" {
		return body
	}
	root := map[string]any{
		"type":  "accordion",
		"title": title,
		"children": []map[string]any{
			{"type": "markdown", "value": body},
		},
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		// Marshal of string-keyed maps with string values cannot fail in
		// practice; degrade to the raw body rather than dropping the report.
		return body
	}
	return "```" + FenceInfo + "\n" + string(encoded) + "\n```"
}
