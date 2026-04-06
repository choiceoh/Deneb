// Package server — Telegram /status dashboard command.
//
// Extracted from inbound.go: status dashboard rendering with gateway info,
// session state, model, token usage, provider API usage, and channel health.
package server

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

// handleStatusDashboardCommand sends a combined gateway + session status message.
func (p *InboundProcessor) handleStatusDashboardCommand(chatID, sessionKey string) {
	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	var b strings.Builder
	b.Grow(1024)

	b.WriteString("<b>📊 상태 대시보드</b>\n")
	b.WriteString("──────────────────\n\n")

	// Gateway info.
	if p.server.version != "" {
		b.WriteString("🖥️ <b>Gateway:</b> v")
		b.WriteString(html.EscapeString(p.server.version))
		if !p.server.startedAt.IsZero() {
			b.WriteString(" | Uptime: ")
			b.WriteString(formatDashboardUptime(time.Since(p.server.startedAt)))
		}
		b.WriteByte('\n')
	}

	b.WriteString("🔧 <b>Core:</b> pure Go")

	// Session count.
	if p.server.sessions != nil {
		fmt.Fprintf(&b, " | Sessions: %d", p.server.sessions.Count())
	}

	// WS connections.
	fmt.Fprintf(&b, " | WS: %d\n", p.server.clientCnt.Load())
	b.WriteByte('\n')

	// Current session info.
	b.WriteString("<b>📋 세션</b>\n")
	sess := p.server.sessions.Get(sessionKey)
	if sess != nil {
		statusIcon := "🟢"
		switch sess.Status {
		case session.StatusRunning:
			statusIcon = "🔄"
		case session.StatusFailed:
			statusIcon = "❌"
		case session.StatusKilled:
			statusIcon = "⛔"
		case session.StatusTimeout:
			statusIcon = "⏰"
		}
		fmt.Fprintf(&b, "%s <b>상태:</b> %s\n", statusIcon, string(sess.Status))
	}

	// Current model.
	currentModel := p.chatHandler.DefaultModel()
	if currentModel == "" && p.server.modelRegistry != nil {
		currentModel = p.server.modelRegistry.FullModelID(modelrole.RoleMain)
	}
	if currentModel != "" {
		b.WriteString("🤖 <b>모델:</b> <code>")
		b.WriteString(html.EscapeString(currentModel))
		b.WriteString("</code>\n")
	}

	// Mode settings.
	if sess != nil {
		var modes []string
		if sess.ThinkingLevel != "" && sess.ThinkingLevel != "off" {
			modes = append(modes, "Think: "+sess.ThinkingLevel)
		}
		if sess.FastMode != nil && *sess.FastMode {
			modes = append(modes, "Fast: on")
		}
		if sess.ReasoningLevel != "" && sess.ReasoningLevel != "off" {
			modes = append(modes, "Reasoning: "+sess.ReasoningLevel)
		}
		if sess.ElevatedLevel != "" && sess.ElevatedLevel != "off" {
			modes = append(modes, "Elevated: "+sess.ElevatedLevel)
		}
		if sess.ToolPreset != "" {
			modes = append(modes, "Preset: "+sess.ToolPreset)
		}
		if len(modes) > 0 {
			b.WriteString("⚙️ <b>모드:</b> ")
			b.WriteString(html.EscapeString(strings.Join(modes, " | ")))
			b.WriteByte('\n')
		}

		// Token usage.
		if sess.TotalTokens != nil && *sess.TotalTokens > 0 {
			in, out := int64(0), int64(0)
			if sess.InputTokens != nil {
				in = *sess.InputTokens
			}
			if sess.OutputTokens != nil {
				out = *sess.OutputTokens
			}
			fmt.Fprintf(&b, "📊 <b>토큰:</b> %s (in: %s, out: %s)\n",
				formatDashboardTokens(*sess.TotalTokens),
				formatDashboardTokens(in),
				formatDashboardTokens(out))
		}

		// Failure reason.
		if sess.FailureReason != "" {
			b.WriteString("⚠️ <b>마지막 오류:</b> ")
			b.WriteString(html.EscapeString(sess.FailureReason))
			b.WriteByte('\n')
		}
	}

	b.WriteByte('\n')

	// Per-provider API usage.
	if p.server.usageTracker != nil {
		report := p.server.usageTracker.Status()
		if report != nil && len(report.Providers) > 0 {
			b.WriteString("<b>📈 API 사용량</b>\n")
			names := make([]string, 0, len(report.Providers))
			for name := range report.Providers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				ps := report.Providers[name]
				total := ps.Tokens.Input + ps.Tokens.Output
				fmt.Fprintf(&b, "  %s — %s회, %s tokens\n",
					html.EscapeString(name),
					formatDashboardTokens(ps.Calls),
					formatDashboardTokens(total))
			}
			b.WriteByte('\n')
		}
	}

	// Channel health.
	if p.server.channelHealth != nil {
		snapshot := p.server.channelHealth.HealthSnapshot()
		if len(snapshot) > 0 {
			b.WriteString("<b>📡 채널</b>\n")
			for _, ch := range snapshot {
				icon := "💚"
				status := "정상"
				if !ch.Healthy {
					icon = "❌"
					status = "비정상"
					if ch.Reason != "" {
						status = ch.Reason
					}
				}
				fmt.Fprintf(&b, "  %s %s: %s\n", icon,
					html.EscapeString(ch.ChannelID),
					html.EscapeString(status))
			}
		}
	}

	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := telegram.SendText(ctx, client, id, b.String(), telegram.SendOptions{
		ParseMode: "HTML",
	}); err != nil {
		p.logger.Warn("failed to send status dashboard", "error", err)
	}
}

// formatDashboardUptime formats a duration as compact uptime (e.g. "2d 5h 32m").
func formatDashboardUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// formatDashboardTokens formats token counts in compact form (e.g. "1.2M", "890K").
func formatDashboardTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
