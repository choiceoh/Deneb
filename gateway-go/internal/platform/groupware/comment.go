package groupware

import (
	"strings"
	"unicode"
)

// MaxApprovalCommentRunes is the maximum sanitized approval comment length.
const MaxApprovalCommentRunes = 500

// SanitizeApprovalComment normalizes an operator-entered approval comment for
// the groupware mutation boundary. The result is a single line without raw
// angle brackets or control characters and is truncated on a rune boundary.
func SanitizeApprovalComment(comment string) string {
	var normalized strings.Builder
	normalized.Grow(len(comment))
	pendingSpace := false
	wroteContent := false

	for _, r := range strings.TrimSpace(comment) {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '<' || r == '>' {
			pendingSpace = wroteContent
			continue
		}
		if pendingSpace {
			normalized.WriteByte(' ')
			pendingSpace = false
		}
		normalized.WriteRune(r)
		wroteContent = true
	}

	runes := []rune(normalized.String())
	if len(runes) > MaxApprovalCommentRunes {
		runes = runes[:MaxApprovalCommentRunes]
	}
	return strings.TrimSpace(string(runes))
}
