package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/autoresearch"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// startAsyncRun is the shared logic for Send/SessionsSend/SessionsSteer.
// It validates the session, creates abort context, and spawns the agent goroutine.
func (h *Handler) startAsyncRun(reqID string, params RunParams, isSteer bool) *protocol.ResponseFrame {
	// Ensure session exists.
	sess := h.sessions.Get(params.SessionKey)
	if sess == nil {
		sess = h.sessions.Create(params.SessionKey, session.KindDirect)
	}

	// Inherit model from session state when RunParams doesn't specify one.
	// Skip for sub-agents — their default model is resolved separately in
	// executeAgentRun (subagentDefaultModel takes priority over session.Model).
	if params.Model == "" && sess.Model != "" && sess.SpawnedBy == "" {
		params.Model = sess.Model
	}

	// Transition session to running.
	h.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
		Phase: session.PhaseStart,
		Ts:    time.Now().UnixMilli(),
	})

	// Create a background context (not tied to the RPC request lifetime).
	runCtx, runCancel := context.WithCancel(context.Background())

	if params.ClientRunID != "" {
		h.abortMu.Lock()
		h.abortMap[params.ClientRunID] = &AbortEntry{
			SessionKey: params.SessionKey,
			ClientRun:  params.ClientRunID,
			CancelFn:   runCancel,
			ExpiresAt:  time.Now().Add(4 * time.Hour),
		}
		h.abortMu.Unlock()
	}

	// Broadcast session start event.
	if h.broadcast != nil {
		reason := "message_sent"
		if isSteer {
			reason = "steered"
		}
		h.broadcast("sessions.changed", map[string]any{
			"sessionKey": params.SessionKey,
			"reason":     reason,
			"status":     "running",
		})
	}

	// Spawn async agent run with panic recovery.
	deps := h.buildRunDeps()

	// Wire subagent notification channel so the running agent receives
	// child completion notifications via DeferredSystemText.
	deps.subagentNotifyCh = h.subagentNotifyCh(params.SessionKey)

	// Continuation (continue_run tool + autonomous multi-run) is active in
	// Normal and Work modes. Chat mode (conversation-only) runs once and stops.
	if sess.Mode == session.ModeChat && !params.DeepWork {
		deps.continuationEnabled = false
		deps.maxContinuations = 0
	}
	h.callbackMu.RLock()
	rsm := h.runStateMachine
	h.callbackMu.RUnlock()
	go func() {
		if rsm != nil {
			rsm.StartRun()
			defer rsm.EndRun()
		}
		defer runCancel()
		defer h.cleanupAbort(params.ClientRunID)
		defer func() {
			if r := recover(); r != nil {
				panicArgs := []any{"panic", r, "runId", params.ClientRunID}
				if !isMainSession(params.SessionKey) {
					panicArgs = append(panicArgs, "session", abbreviateSession(params.SessionKey))
				}
				h.logger.Error("panic in agent run", panicArgs...)
				// Ensure session transitions out of running state.
				h.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
					Phase: session.PhaseError,
					Ts:    time.Now().UnixMilli(),
				})
				if h.broadcast != nil {
					h.broadcast("sessions.changed", map[string]any{
						"sessionKey": params.SessionKey,
						"reason":     "panic",
						"status":     "failed",
					})
				}
			}
		}()
		runAgentAsync(runCtx, params, deps)
	}()

	// Immediately return with runId.
	resp, _ := protocol.NewResponseOK(reqID, map[string]any{
		"runId":  params.ClientRunID,
		"status": "started",
	})
	return resp
}

// hasActiveRunForSession reports whether at least one run is active for the session.
func (h *Handler) hasActiveRunForSession(sessionKey string) bool {
	h.abortMu.Lock()
	defer h.abortMu.Unlock()
	for _, entry := range h.abortMap {
		if entry.SessionKey == sessionKey {
			return true
		}
	}
	return false
}

