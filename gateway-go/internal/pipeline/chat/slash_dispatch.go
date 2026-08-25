package chat

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	chatrecall "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
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
		h.handleResetCommand(sessionKey, respond)

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
		h.runSlashAsync("/rollback command handler", func() {
			h.handleRollbackCommand(sessionKey, delivery, cmd.Args)
		})

	case "update":
		// /update — preview pending commits; /update 확인 — pull + build +
		// restart. Delegated to update_dispatch.go. Runs in a goroutine
		// because the build step can take a couple of minutes.
		h.runSlashAsync("/update command handler", func() {
			h.handleUpdateCommand(reqID, sessionKey, delivery, cmd.Args)
		})

	case "restart":
		// /restart — guidance; /restart 확인 — restart the gateway.
		// Delegated to restart_dispatch.go. Runs in a goroutine so the
		// reply is delivered before graceful shutdown begins.
		h.runSlashAsync("/restart command handler", func() {
			h.handleRestartCommand(delivery, cmd.Args)
		})

	case "goal":
		// /goal <text> sets a standing goal (Ralph loop); status|pause|resume|stop
		// manage it. Synchronous (store ops only) — handled in goal_command.go.
		h.handleGoalCommand(sessionKey, cmd.Args, respond)

	case "weekly":
		// /weekly (/주간보고) — deterministic 주간업무보고: post the formal form
		// image (best-effort, async) and reply with the server-assembled deneb-ui
		// card (head line + fence). Mirrors the Saturday cron path
		// (cron_agent_adapter.go) so a manual trigger produces the same output.
		// No agent loop — the card is built straight from wiki data
		// (routine.RenderWeeklyReportCard) so the format never drifts.
		h.handleWeeklyCommand(respond)

	}

	return protocol.MustResponseOK(reqID, map[string]any{
		"command": cmd.Command,
		"handled": true,
	})
}

