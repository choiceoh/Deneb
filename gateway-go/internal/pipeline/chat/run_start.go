package chat

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/reply"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/typing"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
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

	h.abort.Register(params.ClientRunID, &AbortEntry{
		SessionKey: params.SessionKey,
		ClientRun:  params.ClientRunID,
		CancelFn:   runCancel,
		ExpiresAt:  time.Now().Add(4 * time.Hour),
	})

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
	deps.subagentNotifyCh = h.subagent.NotifyCh(params.SessionKey)

	// Continuation (continue_run tool + autonomous multi-run) is active in
	// Normal and Work modes. Chat mode (conversation-only) runs once and stops.
	if sess.Mode == session.ModeChat && !params.DeepWork {
		deps.continuationEnabled = false
		deps.maxContinuations = 0
	}
	rsm := h.RunStateMachine()
	go func() {
		if rsm != nil {
			rsm.StartRun()
			defer rsm.EndRun()
		}
		defer runCancel()
		defer h.abort.Cleanup(params.ClientRunID)
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

// InterruptActiveRun cancels all active runs for a session key.
func (h *Handler) InterruptActiveRun(sessionKey string) {
	h.abort.InterruptSession(sessionKey)
}

// buildRunDeps assembles the dependency struct for runAgentAsync.
// Snapshots all callback fields atomically so the run goroutine
// holds stable references even if Set*() is called concurrently.
func (h *Handler) buildRunDeps() runDeps {
	return runDeps{
		sessions:             h.sessions,
		llmClient:            h.llmClient,
		transcript:           h.transcript,
		tools:                h.tools,
		authManager:          h.authManager,
		providerRuntime:      h.providerRuntime,
		broadcast:            h.broadcast,
		jobTracker:           h.jobTracker,
		channelUploadLimitFn: h.ChannelUploadLimit,
		providerConfigs:      h.providerConfigs,
		logger:               h.logger,
		wikiStore:            h.wikiStore,
		dreamTurnFn:          h.dreamTurnFn,
		agentLog:             h.agentLog,
		registry:             h.registry,
		contextCfg:           h.contextCfg,
		subagentDefaultModel: h.subagentDefaultModel,
		defaultSystem:        h.defaultSystem,
		maxTokens:            h.maxTokens,
		internalHookRegistry: h.internalHookRegistry,
		drainPendingFn:       h.pending.Drain,
		startRunFn: func(params RunParams) {
			h.startAsyncRun("pending-"+params.ClientRunID, params, false)
		},
		maxContinuations:    5,
		continuationEnabled: true,

		// Atomic snapshot of channel callbacks (reply, media, typing, etc.).
		callbacks: h.Snapshot(),

		// chatport boundary: wire concrete autoreply implementations.
		chatport: chatportAdapters{
			NewTypingSignaler: func(onStart func()) chatport.TypingSignaler {
				ctrl := typing.NewTypingController(typing.TypingControllerConfig{
					OnStart:    onStart,
					IntervalMs: 5000, // Telegram typing expires after 5s
				})
				return typing.NewFullTypingSignaler(ctrl, typing.TypingModeInstant, false)
			},
			SanitizeDraft:        reply.SanitizeDraftText,
			ParseReplyDirectives: reply.ParseReplyDirectives,
			IsTransientError:     autoreply.IsTransientHTTPError,
		},
	}
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