// enqueuePending queues a message for processing after the active run completes.
func (h *Handler) enqueuePending(sessionKey string, params RunParams) {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	q, ok := h.pendingMsgs[sessionKey]
	if !ok {
		q = &pendingRunQueue{}
		h.pendingMsgs[sessionKey] = q
	}
	q.enqueue(params)
}

// drainPending removes and returns the next pending message for a session.
func (h *Handler) drainPending(sessionKey string) *RunParams {
	h.pendingMu.Lock()
	q, ok := h.pendingMsgs[sessionKey]
	h.pendingMu.Unlock()
	if !ok {
		return nil
	}
	return q.drain()
}

// clearPending removes all pending messages for a session (used on /reset).
func (h *Handler) clearPending(sessionKey string) {
	h.pendingMu.Lock()
	delete(h.pendingMsgs, sessionKey)
	h.pendingMu.Unlock()
}

// InterruptActiveRun cancels all active runs for a session key.
func (h *Handler) InterruptActiveRun(sessionKey string) {
	h.abortMu.Lock()
	var toDelete []string
	for id, entry := range h.abortMap {
		if entry.SessionKey == sessionKey {
			entry.CancelFn()
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(h.abortMap, id)
	}
	h.abortMu.Unlock()
}

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
		h.clearPending(sessionKey)
		prompt.ClearSessionSnapshot(sessionKey)
		if h.sessionMemory != nil {
			h.sessionMemory.Delete(sessionKey)
		}
		if h.transcript != nil {
			if err := h.transcript.Delete(sessionKey); err != nil {
				h.logger.Warn("failed to delete transcript on reset", "error", err)
			}
		}
		// Clear tool preset so session exits coordinator/worker mode.
		if sess := h.sessions.Get(sessionKey); sess != nil && sess.ToolPreset != "" {
			sess.ToolPreset = ""
			_ = h.sessions.Set(sess)
		}
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		h.deliverSlashResponse(delivery, "세션이 초기화되었습니다.")

	case "kill":
		h.InterruptActiveRun(sessionKey)
		h.clearPending(sessionKey)
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
			h.callbackMu.Lock()
			h.defaultModel = modelArg
			h.callbackMu.Unlock()
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
			_ = h.sessions.Set(sess)
			h.deliverSlashResponse(delivery, "💬 대화 모드 — 도구 없이 대화만 합니다.")
		case "작업", "work":
			sess.Mode = session.ModeWork
			sess.ToolPreset = ""
			_ = h.sessions.Set(sess)
			h.deliverSlashResponse(delivery, "🔨 작업 모드 — 모든 도구 + 자율 계속 실행이 활성화됩니다.")
		case "일반", "normal":
			sess.Mode = session.ModeNormal
			sess.ToolPreset = ""
			_ = h.sessions.Set(sess)
			h.deliverSlashResponse(delivery, "🔧 일반 모드 — 모든 도구를 사용하지만 자율 계속 실행은 비활성화됩니다.")
		case "":
			// Cycle: normal → chat → work → normal
			switch sess.Mode {
			case session.ModeNormal:
				sess.Mode = session.ModeChat
				sess.ToolPreset = "conversation"
				_ = h.sessions.Set(sess)
				h.deliverSlashResponse(delivery, "💬 대화 모드 — 도구 없이 대화만 합니다.")
			case session.ModeChat:
				sess.Mode = session.ModeWork
				sess.ToolPreset = ""
				_ = h.sessions.Set(sess)
				h.deliverSlashResponse(delivery, "🔨 작업 모드 — 모든 도구 + 자율 계속 실행이 활성화됩니다.")
			default:
				sess.Mode = session.ModeNormal
				sess.ToolPreset = ""
				_ = h.sessions.Set(sess)
				h.deliverSlashResponse(delivery, "🔧 일반 모드 — 모든 도구를 사용하지만 자율 계속 실행은 비활성화됩니다.")
			}
		default:
			h.deliverSlashResponse(delivery, "사용법: /mode [일반|대화|작업] — 인자 없이 순환 전환")
		}

	case "coordinator":
		// Activate coordinator mode: set ToolPreset on the session, reset transcript.
		h.InterruptActiveRun(sessionKey)
		h.clearPending(sessionKey)
		if h.transcript != nil {
			_ = h.transcript.Delete(sessionKey)
		}
		sess := h.sessions.Get(sessionKey)
		if sess != nil {
			sess.ToolPreset = "coordinator"
			_ = h.sessions.Set(sess)
		}
		h.deliverSlashResponse(delivery, "코디네이터 모드가 활성화되었습니다. 워커 에이전트를 조율하여 작업을 수행합니다.")

	case "mail":
		go h.handleMailCommand(reqID, sessionKey, delivery)

	case "chart":
		// Prefer the most recently used autoresearch workdir (from Runner)
		// so /chart works regardless of the global workspace config.
		var workdir string
		h.callbackMu.RLock()
		if h.autoresearchWorkdirFn != nil {
			workdir = h.autoresearchWorkdirFn()
		}
		h.callbackMu.RUnlock()
		if workdir == "" {
			workdir = resolveWorkspaceDirForPrompt()
		}
		cfg, err := autoresearch.LoadConfig(workdir)
		if err != nil {
			h.deliverSlashResponse(delivery, "실험이 없습니다. autoresearch를 먼저 실행하세요.")
			break
		}
		rows, err := autoresearch.ParseResults(workdir)
		if err != nil || len(rows) == 0 {
			h.deliverSlashResponse(delivery, "실험 결과가 없습니다.")
			break
		}
		chartPath, err := autoresearch.SaveChart(workdir, rows, cfg)
		if err != nil {
			h.deliverSlashResponse(delivery, fmt.Sprintf("차트 생성 실패: %s", err.Error()))
			break
		}
		h.callbackMu.RLock()
		sendFn := h.mediaSendFn
		h.callbackMu.RUnlock()
		if sendFn != nil && delivery != nil {
			sendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			caption := fmt.Sprintf("📊 %s (%s)", cfg.MetricName, cfg.MetricDirection)
			if sendErr := sendFn(sendCtx, delivery, chartPath, "photo", caption, false); sendErr != nil {
				h.deliverSlashResponse(delivery, fmt.Sprintf("차트 전송 실패: %s", sendErr.Error()))
			}
			cancel()
		}
	}

	return protocol.MustResponseOK(reqID, map[string]any{
		"command": cmd.Command,
		"handled": true,
	})
}

