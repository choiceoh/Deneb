package denebui

import (
	"strings"
)

// rawTextEscaper makes an arbitrary markdown body safe inside a <markdown>
// raw-text element: '<' can never form a close tag, '&' round-trips through
// entity decoding, and '`' can never open a code fence line that would
// terminate the outer ```deneb-ui markdown fence early (the hazard the legacy
// JSON encoding avoided via \n string escaping).
var rawTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", "`", "&#96;")

// attrEscaper makes a string safe inside a double-quoted HTML attribute.
var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

// CollapsedReportFence wraps a long proactive report (e.g. a mail analysis) in
// a deneb-ui accordion so the native chat shows a collapsed title-only card
// that expands in place. The body rides inside a <markdown> raw-text child
// with backticks entity-escaped, so code fences inside the report can never
// terminate the outer fence early.
//
// This is server-side deterministic assembly — not LLM-emitted — so the output
// always validates against the node schema. Returns the body unchanged when
// title or body is blank (callers fall back to plain delivery).
func CollapsedReportFence(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" || strings.TrimSpace(body) == "" {
		return body
	}
	var b strings.Builder
	b.WriteString("```")
	b.WriteString(fenceInfo)
	b.WriteString("\n<accordion title=\"")
	b.WriteString(attrEscaper.Replace(title))
	b.WriteString("\">\n<markdown>")
	b.WriteString(rawTextEscaper.Replace(body))
	b.WriteString("</markdown>\n</accordion>\n```")
	return b.String()
}
