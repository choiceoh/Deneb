package chat

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/chatportwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// startAsyncRun is the shared logic for Send/SessionsSend/SessionsSteer.
// It validates the session, creates abort context, and spawns the agent goroutine.
func (h *Handler) startAsyncRun(reqID string, params RunParams, isSteer bool) *protocol.ResponseFrame {
	return h.startAsyncRunWithAdmission(reqID, params, isSteer, runAdmissionNew)
}

func (h *Handler) startAdmittedAsyncRun(reqID string, params RunParams, isSteer bool) *protocol.ResponseFrame {
	return h.startAsyncRunWithAdmission(reqID, params, isSteer, runAdmissionReserved)
}

// startQueuedAsyncRun continues work that entered the pending queue before a
// shutdown drain began. The current run remains registered until this method
// returns, so RegisterContinuation can extend the same uninterrupted drain.
func (h *Handler) startQueuedAsyncRun(reqID string, params RunParams) *protocol.ResponseFrame {
	return h.startAsyncRunWithAdmission(reqID, params, false, runAdmissionContinuation)
}

// startOrQueueRun serializes internally generated work (notably subagent
// completion notifications) with the same per-session handoff used by user
// chat. A stale active/idle observation can therefore neither start parallel
// runs nor strand a notification behind a run that just finished.
func (h *Handler) startOrQueueRun(reqID string, params RunParams, isSteer bool) {
	if h.abort == nil {
		return
	}

	sessLock := h.mergeWindow.SessionLock(params.SessionKey)
	sessLock.Lock()
	if h.abort.HasActiveRun(params.SessionKey) {
		// Queue-only handoff never consumes a top-level admission slot. During
		// shutdown drain the parent run will hand off this continuation via
		// finishRunWithPendingHandoff without reopening admission.
		h.pending.Enqueue(params.SessionKey, params)
		sessLock.Unlock()
		return
	}
	sessLock.Unlock()

	if !h.abort.AcquireAdmission() {
		return
	}
	defer h.abort.ReleaseAdmission()

	sessLock.Lock()
	defer sessLock.Unlock()
	if h.abort.HasActiveRun(params.SessionKey) {
		h.pending.Enqueue(params.SessionKey, params)
		return
	}
	h.startAdmittedAsyncRun(reqID, params, isSteer)
}

type runAdmissionMode uint8

const (
	runAdmissionNew runAdmissionMode = iota
	runAdmissionReserved
	runAdmissionContinuation
)

