package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// handleSlashCommand processes a recognized slash command and returns a response.
// This runs synchronously (no agent loop). Immediate replies go through respond;
// a nil respond falls back to channel delivery (deliverSlashResponse), which the
// async chat.send path uses. The sync native path (SendSync) passes a collector
// so the reply text returns in the RPC response instead. Long-running commands
// (/update, /restart, /rollback) still deliver their late output via the
// channel reply path from their own goroutines.
func (h *Handler) handleSlashCommand(
	reqID string,
	sessionKey string,
	delivery *DeliveryContext,
	cmd *SlashResult,
	respond func(text string),
) *protocol.ResponseFrame {
	if respond == nil {
		respond = func(text string) { h.deliverSlashResponse(delivery, text) }
	}
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
		clearRecallMemory(sessionKey)
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
		respond("세션이 초기화되었습니다.")

	case "kill":
		h.InterruptActiveRun(sessionKey)
		h.pending.Clear(sessionKey)
		h.mergeWindow.Clear(sessionKey)
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		respond("실행이 중단되었습니다.")

	case "help":
		respond(slashHelpText())

	case "status":
		status := h.buildSessionStatus(sessionKey)
		respond(status)

	case "rollback":
		// /rollback [list|목록] [N] | [diff|비교] <id> | [restore|복원] <id>
		// Delegated to rollback_dispatch.go for parsing + rendering.
		rollbackLogger := h.logger
		go func() {
			defer func() {
				if r := recover(); r != nil && rollbackLogger != nil {
					rollbackLogger.Error("panic in /rollback command handler", "panic", r)
				}
			}()
			h.handleRollbackCommand(sessionKey, delivery, cmd.Args)
		}()

	case "update":
		// /update — preview pending commits; /update 확인 — pull + build +
		// restart. Delegated to update_dispatch.go. Runs in a goroutine
		// because the build step can take a couple of minutes.
		updateLogger := h.logger
		go func() {
			defer func() {
				if r := recover(); r != nil && updateLogger != nil {
					updateLogger.Error("panic in /update command handler", "panic", r)
				}
			}()
			h.handleUpdateCommand(reqID, sessionKey, delivery, cmd.Args)
		}()

	case "restart":
		// /restart — guidance; /restart 확인 — restart the gateway.
		// Delegated to restart_dispatch.go. Runs in a goroutine so the
		// reply is delivered before graceful shutdown begins.
		restartLogger := h.logger
		go func() {
			defer func() {
				if r := recover(); r != nil && restartLogger != nil {
					restartLogger.Error("panic in /restart command handler", "panic", r)
				}
			}()
			h.handleRestartCommand(delivery, cmd.Args)
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
		// The user issued a slash command and got no response back — surface it
		// as Error, not a Warn that hides the dropped reply.
		h.logger.Error("slash command reply failed", "error", err)
	}
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

	// Process-wide prompt-cache hit ratio — the cache-doctrine regression
	// alarm (.claude/rules/prompt-cache.md), counted only for Anthropic-mode
	// runs (non-Anthropic providers can't report cache usage). Shows a recent
	// EWMA (surfaces a fresh regression) alongside the cumulative-since-start
	// total. Only rendered once some prompt tokens are recorded.
	if cr, cc, fi := metrics.CacheHits.Snapshot(); cr+cc+fi > 0 {
		// Compute the cumulative ratio from this same snapshot (not a second
		// atomic load) so the shown percentage and counts stay consistent.
		line := fmt.Sprintf("💾 **캐시 히트율:** 누적 %.0f%%", metrics.HitRatioOf(cr, cc, fi)*100)
		if recent, ok := metrics.CacheHits.RecentRatio(); ok {
			line += fmt.Sprintf(" · 최근 %.0f%%", recent*100)
		}
		line += fmt.Sprintf(" (read %s · write %s · fresh %s)",
			formatCompactTokens(cr), formatCompactTokens(cc), formatCompactTokens(fi))
		sections = append(sections, line)
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
