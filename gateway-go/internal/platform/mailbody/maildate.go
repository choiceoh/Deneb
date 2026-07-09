package mailbody

import (
	"net/mail"
	"strings"
	"time"
)

// ParseMailDate parses an RFC 5322 Date header value, tolerating the common
// Korean-mail variants (missing weekday, "(KST)" comments handled by net/mail
// already). Returns the zero time when nothing parses (fail-open: callers skip
// the anchor).
//
// The single canonical implementation for both the live analysis path and the
// archive path. Korean groupware sends a bare "... 23:30:00 KST" zone token:
// net/mail parses the unknown abbreviation as UTC+0, which shifts late-evening
// mail to the NEXT calendar day after KST conversion — mis-anchoring the whole
// date table and (on the archive path) mis-filing by day. Rewrite the bare
// token to its numeric offset first. ("(KST)" comments after a numeric offset
// are untouched: no leading space match.)
func ParseMailDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if strings.HasSuffix(raw, " KST") {
		raw = strings.TrimSuffix(raw, " KST") + " +0900"
	}
	if t, err := mail.ParseDate(raw); err == nil {
		return t
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, "2006-01-02 15:04:05 -0700", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
