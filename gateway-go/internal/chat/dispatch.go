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
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/chatport"
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
		draftEditFn:          h.draftEditFn,
		draftDeleteFn:        h.draftDeleteFn,
		channelUploadLimitFn: h.ChannelUploadLimit,
		providerConfigs:      h.providerConfigs,
		logger:               h.logger,
		agentTraces:          h.agentTraces,
		wikiStore:            h.wikiStore,
		dreamTurnFn:          h.dreamTurnFn,
		agentLog:             h.agentLog,
		registry:             h.registry,
		emitAgentFn:          h.emitAgentFn,
		emitTranscriptFn:     h.emitTranscriptFn,
		contextCfg:           h.contextCfg,
		defaultModel:         h.defaultModel,
		subagentDefaultModel: h.subagentDefaultModel,
		defaultSystem:        h.defaultSystem,
		maxTokens:            h.maxTokens,
		shutdownCtx:          h.shutdownCtx,
		internalHookRegistry: h.internalHookRegistry,
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