// deliverSlashResponse sends a slash command response back to the originating channel.
func (h *Handler) deliverSlashResponse(delivery *DeliveryContext, text string) {
	h.callbackMu.RLock()
	fn := h.replyFunc
	h.callbackMu.RUnlock()
	if fn == nil || delivery == nil || text == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := fn(ctx, delivery, text); err != nil {
		h.logger.Warn("slash command reply failed", "error", err)
	}
}

// handleMailCommand fetches the Gmail inbox and either responds directly (no mail)
// or starts an LLM run with the inbox data for analysis.
func (h *Handler) handleMailCommand(reqID, sessionKey string, delivery *DeliveryContext) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := gmail.GetClient()
	if err != nil {
		h.deliverSlashResponse(delivery, "📬 Gmail 인증 정보를 찾을 수 없습니다.")
		return
	}

	unread, err := client.Search(ctx, "is:unread", 10)
	if err != nil {
		h.deliverSlashResponse(delivery, fmt.Sprintf("📬 메일 조회 실패: %s", err))
		return
	}

	var prompt string
	if len(unread) > 0 {
		inbox := gmail.FormatSearchResults(unread)
		prompt = fmt.Sprintf("안 읽은 메일 %d건을 확인했어. 각 메일을 분석해서 요약해줘.\n\n%s", len(unread), inbox)
	} else {
		// No unread — fetch recent mail instead.
		recent, err := client.Search(ctx, "newer_than:3d", 10)
		if err != nil || len(recent) == 0 {
			h.deliverSlashResponse(delivery, "📬 새로운 메일이 없습니다.")
			return
		}
		inbox := gmail.FormatSearchResults(recent)
		prompt = fmt.Sprintf("안 읽은 메일은 없어. 최근 메일 %d건을 확인했어. 요약해줘.\n\n%s", len(recent), inbox)
	}
	h.startAsyncRun(reqID, RunParams{
		SessionKey: sessionKey,
		Message:    prompt,
		Delivery:   delivery,
	}, false)
}