func (h *Handler) startAsyncRunWithAdmission(reqID string, params RunParams, isSteer bool, admission runAdmissionMode) *protocol.ResponseFrame {
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

	// Create a background context (not tied to the RPC request lifetime).
	// WithCancelCause lets callers attach a sentinel (e.g.
	// ErrMergedIntoNewRun) so the run goroutine can choose targeted
	// cleanup based on why it was cancelled.
	runCtx, runCancel := context.WithCancelCause(context.Background())
	if params.ClientRunID == "" {
		params.ClientRunID = leafbind.NewShortID("async")
	}

	// Attach a per-run checkpoint manager so file-editing tools can
	// snapshot before mutating. Skipped entirely when SetCheckpointRoot
	// was never called (checkpointRoot == "") — the tools will then see a
	// nil Checkpointer and fall through to a direct write. Scoped to
	// SessionKey; the Manager's sequence counter is seeded from the
	// existing on-disk index so concurrent runs on the same session do
	// not clobber one another.
	if root := h.checkpointRoot; root != "" {
		cpm := checkpoint.New(root, params.SessionKey)
		runCtx = toolport.WithCheckpointer(runCtx, checkpoint.NewToolAdapter(cpm))
	}

	entry := &AbortEntry{
		SessionKey: params.SessionKey,
		ClientRun:  params.ClientRunID,
		CancelFn:   runCancel,
		ExpiresAt:  time.Now().Add(4 * time.Hour),
		Automation: isAutomationRun(params),
	}
	var registered bool
	switch admission {
	case runAdmissionContinuation:
		registered = h.abort.RegisterContinuation(params.ClientRunID, entry)
	case runAdmissionReserved:
		registered = h.abort.RegisterAdmitted(params.ClientRunID, entry)
	default:
		registered = h.abort.TryRegister(params.ClientRunID, entry)
	}
	if !registered {
		runCancel(ErrRuntimeDraining)
		return leafbind.RPCNew(protocol.ErrUnavailable, ErrRuntimeDraining.Error()).Response(reqID)
	}

	// Transition session only after admission succeeds. A rejected restart-time
	// request must not leave the session stuck in a synthetic running state.
	h.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
		Phase: session.PhaseStart,
		Ts:    time.Now().UnixMilli(),
	})

	// Broadcast session start event.
	if h.broadcast != nil {
		reason := "message_sent"
		if isSteer {
			reason = "steered"
		}
		broadcastPayload(h.broadcast, "sessions.changed", SessionsChangedEvent{
			SessionKey: params.SessionKey,
			Reason:     reason,
			Status:     "running",
		})
	}

	// Spawn async agent run with panic recovery.
	deps := h.buildRunDeps()

	// Wire subagent notification channel so the running agent receives
	// child completion notifications via DeferredSystemText.
	deps.subagentNotifyCh = h.subagent.NotifyCh(params.SessionKey)

	go func() {
		defer runCancel(nil)
		// Registered before finishRunWithPendingHandoff → deferred-runs-later
		// (LIFO), so this fires after the registry handoff and hasActiveRun reports
		// either the successor or a genuinely idle parent. Any child completion
		// notification parked while the run was active is then re-routed instead
		// of orphaned. Covers success, error, and panic exits.
		defer func() {
			if h.subagent != nil {
				h.subagent.ReclaimOnIdle(params.SessionKey)
			}
		}()
		defer h.finishRunWithPendingHandoff(params.SessionKey, params.ClientRunID)
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
					broadcastPayload(h.broadcast, "sessions.changed", SessionsChangedEvent{
						SessionKey: params.SessionKey,
						Reason:     "panic",
						Status:     "failed",
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

// finishRunWithPendingHandoff closes the last enqueue/cleanup race for a
// session. chat.send and SendDirect take the same session lock before deciding
// whether to enqueue. The finishing run therefore either sees and registers
// the queued continuation before Cleanup, or becomes idle before a producer
// decides to start a fresh run. BeginDrain can never observe a false idle gap.
func (h *Handler) finishRunWithPendingHandoff(sessionKey, clientRunID string) {
	sessLock := h.mergeWindow.SessionLock(sessionKey)
	sessLock.Lock()
	defer sessLock.Unlock()

	// executeAgentRun normally hands off the pending message before returning.
	// Check once more under the decision lock to cover a message that arrived
	// between its final drain and this goroutine's cleanup. If a successor is
	// already registered, leave the queue for that run so continuations remain
	// serialized rather than starting in parallel.
	if !h.abort.HasOtherActiveRun(sessionKey, clientRunID) {
		if pending := h.pending.Drain(sessionKey); pending != nil {
			h.startQueuedAsyncRun("pending-"+pending.ClientRunID, *pending)
		}
	}
	h.abort.Cleanup(clientRunID)
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
		providerConfigs:      h.ProviderConfigs(),
		logger:               h.logger,
		memory:               h.memory,
		dreamTurnFn:          h.dreamTurnFn,
		preferenceSignalFn:   h.preferenceSignalFn,
		projectSignalFn:      h.projectSignalFn,
		deliverablePublisher: h.deliverablePublisher,
		translateThinking:    h.translateThinking,
		agentLog:             h.agentLog,
		registry:             h.registry,
		contextCfg:           h.contextCfg,
		subagentDefaultModel: h.subagentDefaultModel,
		defaultSystem:        h.defaultSystem,
		maxTokens:            h.maxTokens,
		runLimits:            h.runLimits,
		samplingSeed:         h.samplingSeed,
		disableTier1Wiki:     h.disableTier1Wiki,
		semanticNow:          h.semanticNow,
		semanticTimezone:     h.semanticTimezone,
		workspaceDir:         h.workspaceDir,
		promptWorkspaceDir:   h.promptWorkspaceDir,
		briefcaseMode:        h.briefcaseMode,
		auditSystemPrompt:    h.auditSystemPrompt,
		drainPendingFn:       h.pending.Drain,
		startRunFn: func(params RunParams) {
			h.startQueuedAsyncRun("pending-"+params.ClientRunID, params)
		},
		steerQueue: h.steer,
		skills:     h.skills,
		ambient:    h.ambient,

		// Atomic snapshot of channel callbacks (reply, media, typing, etc.).
		callbacks: h.Snapshot(),

		// chatport boundary: wire concrete autoreply implementations.
		chatport: chatportAdapters{
			NewTypingSignaler:    chatportwire.NewTypingSignaler,
			SanitizeDraft:        chatportwire.SanitizeDraft,
			ParseReplyDirectives: chatportwire.ParseReplyDirectives,
		},
		normalizeCardReply: h.normalizeCardReply,
		reportCardHealth:   h.reportCardHealth,
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

// SanitizeInput exposes the exact chat wire normalization to trusted harnesses
// that must bind provenance to the bytes the model actually receives.
func SanitizeInput(s string) string { return sanitizeInput(s) }
