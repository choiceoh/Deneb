package nativepush

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// Titles may already carry a topic prefix (e.g. "🔴 플릿 · node down: srv3").
	// Keep enough room for that grammar; the glasses OS truncates further if needed.
	hudTitleMaxRunes = 40
	hudBodyMaxRunes  = 120
)

var (
	hudFenced  = regexp.MustCompile("(?s)```.*?```")
	hudInline  = regexp.MustCompile("`([^`]+)`")
	hudURL     = regexp.MustCompile(`https?://\S+`)
	hudMDLink  = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	hudHeading = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	hudBold    = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	hudSpace   = regexp.MustCompile(`\s+`)
)

// FormatHUDPush rewrites a notification into a one-line G2-friendly grammar:
// short title + single-line body, no markdown/URLs. Command frames
// (phone_action / workspace) are left unchanged.
func FormatHUDPush(ev Event) Event {
	if ev.Kind == PushKindPhoneAction || ev.Kind == PushKindWorkspace || ev.Kind == PushKindComputer {
		return ev
	}
	title := strings.TrimSpace(ev.Title)
	body := flattenHUDText(ev.Body)
	if title == "" {
		title = "Deneb"
	}
	title = truncateHUD(stripHUDMarkup(title), hudTitleMaxRunes)
	if body == "" {
		body = "새 알림"
	} else {
		body = truncateHUD(body, hudBodyMaxRunes)
	}
	// Keep topic signal when the title was the generic product name.
	if strings.EqualFold(title, "Deneb") && body != "" {
		topic := hudTopic(ev.Kind)
		if topic != "" {
			title = truncateHUD("Deneb · "+topic, hudTitleMaxRunes)
		}
	}
	ev.Title = title
	ev.Body = body
	return ev
}

func hudTopic(kind string) string {
	switch kind {
	case PushKindWorkfeed:
		return "업무"
	case PushKindFleet:
		return "플릿"
	default:
		return ""
	}
}

func flattenHUDText(raw string) string {
	s := stripHUDMarkup(raw)
	s = hudSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func stripHUDMarkup(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = hudFenced.ReplaceAllString(s, "")
	s = hudInline.ReplaceAllString(s, "$1")
	s = hudMDLink.ReplaceAllString(s, "$1")
	s = hudURL.ReplaceAllString(s, "")
	s = hudHeading.ReplaceAllString(s, "")
	s = hudBold.ReplaceAllString(s, "$1$2")
	s = strings.ReplaceAll(s, "*", "")
	return strings.TrimSpace(s)
}

func truncateHUD(s string, max int) string {
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
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}