// buildSessionStatus constructs a human-readable session status string.
func (h *Handler) buildSessionStatus(sessionKey string) string {
	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		return fmt.Sprintf("세션 %q: 정보 없음", sessionKey)
	}
	h.callbackMu.RLock()
	model := h.defaultModel
	h.callbackMu.RUnlock()
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

	// Token usage from session (live budget).
	liveBudget := h.contextCfg.LiveTokenBudget
	if sess.TotalTokens != nil && *sess.TotalTokens > 0 {
		in, out := int64(0), int64(0)
		if sess.InputTokens != nil {
			in = *sess.InputTokens
		}
		if sess.OutputTokens != nil {
			out = *sess.OutputTokens
		}
		livePct := float64(*sess.TotalTokens) / float64(liveBudget) * 100
		if livePct > 100 {
			livePct = 100
		}
		sections = append(sections, fmt.Sprintf("📊 **라이브:** %s / %s (%s %.0f%%) in: %s, out: %s",
			formatCompactTokens(*sess.TotalTokens), formatCompactTokens(int64(liveBudget)),
			buildUsageBar(livePct), livePct,
			formatCompactTokens(in), formatCompactTokens(out)))
	} else {
		sections = append(sections, fmt.Sprintf("📊 **라이브:** 0 / %s", formatCompactTokens(int64(liveBudget))))
	}

	// Aurora stored context usage + compaction status.
	if h.auroraStore != nil {
		memBudget := h.contextCfg.MemoryTokenBudget
		if storedTokens, err := h.auroraStore.FetchTokenCount(1); err == nil && storedTokens > 0 {
			memPct := float64(storedTokens) / float64(memBudget) * 100
			if memPct > 100 {
				memPct = 100
			}
			sections = append(sections, fmt.Sprintf("🧠 **Aurora:** %s / %s (%s %.0f%%)",
				formatCompactTokens(int64(storedTokens)), formatCompactTokens(int64(memBudget)),
				buildUsageBar(memPct), memPct))

			// Summary stats (compaction depth indicator).
			if stats, err := h.auroraStore.FetchSummaryStats(1); err == nil && (stats.LeafCount > 0 || stats.CondensedCount > 0) {
				sections = append(sections, fmt.Sprintf("📦 **컴팩션:** 요약 %d개 (leaf: %d, condensed: %d, depth: %d)",
					stats.LeafCount+stats.CondensedCount, stats.LeafCount, stats.CondensedCount, stats.MaxDepth))
			}
		} else {
			sections = append(sections, fmt.Sprintf("🧠 **Aurora:** 0 / %s", formatCompactTokens(int64(memBudget))))
		}

		// Compaction circuit breaker + last run.
		cb := getCompactionCircuitBreaker()
		if cb.IsTripped() {
			sections = append(sections, fmt.Sprintf("🔴 **컴팩션 차단:** 연속 %d회 실패 (circuit breaker tripped)", cb.ConsecutiveFailures()))
		} else if lastMs := proactiveCompaction.lastRun.Load(); lastMs > 0 {
			ago := time.Since(time.UnixMilli(lastMs))
			sections = append(sections, fmt.Sprintf("🟢 **마지막 컴팩션:** %s 전", formatUptime(ago)))
		}
	}

	// Channel.
	if sess.Channel != "" {
		sections = append(sections, fmt.Sprintf("📡 **채널:** %s", sess.Channel))
	}

	// Active runs.
	activeRuns := h.countActiveRuns(sessionKey)
	if activeRuns > 0 {
		sections = append(sections, fmt.Sprintf("🏃 **실행 중:** %d개", activeRuns))
	}

	// Pending messages.
	h.pendingMu.Lock()
	pendingCount := 0
	if q, ok := h.pendingMsgs[sessionKey]; ok {
		pendingCount = q.len()
	}
	h.pendingMu.Unlock()
	if pendingCount > 0 {
		sections = append(sections, fmt.Sprintf("📬 **대기 중:** %d개", pendingCount))
	}

	// Server-level info from StatusDepsFunc.
	h.callbackMu.RLock()
	statusFn := h.statusDepsFunc
	h.callbackMu.RUnlock()
	if statusFn != nil {
		sd := statusFn(sessionKey)
		if sd.Version != "" {
			uptime := ""
			if !sd.StartedAt.IsZero() {
				uptime = fmt.Sprintf(" | Uptime: %s", formatUptime(time.Since(sd.StartedAt)))
			}
			sections = append(sections, fmt.Sprintf("🖥️ **Gateway** v%s%s", sd.Version, uptime))
		}
		rustIcon := "❌"
		if sd.RustFFI {
			rustIcon = "✅"
		}
		sections = append(sections, fmt.Sprintf("🔧 Rust Core: %s | Sessions: %d | WS: %d",
			rustIcon, sd.SessionCount, sd.WSConnections))
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

// countActiveRuns returns the number of active runs for a session.
func (h *Handler) countActiveRuns(sessionKey string) int {
	h.abortMu.Lock()
	defer h.abortMu.Unlock()
	count := 0
	for _, entry := range h.abortMap {
		if entry.SessionKey == sessionKey {
			count++
		}
	}
	return count
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
	for i := 0; i < filled; i++ {
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

// buildRunDeps assembles the dependency struct for runAgentAsync.
// Snapshots all callback fields under callbackMu so the run goroutine
// holds stable references even if Set*() is called concurrently.
func (h *Handler) buildRunDeps() runDeps {
	h.callbackMu.RLock()
	deps := runDeps{
		sessions:             h.sessions,
		llmClient:            h.llmClient,
		transcript:           h.transcript,
		tools:                h.tools,
		authManager:          h.authManager,
		providerRuntime:      h.providerRuntime,
		broadcast:            h.broadcast,
		broadcastRaw:         h.broadcastRaw,
		jobTracker:           h.jobTracker,
		replyFunc:            h.replyFunc,
		mediaSendFn:          h.mediaSendFn,
		typingFn:             h.typingFn,
		reactionFn:           h.reactionFn,
		toolProgressFn:       h.toolProgressFn,
		draftEditFn:          h.draftEditFn,
		draftDeleteFn:        h.draftDeleteFn,
		channelUploadLimitFn: h.ChannelUploadLimit,
		providerConfigs:      h.providerConfigs,
		logger:               h.logger,
		auroraStore:          h.auroraStore,
		vegaBackend:          h.vegaBackend,
		memoryStore:          h.memoryStore,
		sessionMemory:        h.sessionMemory,
		memoryEmbedder:       h.memoryEmbedder,
		unifiedStore:         h.unifiedStore,
		dreamTurnFn:          h.dreamTurnFn,
		agentLog:             h.agentLog,
		registry:             h.registry,
		emitAgentFn:          h.emitAgentFn,
		emitTranscriptFn:     h.emitTranscriptFn,
		contextCfg:           h.contextCfg,
		compactionCfg:        h.compactionCfg,
		defaultModel:         h.defaultModel,
		subagentDefaultModel: h.subagentDefaultModel,
		defaultSystem:        h.defaultSystem,
		maxTokens:            h.maxTokens,
		shutdownCtx:          h.shutdownCtx,
		hookRegistry:         h.hookRegistry,
		internalHookRegistry: h.internalHookRegistry,
		pluginHookRunner:     h.pluginHookRunner,
		drainPendingFn:       h.drainPending,
		startRunFn: func(params RunParams) {
			// Re-use startAsyncRun for full lifecycle management (abort map,
			// panic recovery, runStateMachine, session state transitions).
			h.startAsyncRun("pending-"+params.ClientRunID, params, false)
		},
		maxContinuations:    5,
		continuationEnabled: true,

		// chatport boundary: wire concrete autoreply implementations.
		newTypingSignaler: func(onStart func()) chatport.TypingSignaler {
			ctrl := typing.NewTypingController(typing.TypingControllerConfig{
				OnStart:    onStart,
				IntervalMs: 5000, // Telegram typing expires after 5s
			})
			return typing.NewFullTypingSignaler(ctrl, typing.TypingModeInstant, false)
		},
		sanitizeDraft:        reply.SanitizeDraftText,
		parseReplyDirectives: reply.ParseReplyDirectives,
		isTransientError:     autoreply.IsTransientHTTPError,
	}
	h.callbackMu.RUnlock()
	return deps
}

// cleanupAbort removes a run's abort entry after the run completes.
func (h *Handler) cleanupAbort(clientRunID string) {
	if clientRunID == "" {
		return
	}
	h.abortMu.Lock()
	delete(h.abortMap, clientRunID)
	h.abortMu.Unlock()
}

// abortGCLoop periodically cleans up expired abort entries.
// Exits when h.done is closed (via Close()).
func (h *Handler) abortGCLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.abortMu.Lock()
			now := time.Now()
			for id, entry := range h.abortMap {
				if now.After(entry.ExpiresAt) {
					entry.CancelFn()
					delete(h.abortMap, id)
				}
			}
			h.abortMu.Unlock()
		}
	}
}

