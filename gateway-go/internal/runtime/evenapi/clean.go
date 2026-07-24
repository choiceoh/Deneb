package evenapi

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxG2Runes is the practical HUD budget for a single G2 text reply.
// Community bridges truncate around ~400 characters; keep a little headroom
// for the ellipsis marker.
const MaxG2Runes = 400

var (
	reFenced  = regexp.MustCompile("(?s)```.*?```")
	reInline  = regexp.MustCompile("`([^`]+)`")
	reURL     = regexp.MustCompile(`https?://\S+`)
	reMDLink  = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reHeading = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	reBold    = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	reBullet  = regexp.MustCompile(`(?m)^\s*[-*•]\s+`)
	reMultiNL = regexp.MustCompile(`\n{3,}`)
	reMultiSp = regexp.MustCompile(`[ \t]{2,}`)
)

// CleanForG2 strips markdown/URL noise and truncates to the G2 HUD budget.
func CleanForG2(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = reFenced.ReplaceAllString(s, "")
	s = reInline.ReplaceAllString(s, "$1")
	s = reMDLink.ReplaceAllString(s, "$1")
	s = reURL.ReplaceAllString(s, "")
	s = reHeading.ReplaceAllString(s, "")
	s = reBold.ReplaceAllString(s, "$1$2")
	s = reBullet.ReplaceAllString(s, "· ")
	s = strings.ReplaceAll(s, "*", "")
	s = reMultiNL.ReplaceAllString(s, "\n\n")
	s = reMultiSp.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	return truncateRunes(s, MaxG2Runes)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return "…"
	}
	cut := max - 1
	out := strings.TrimSpace(string(runes[:cut]))
	return out + "…"
}
