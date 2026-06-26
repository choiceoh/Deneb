// notify_status.go — notifyService Korean rendering: the status snapshot
// report, per-session activity lines, error-event alert formatting, and the
// small string/duration formatting helpers. Split from notify_relay.go
// (pure move).
package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// buildStatusReport formats the current session manager state as a Korean
// summary. Public-shaped (lowercase b on the function but unexported pkg)
// so unit tests can assert formatting without spinning up a Telegram client.
func (n *notifyService) buildStatusReport(now time.Time) string {
	if n.sessions == nil {
		return "📡 게이트웨이 상태\n세션 매니저 미초기화."
	}
	all := n.sessions.List()
	running := make([]*session.Session, 0, len(all))
	for _, s := range all {
		if s == nil {
			continue
		}
		if s.Status == session.StatusRunning {
			running = append(running, s)
		}
	}

	var b strings.Builder
	b.WriteString("📡 게이트웨이 상태 — ")
	b.WriteString(now.Format("2006-01-02 15:04:05"))
	b.WriteString("\n")
	if len(running) == 0 {
		b.WriteString("실행 중인 세션 없음. (대기 상태)")
		n.appendCacheLine(&b)
		return b.String()
	}

	// Newest session first — most recently active is most likely what the
	// user wants to see.
	sort.SliceStable(running, func(i, j int) bool {
		return running[i].UpdatedAt > running[j].UpdatedAt
	})

	fmt.Fprintf(&b, "활성 세션 %d개:\n", len(running))
	for _, s := range running {
		label := s.Label
		if label == "" {
			label = "(라벨 없음)"
		}
		started := ""
		if s.StartedAt != nil {
			elapsed := now.Sub(time.UnixMilli(*s.StartedAt))
			started = fmt.Sprintf(", %s 경과", humanDuration(elapsed))
		}
		fmt.Fprintf(&b, "• %s — %s%s\n", s.Key, label, started)
		if line := n.activityLineKO(s.Key, now); line != "" {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		if s.Model != "" {
			fmt.Fprintf(&b, "  모델: %s\n", s.Model)
		}
		if s.LastOutput != "" {
			fmt.Fprintf(&b, "  최근 응답: %s\n", truncate(s.LastOutput, 120))
		}
	}
	n.appendCacheLine(&b)
	return strings.TrimRight(b.String(), "\n")
}

// appendCacheLine writes the one-line vLLM prefix-cache hit-rate status into the
// status snapshot, when available. No-op when the cacheSummary accessor is unset
// or has nothing yet (non-vLLM host, or no /health probe has scraped) — the
// snapshot stays unchanged on deployments where the signal does not apply.
func (n *notifyService) appendCacheLine(b *strings.Builder) {
	if n.cacheSummary == nil {
		return
	}
	if line := n.cacheSummary(); line != "" {
		fmt.Fprintf(b, "\n🧠 %s", line)
	}
}

// activityLineKO renders the per-session in-flight tool activity as a
// Korean status line, or "" when nothing has been recorded for the
// session. Distinguishes:
//
//   - running tool: "🔧 X 도구 실행 중 (12초째)"
//   - errored tool: "✗ X 도구 실패 (5분 전)"
//   - completed tool: "✓ X 도구 완료 (5분 전)"
//
// Activity older than 30 minutes is suppressed — stale state from a
// long-idle session would mislead the operator.
func (n *notifyService) activityLineKO(sessionKey string, now time.Time) string {
	e := n.activityFor(sessionKey)
	if e == nil || e.tool == "" {
		return ""
	}
	age := now.Sub(e.updated)
	if age > 30*time.Minute {
		return ""
	}
	switch {
	case e.running:
		return fmt.Sprintf("🔧 %s 도구 실행 중 (%s째)", e.tool, humanDuration(age))
	case e.isError:
		return fmt.Sprintf("✗ %s 도구 실패 (%s 전)", e.tool, humanDuration(age))
	default:
		return fmt.Sprintf("✓ %s 도구 완료 (%s 전)", e.tool, humanDuration(age))
	}
}

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
