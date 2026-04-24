package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// handleSlashCommand processes a recognized slash command and returns a response.
// This runs synchronously (no agent loop) and delivers a reply to the channel.
func (h *Handler) handleSlashCommand(
	reqID string,
	sessionKey string,
	delivery *DeliveryContext,
	cmd *SlashResult,
) *protocol.ResponseFrame {
	switch cmd.Command {
	case "reset":
		// Abort any active run, clear transcript, and discard frozen context snapshot.
		h.InterruptActiveRun(sessionKey)
		h.pending.Clear(sessionKey)
		h.mergeWindow.Clear(sessionKey)
		if h.steer != nil {
			h.steer.Clear(sessionKey)
		}
		prompt.ClearSessionSnapshot(sessionKey)
		if h.transcript != nil {
			if err := h.transcript.Delete(sessionKey); err != nil {
				h.logger.Warn("failed to delete transcript on reset", "error", err)
			}
		}
		// Clear tool preset so session exits any preset mode (e.g. conversation).
		if sess := h.sessions.Get(sessionKey); sess != nil && sess.ToolPreset != "" {
			sess.ToolPreset = ""
			_ = h.sessions.Set(sess) // best-effort: in-memory store, error unreachable
		}
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		h.deliverSlashResponse(delivery, "세션이 초기화되었습니다.")

	case "kill":
		h.InterruptActiveRun(sessionKey)
		h.pending.Clear(sessionKey)
		h.mergeWindow.Clear(sessionKey)
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		h.deliverSlashResponse(delivery, "실행이 중단되었습니다.")

	case "status":
		status := h.buildSessionStatus(sessionKey)
		h.deliverSlashResponse(delivery, status)

	case "model":
		if cmd.Args != "" {
			modelArg := cmd.Args
			displayName := modelArg
			// Accept role names ("main", "lightweight", etc.) as well as model IDs.
			if h.registry != nil {
				if resolved, role, ok := h.registry.ResolveModel(modelArg); ok {
					modelArg = resolved
					displayName = fmt.Sprintf("%s (%s)", modelArg, string(role))
				}
			}
			h.SetDefaultModel(modelArg)
			h.deliverSlashResponse(delivery, fmt.Sprintf("모델이 %s(으)로 변경되었습니다.", displayName))
		}

	case "think":
		h.deliverSlashResponse(delivery, "사고 모드가 토글되었습니다.")

	case "mode":
		sess := h.sessions.Get(sessionKey)
		if sess == nil {
			h.deliverSlashResponse(delivery, "세션이 없습니다.")
			break
		}
		arg := strings.ToLower(strings.TrimSpace(cmd.Args))
		switch arg {
		case "대화", "chat", "conversation":
			sess.Mode = session.ModeChat
			sess.ToolPreset = "conversation"
			_ = h.sessions.Set(sess) // best-effort: in-memory store, error unreachable
			h.deliverSlashResponse(delivery, "💬 대화 모드 — 도구 없이 대화만 합니다.")
		case "일반", "normal":
			sess.Mode = session.ModeNormal
			sess.ToolPreset = ""
			_ = h.sessions.Set(sess) // best-effort: in-memory store, error unreachable
			h.deliverSlashResponse(delivery, "🔧 일반 모드 — 모든 도구를 사용합니다.")
		case "":
			// Toggle: normal ↔ chat
			if sess.Mode == session.ModeChat {
				sess.Mode = session.ModeNormal
				sess.ToolPreset = ""
				_ = h.sessions.Set(sess) // best-effort: in-memory store, error unreachable
				h.deliverSlashResponse(delivery, "🔧 일반 모드 — 모든 도구를 사용합니다.")
			} else {
				sess.Mode = session.ModeChat
				sess.ToolPreset = "conversation"
				_ = h.sessions.Set(sess) // best-effort: in-memory store, error unreachable
				h.deliverSlashResponse(delivery, "💬 대화 모드 — 도구 없이 대화만 합니다.")
			}
		default:
			h.deliverSlashResponse(delivery, "사용법: /mode [일반|대화] — 인자 없이 토글")
		}

	case "mail":
		mailLogger := h.logger
		go func() {
			defer func() {
				if r := recover(); r != nil && mailLogger != nil {
					mailLogger.Error("panic in /mail command handler", "panic", r)
				}
			}()
			h.handleMailCommand(reqID, sessionKey, delivery)
		}()

	case "insights":
		insightsLogger := h.logger
		go func() {
			defer func() {
				if r := recover(); r != nil && insightsLogger != nil {
					insightsLogger.Error("panic in /insights command handler", "panic", r)
				}
			}()
			h.handleInsightsCommand(delivery, cmd.Args)
		}()

	}

	return protocol.MustResponseOK(reqID, map[string]any{
		"command": cmd.Command,
		"handled": true,
	})
}

