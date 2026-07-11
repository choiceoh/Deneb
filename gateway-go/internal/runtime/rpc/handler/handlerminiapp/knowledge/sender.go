package knowledge

import (
	"net/mail"
	"strings"
)

// ParseSender splits a sender header into a validated email and display name.
func ParseSender(raw string) (email, display string) {
	if addr, err := mail.ParseAddress(raw); err == nil {
		candidate := strings.TrimSpace(addr.Address)
		if LooksLikeEmail(candidate) {
			return candidate, strings.TrimSpace(addr.Name)
		}
	}
	if i := strings.Index(raw, "<"); i >= 0 {
		j := strings.Index(raw[i:], ">")
		if j > 0 {
			candidate := strings.TrimSpace(raw[i+1 : i+j])
			display = strings.TrimSpace(raw[:i])
			display = strings.Trim(display, `"' `)
			if LooksLikeEmail(candidate) {
				return candidate, display
			}
			return "", display
		}
	}
	if LooksLikeEmail(raw) {
		return raw, ""
	}
	return "", raw
}

// LooksLikeEmail reports whether a value is safe to use as an email search term.
func LooksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, " \t\r\n\"'`<>") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at < 1 || at == len(s)-1 {
		return false
	}
	return strings.IndexByte(s[at+1:], '@') < 0
}

func parseSender(raw string) (email, display string) {
	return ParseSender(raw)
}

func looksLikeEmail(s string) bool {
	return LooksLikeEmail(s)
}
