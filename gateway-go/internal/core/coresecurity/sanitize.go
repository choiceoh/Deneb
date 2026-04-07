package coresecurity

import "strings"

// SanitizeHTML escapes HTML-significant characters to prevent XSS.
// Matches Rust sanitize_html: < > & " ' → entity references.
func SanitizeHTML(input string) string {
	// Fast path: no special chars — return as-is (zero alloc).
	if !strings.ContainsAny(input, "<>&\"'") {
		return input
	}
	var b strings.Builder
	b.Grow(len(input) + len(input)/4)
	for _, r := range input {
		switch r {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#x27;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