// deliverSlashResponse sends a slash command response back to the originating channel.
func (h *Handler) deliverSlashResponse(delivery *DeliveryContext, text string) {
	fn := h.ReplyFn()
	if fn == nil || delivery == nil || text == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := fn(ctx, delivery, text); err != nil {
		h.logger.Warn("slash command reply failed", "error", err)
	}
}

// handleInsightsCommand renders a usage report and delivers it to the channel.
// Takes an optional numeric argument for the lookback window in days (default 30).
// Falls back to a friendly notice if the insights provider is not wired.
func (h *Handler) handleInsightsCommand(delivery *DeliveryContext, args string) {
	provider := h.InsightsProvider()
	if provider == nil {
		h.deliverSlashResponse(delivery, "사용량 리포트가 현재 비활성화되어 있습니다.")
		return
	}
	days := 30
	if trimmed := strings.TrimSpace(args); trimmed != "" {
		if parsed, err := parseInsightsDays(trimmed); err == nil {
			days = parsed
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	md, err := provider(ctx, days)
	if err != nil {
		h.logger.Error("insights report generation failed", "days", days, "error", err)
		h.deliverSlashResponse(delivery, "사용량 리포트 생성에 실패했습니다. 잠시 후 다시 시도해 주세요.")
		return
	}
	h.deliverSlashResponse(delivery, md)
}

// parseInsightsDays parses and clamps the /insights day argument.
// Accepts 1..365; otherwise returns an error so caller keeps default.
func parseInsightsDays(s string) (int, error) {
	// Strip a trailing "일" if present ("/insights 7일").
	s = strings.TrimSuffix(s, "일")
	s = strings.TrimSpace(s)
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("days must be positive")
	}
	if n > 365 {
		n = 365
	}
	return n, nil
}

// handleMailCommand fetches the Gmail inbox and either responds directly (no mail)
// or starts an LLM run with the inbox data for analysis.
func (h *Handler) handleMailCommand(reqID, sessionKey string, delivery *DeliveryContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := gmail.DefaultClient()
	if err != nil {
		h.deliverSlashResponse(delivery, "📬 Gmail 인증 정보를 찾을 수 없습니다.")
		return
	}

	unread, err := client.Search(ctx, "is:unread", 10)
	if err != nil {
		h.deliverSlashResponse(delivery, fmt.Sprintf("📬 메일 조회 실패: %s", err))
		return
	}

	var mailPrompt string
	if len(unread) > 0 {
		inbox := gmail.FormatSearchResults(unread)
		mailPrompt = fmt.Sprintf("안 읽은 메일 %d건을 확인했어. 각 메일을 분석해서 요약해줘.\n\n%s", len(unread), inbox)
	} else {
		// No unread — fetch recent mail instead.
		recent, err := client.Search(ctx, "newer_than:3d", 10)
		if err != nil || len(recent) == 0 {
			h.deliverSlashResponse(delivery, "📬 새로운 메일이 없습니다.")
			return
		}
		inbox := gmail.FormatSearchResults(recent)
		mailPrompt = fmt.Sprintf("안 읽은 메일은 없어. 최근 메일 %d건을 확인했어. 요약해줘.\n\n%s", len(recent), inbox)
	}
	h.startAsyncRun(reqID, RunParams{
		SessionKey: sessionKey,
		Message:    mailPrompt,
		Delivery:   delivery,
	}, false)
}

// buildSessionStatus constructs a human-readable session status string.
func (h *Handler) buildSessionStatus(sessionKey string) string {
	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		return fmt.Sprintf("세션 %q: 정보 없음", sessionKey)
	}
	model := h.DefaultModel()
	if model == "" && h.registry != nil {
		model = h.registry.FullModelID(modelrole.RoleMain)
	}

	var sections []string

	// Session + status.
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
	default:
		// StatusDone, StatusIdle, etc.
	}
	sections = append(sections, fmt.Sprintf("📋 **세션:** `%s` %s %s", sessionKey, statusIcon, string(sess.Status)))

	// Model.
	if model != "" {
		sections = append(sections, fmt.Sprintf("🤖 **모델:** %s", model))
	}

	// Mode settings.
	var modes []string
	if sess.ThinkingLevel != "" && sess.ThinkingLevel != "off" {
		modes = append(modes, fmt.Sprintf("Think: %s", sess.ThinkingLevel))
	}
	if sess.FastMode != nil && *sess.FastMode {
		modes = append(modes, "Fast: on")
	}
	if sess.ReasoningLevel != "" && sess.ReasoningLevel != "off" {
		modes = append(modes, fmt.Sprintf("Reasoning: %s", sess.ReasoningLevel))
	}
	if sess.ElevatedLevel != "" && sess.ElevatedLevel != "off" {
		modes = append(modes, fmt.Sprintf("Elevated: %s", sess.ElevatedLevel))
	}
	if sess.ToolPreset != "" {
		presetLabel := sess.ToolPreset
		if sess.ToolPreset == "conversation" {
			presetLabel = "대화모드"
		}
		modes = append(modes, fmt.Sprintf("Preset: %s", presetLabel))
	}
	if len(modes) > 0 {
		sections = append(sections, "⚙️ **모드:** "+strings.Join(modes, " | "))
	}

	// Token usage from session.
	memBudget := h.contextCfg.MemoryTokenBudget
	if sess.TotalTokens != nil && *sess.TotalTokens > 0 {
		in, out := int64(0), int64(0)
		if sess.InputTokens != nil {
			in = *sess.InputTokens
		}
		if sess.OutputTokens != nil {
			out = *sess.OutputTokens
		}
		usagePct := float64(*sess.TotalTokens) / float64(memBudget) * 100
		if usagePct > 100 {
			usagePct = 100
		}
		sections = append(sections, fmt.Sprintf("📊 **토큰:** %s / %s (%s %.0f%%) in: %s, out: %s",
			formatCompactTokens(*sess.TotalTokens), formatCompactTokens(int64(memBudget)), //nolint:gosec // G115 — memBudget is a practical token count, never near int64 overflow
			buildUsageBar(usagePct), usagePct,
			formatCompactTokens(in), formatCompactTokens(out)))
	} else {
		sections = append(sections, fmt.Sprintf("📊 **토큰:** 0 / %s", formatCompactTokens(int64(memBudget)))) //nolint:gosec // G115 — memBudget is a practical token count
	}

	// Channel.
	if sess.Channel != "" {
		sections = append(sections, fmt.Sprintf("📡 **채널:** %s", sess.Channel))
	}

	// Active runs.
	activeRuns := h.abort.CountForSession(sessionKey)
	if activeRuns > 0 {
		sections = append(sections, fmt.Sprintf("🏃 **실행 중:** %d개", activeRuns))
	}

	// Pending messages.
	pendingCount := h.pending.Len(sessionKey)
	if pendingCount > 0 {
		sections = append(sections, fmt.Sprintf("📬 **대기 중:** %d개", pendingCount))
	}

	// Server-level info from StatusDepsFunc.
	statusFn := h.StatusDeps()
	if statusFn != nil {
		sd := statusFn(sessionKey)
		if sd.Version != "" {
			uptime := ""
			if !sd.StartedAt.IsZero() {
				uptime = fmt.Sprintf(" | Uptime: %s", formatUptime(time.Since(sd.StartedAt)))
			}
			sections = append(sections, fmt.Sprintf("🖥️ **Gateway** v%s%s", sd.Version, uptime))
		}
		sections = append(sections, fmt.Sprintf("🔧 Sessions: %d", sd.SessionCount))
		if sd.LastFailureReason != "" {
			sections = append(sections, fmt.Sprintf("⚠️ **마지막 오류:** %s", sd.LastFailureReason))
		}
	}

	// Session failure reason (from session itself).
	if sess.FailureReason != "" && statusFn == nil {
		sections = append(sections, fmt.Sprintf("⚠️ **마지막 오류:** %s", sess.FailureReason))
	}

	return strings.Join(sections, "\n")
}

// formatCompactTokens formats token counts in compact form (e.g. "1.2M", "890K", "500").
func formatCompactTokens(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

// buildUsageBar returns a simple text progress bar for percentage values.
// Example: "████░░░░░░" for 40%.
func buildUsageBar(pct float64) string {
	const totalBlocks = 10
	filled := int(pct / 100 * totalBlocks)
	if filled > totalBlocks {
		filled = totalBlocks
	}
	bar := ""
	for range filled {
		bar += "█"
	}
	for i := filled; i < totalBlocks; i++ {
		bar += "░"
	}
	return bar
}

// formatUptime formats a duration as compact uptime (e.g. "2d 5h 32m").
func formatUptime(d time.Duration) string {
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
