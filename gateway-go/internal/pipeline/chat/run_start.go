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
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
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
	// WithCancelCause lets callers attach a sentinel (e.g.
	// ErrMergedIntoNewRun) so the run goroutine can choose targeted
	// cleanup based on why it was cancelled.
	runCtx, runCancel := context.WithCancelCause(context.Background())

	// Attach a per-run checkpoint manager so file-editing tools can
	// snapshot before mutating. Skipped entirely when SetCheckpointRoot
	// was never called (checkpointRoot == "") — the tools will then see a
	// nil Checkpointer and fall through to a direct write. Scoped to
	// SessionKey; the Manager's sequence counter is seeded from the
	// existing on-disk index so concurrent runs on the same session do
	// not clobber one another.
	if root := h.checkpointRoot; root != "" {
		cpm := checkpoint.New(root, params.SessionKey)
		runCtx = toolctx.WithCheckpointer(runCtx, checkpoint.NewToolAdapter(cpm))
	}

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

	rsm := h.RunStateMachine()
	go func() {
		if rsm != nil {
			rsm.StartRun()
			defer rsm.EndRun()
		}
		defer runCancel(nil)
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
		embeddingClient:      h.embeddingClient,
		wikiStore:            h.wikiStore,
		dreamTurnFn:          h.dreamTurnFn,
		agentLog:             h.agentLog,
		registry:             h.registry,
		contextCfg:           h.contextCfg,
		subagentDefaultModel: h.subagentDefaultModel,
		defaultSystem:        h.defaultSystem,
		maxTokens:            h.maxTokens,
		drainPendingFn:       h.pending.Drain,
		startRunFn: func(params RunParams) {
			h.startAsyncRun("pending-"+params.ClientRunID, params, false)
		},
		steerQueue: h.steer,

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
