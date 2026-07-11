// notify_status.go — notifyService Korean rendering: error-event alert
// formatting and the small string/duration formatting helpers. (The Telegram-era
// status-snapshot report and per-session activity lines were retired — nothing
// reached them after the bot's /status surface was replaced by the native
// client's live SSE/status RPCs.)
package notify

import (
	"fmt"
	"strings"
	"time"
)

// formatErrorEvent renders a monitored broadcast event as a Korean alert
// line. Returns "" when the event isn't recognized — defensive guard for
// the tap filter (which already excludes unknowns).
func formatErrorEvent(event string, payload any) string {
	fields, _ := payload.(map[string]any)

	headline := errorHeadlineKO(event)
	if headline == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("⚠️ ")
	b.WriteString(headline)
	sess := stringField(fields, "session")
	if sess == "" {
		sess = stringField(fields, "sessionKey")
	}
	if sess != "" {
		fmt.Fprintf(&b, "\n세션: %s", sess)
	}
	if tool := stringField(fields, "tool"); tool != "" {
		fmt.Fprintf(&b, "\n도구: %s", tool)
	}
	if reason := stringField(fields, "reason"); reason != "" {
		fmt.Fprintf(&b, "\n원인: %s", reason)
	}
	if errMsg := stringField(fields, "error"); errMsg != "" {
		fmt.Fprintf(&b, "\n에러: %s", truncate(errMsg, 200))
	}
	return b.String()
}

// errorHeadlineKO maps the broadcast event name to a Korean headline. Kept
// alongside mirroredEvents so adding a new monitored event requires both
// the filter and the headline to be updated together.
func errorHeadlineKO(event string) string {
	switch event {
	case "chat.delivery_failed":
		return "채팅 응답 전달 실패"
	case "chat.media_delivery_failed":
		return "미디어 전달 실패"
	case "chat.tool_failed":
		return "도구 실행 실패"
	case "chat.context_overflow_unrecoverable":
		return "컨텍스트 오버플로 (복구 불가)"
	case "chat.compaction_stuck":
		return "컨텍스트 압축 중단"
	default:
		return ""
	}
}

// stringField returns the field value as a string, or "" when missing.
// Tolerates nil maps so the caller need not guard.
func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// truncate clamps s to maxRunes runes (not bytes) and appends ellipsis.
// Korean text is multi-byte; rune count keeps the cap visually predictable.
func truncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

// humanDuration formats a duration as Korean shorthand: "30초", "5분",
// "2시간 13분". Coarse on purpose — the monitoring chat shows snapshots,
// not millisecond-grade telemetry.
func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d초", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%d분", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) - hours*60
	if mins == 0 {
		return fmt.Sprintf("%d시간", hours)
	}
	return fmt.Sprintf("%d시간 %d분", hours, mins)
}