// budgetHistory truncates a history payload to fit within maxHistoryBytes.
func (h *Handler) budgetHistory(reqID string, payload json.RawMessage) *protocol.ResponseFrame {
	var parsed struct {
		Messages []json.RawMessage `json:"messages"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		resp := protocol.MustResponseOK(reqID, map[string]any{
			"messages":  []any{},
			"total":     0,
			"truncated": true,
			"error":     "failed to parse history for budgeting",
		})
		return resp
	}

	// Keep messages from the end (most recent) until budget exhausted.
	// Collect in reverse, then flip to preserve chronological order.
	reversed := make([]json.RawMessage, 0, len(parsed.Messages))
	totalBytes := 0
	truncatedCount := 0
	for i := len(parsed.Messages) - 1; i >= 0; i-- {
		msgBytes := len(parsed.Messages[i])
		if msgBytes > h.maxMessageBytes {
			placeholder, _ := json.Marshal(map[string]any{
				"role":      "system",
				"content":   fmt.Sprintf("[message truncated: %d bytes]", msgBytes),
				"truncated": true,
			})
			msgBytes = len(placeholder)
			parsed.Messages[i] = placeholder
			truncatedCount++
		}
		if totalBytes+msgBytes > h.maxHistoryBytes {
			break
		}
		reversed = append(reversed, parsed.Messages[i])
		totalBytes += msgBytes
	}
	// Reverse to restore chronological order.
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	resp := protocol.MustResponseOK(reqID, map[string]any{
		"messages":       reversed,
		"total":          parsed.Total,
		"truncatedCount": truncatedCount,
		"budgeted":       true,
	})
	return resp
}

// sanitizeInput normalizes input text: NFC normalization approximation,
// strips control chars (except tab/newline/CR), and trims whitespace.
func sanitizeInput(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i += size
			continue
		}
		// Allow tab, newline, carriage return.
		if r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(r)
			i += size
			continue
		}
		// Strip other control characters.
		if unicode.IsControl(r) {
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return strings.TrimSpace(b.String())
}