// handleResetCommand implements /reset: abort any active run, clear the
// transcript, and discard every frozen per-session snapshot.
func (h *Handler) handleResetCommand(sessionKey string, respond func(text string)) {
	h.InterruptActiveRun(sessionKey)
	h.pending.Clear(sessionKey)
	h.mergeWindow.Clear(sessionKey)
	if h.steer != nil {
		h.steer.Clear(sessionKey)
	}
	prompt.ClearSessionSnapshot(sessionKey)
	chatrecall.ClearSession(sessionKey)
	clearSessionBlackboard(sessionKey)
	clearTier1Wiki(sessionKey)
	toolport.ClearActiveNotebook(sessionKey) // unbind any active notebook-grounding session
	clearNotebookGrounding(sessionKey)       // drop the frozen grounding snapshot too
	forgetPromptSnapshot(sessionKey)         // drop the persisted copy too, not just memory
	clearPersistedTails(sessionKey)          // recorded user-message tails go with the transcript
	// Stop any standing goal bound to this session so /reset is a clean slate.
	toolwire.ClearStandingGoal(sessionKey)
	if h.transcript != nil {
		if err := h.transcript.Delete(sessionKey); err != nil {
			h.logger.Error("failed to delete transcript on reset", "error", err)
		}
	}
	// Spill files go with the transcript. The lifecycle hook cannot catch this
	// case: /reset ends by emitting PhaseEnd, which is the same terminal status
	// an ordinary completed run emits, and runs must NOT release spills (their
	// read_spillover pointers stay quoted in surviving history). Reset is the
	// opposite — the history that quoted them was just deleted — so the reclaim
	// is explicit here, alongside the other per-session state this clears.
	if h.tools != nil {
		if ss := h.tools.SpilloverStore(); ss != nil {
			ss.CleanSession(sessionKey)
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
}

// handleWeeklyCommand implements /weekly (see the dispatch comment above).
func (h *Handler) handleWeeklyCommand(respond func(text string)) {
	if h.weeklyReportTextFn == nil {
		respond("주간업무보고 생성이 이 게이트웨이에 배선되지 않았습니다.")
		return
	}
	// Form image → native chat (best-effort; render may be skipped on low
	// memory/disk, in which case the text report below still lands).
	if h.weeklyFormDeliverFn != nil {
		formFn := h.weeklyFormDeliverFn
		formLogger := h.logger
		h.runSlashAsync("/weekly form delivery", func() {
			// Bounded background ctx: the chromium render takes a few seconds
			// and is an independent side delivery from the text reply.
			fctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if err := formFn(fctx); err != nil && formLogger != nil {
				formLogger.Error("weekly form image delivery failed", "error", err)
			}
		})
	}
	// Deterministic text — fast wiki read, returned synchronously so the sync
	// native RPC carries it as the command's reply.
	tctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	txt, terr := h.weeklyReportTextFn(tctx)
	cancel()
	if terr != nil || strings.TrimSpace(txt) == "" {
		if h.logger != nil {
			h.logger.Error("weekly report text generation failed", "error", terr)
		}
		respond("주간업무보고를 생성하지 못했습니다. 위키 프로젝트 페이지에 `소관:` 태그가 있는지 확인해주세요.")
		return
	}
	respond(txt)
}

// runSlashAsync runs a long-lived slash command handler on its own goroutine
// with panic recovery. The handler delivers its own late output via the
// channel reply path; `what` names the command in the panic log line.
func (h *Handler) runSlashAsync(what string, fn func()) {
	logger := h.logger
	go func() {
		defer func() {
			if r := recover(); r != nil && logger != nil {
				logger.Error("panic in "+what, "panic", r)
			}
		}()
		fn()
	}()
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
	sections = append(sections, fmt.Sprintf("📋 **세션:** `%s` %s %s", sessionKey, sessionStatusIcon(sess.Status), string(sess.Status)))

	// Model.
	if model != "" {
		sections = append(sections, fmt.Sprintf("🤖 **모델:** %s", model))
	}

	// Mode settings.
	if modes := sessionModeLine(sess); modes != "" {
		sections = append(sections, modes)
	}

	// Token usage from session.
	sections = append(sections, h.sessionTokenLine(sess))

	// Channel.
	if sess.Channel != "" {
		sections = append(sections, fmt.Sprintf("📡 **채널:** %s", sess.Channel))
	}

	// Active runs.
	if activeRuns := h.abort.CountForSession(sessionKey); activeRuns > 0 {
		sections = append(sections, fmt.Sprintf("🏃 **실행 중:** %d개", activeRuns))
	}

	// Pending messages.
	if pendingCount := h.pending.Len(sessionKey); pendingCount > 0 {
		sections = append(sections, fmt.Sprintf("📬 **대기 중:** %d개", pendingCount))
	}

	sections = h.appendServerStatus(sections, sessionKey, sess)

	if line := promptCacheStatusLine(); line != "" {
		sections = append(sections, line)
	}

	return strings.Join(sections, "\n")
}

// sessionStatusIcon maps a session status to its /status emoji.
func sessionStatusIcon(status session.RunStatus) string {
	switch status {
	case session.StatusRunning:
		return "🔄"
	case session.StatusFailed:
		return "❌"
	case session.StatusKilled:
		return "⛔"
	case session.StatusTimeout:
		return "⏰"
	default:
		// StatusDone, StatusIdle, etc.
		return "🟢"
	}
}

// sessionModeLine renders the ⚙️ 모드 line, or "" when no mode is active.
func sessionModeLine(sess *session.Session) string {
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
	if len(modes) == 0 {
		return ""
	}
	return "⚙️ **모드:** " + strings.Join(modes, " | ")
}

// sessionTokenLine renders the 📊 토큰 usage line against the memory budget.
func (h *Handler) sessionTokenLine(sess *session.Session) string {
	memBudget := h.contextCfg.MemoryTokenBudget
	if memBudget > uint64(math.MaxInt64) {
		memBudget = uint64(math.MaxInt64)
	}
	budget := int64(memBudget) //nolint:gosec // G115 — clamped to MaxInt64 above
	if sess.TotalTokens == nil || *sess.TotalTokens <= 0 {
		return fmt.Sprintf("📊 **토큰:** 0 / %s", formatCompactTokens(budget))
	}
	in, out := int64(0), int64(0)
	if sess.InputTokens != nil {
		in = *sess.InputTokens
	}
	if sess.OutputTokens != nil {
		out = *sess.OutputTokens
	}
	usagePct := float64(*sess.TotalTokens) / float64(budget) * 100
	if usagePct > 100 {
		usagePct = 100
	}
	return fmt.Sprintf("📊 **토큰:** %s / %s (%s %.0f%%) in: %s, out: %s",
		formatCompactTokens(*sess.TotalTokens), formatCompactTokens(budget),
		buildUsageBar(usagePct), usagePct,
		formatCompactTokens(in), formatCompactTokens(out))
}

// appendServerStatus appends the server-level info from StatusDepsFunc, or —
// when no status dependency is wired — the session's own failure reason.
func (h *Handler) appendServerStatus(sections []string, sessionKey string, sess *session.Session) []string {
	statusFn := h.StatusDeps()
	if statusFn == nil {
		// Session failure reason (from session itself).
		if sess.FailureReason != "" {
			sections = append(sections, fmt.Sprintf("⚠️ **마지막 오류:** %s", sess.FailureReason))
		}
		return sections
	}
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
	return sections
}

// promptCacheStatusLine renders the process-wide prompt-cache hit ratio — the
// cache-doctrine regression alarm (docs/agent-rules/prompt-cache.md), counted
// only for Anthropic-mode runs (non-Anthropic providers can't report cache
// usage). Shows a recent EWMA (surfaces a fresh regression) alongside the
// cumulative-since-start total. Empty until some prompt tokens are recorded.
func promptCacheStatusLine() string {
	cr, cc, fi := metrics.CacheHits.Snapshot()
	if cr+cc+fi <= 0 {
		return ""
	}
	// Compute the cumulative ratio from this same snapshot (not a second
	// atomic load) so the shown percentage and counts stay consistent.
	line := fmt.Sprintf("💾 **캐시 히트율:** 누적 %.0f%%", metrics.HitRatioOf(cr, cc, fi)*100)
	if recent, ok := metrics.CacheHits.RecentRatio(); ok {
		line += fmt.Sprintf(" · 최근 %.0f%%", recent*100)
	}
	line += fmt.Sprintf(" (read %s · write %s · fresh %s)",
		formatCompactTokens(cr), formatCompactTokens(cc), formatCompactTokens(fi))
	return line
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
