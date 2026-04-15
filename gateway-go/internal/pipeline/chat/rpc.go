package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Send handles "chat.send" — the primary message ingestion endpoint.
// Sanitizes input, starts an async agent run, and immediately returns.
func (h *Handler) Send(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey   string           `json:"sessionKey"`
		Message      string           `json:"message"`
		Attachments  []ChatAttachment `json:"attachments,omitempty"`
		Delivery     *DeliveryContext `json:"delivery,omitempty"`
		ClientRunID  string           `json:"clientRunId,omitempty"`
		Model        string           `json:"model,omitempty"` // role name: "main","lightweight","fallback"
		WorkspaceDir string           `json:"workspaceDir,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.WrapInvalidRequest("invalid chat.send params", err).Response(req.ID)
	}
	if p.SessionKey == "" {
		return rpcerr.MissingParam("sessionKey").Response(req.ID)
	}
	if p.Message == "" && len(p.Attachments) == 0 {
		return rpcerr.New(protocol.ErrMissingParam, "message or attachments required").Response(req.ID)
	}
	if len(p.Message) > h.maxMessageBytes {
		return rpcerr.Newf(protocol.ErrInvalidRequest, "message too large: %d bytes exceeds limit of %d", len(p.Message), h.maxMessageBytes).Response(req.ID)
	}

	// Pre-process slash commands before dispatching to agent.
	if slashResult := ParseSlashCommand(p.Message); slashResult != nil && slashResult.Handled {
		return h.handleSlashCommand(req.ID, p.SessionKey, p.Delivery, slashResult)
	}

	runParams := RunParams{
		SessionKey:   p.SessionKey,
		Message:      sanitizeInput(p.Message),
		Attachments:  p.Attachments,
		Delivery:     p.Delivery,
		ClientRunID:  p.ClientRunID,
		Model:        p.Model,
		WorkspaceDir: p.WorkspaceDir,
	}

	// Record this message's arrival timestamp; prevTs is the previous
	// arrival time for this session (zero if this is the first message).
	prevTs := h.mergeWindow.Touch(p.SessionKey)

	if h.abort.HasActiveRun(p.SessionKey) {
		// Quick-fire merge: when the user sends a follow-up within
		// mergeWindowDuration of the previous message and the previous run
		// is still in progress, cancel that run and start a new one so both
		// messages are answered together. The previous user message has
		// already been persisted to the transcript by executeAgentRun, so
		// the new run sees both turns and produces a single combined reply.
		if !prevTs.IsZero() && time.Since(prevTs) <= mergeWindowDuration {
			h.logger.Info("merging consecutive message into new run",
				"sessionKey", p.SessionKey,
				"deltaMs", time.Since(prevTs).Milliseconds(),
			)
			h.InterruptActiveRun(p.SessionKey)
			// Fold any older queued message into this one so nothing is lost.
			if pending := h.pending.Drain(p.SessionKey); pending != nil {
				if pending.Message != "" {
					if runParams.Message != "" {
						runParams.Message = pending.Message + "\n\n" + runParams.Message
					} else {
						runParams.Message = pending.Message
					}
				}
				if len(pending.Attachments) > 0 {
					runParams.Attachments = append(pending.Attachments, runParams.Attachments...)
				}
			}
			return h.startAsyncRun(req.ID, runParams, false)
		}

		// Outside the merge window: queue the message instead of interrupting.
		// The active run completes normally (preserving its full context),
		// then the queued message is processed automatically. This prevents
		// the "amnesia" bug where the assistant forgets in-progress work when
		// the user sends a message mid-execution.
		h.pending.Enqueue(p.SessionKey, runParams)
		h.logger.Info("queued message for active run",
			"sessionKey", p.SessionKey)
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"status": "queued",
			"reason": "active-run",
		})
		return resp
	}

	return h.startAsyncRun(req.ID, runParams, false)
}

// SessionsSend handles "sessions.send" — interrupts any active run, then starts a new one.
func (h *Handler) SessionsSend(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		Key            string           `json:"key"`
		Message        string           `json:"message"`
		Thinking       string           `json:"thinking,omitempty"`
		Attachments    []ChatAttachment `json:"attachments,omitempty"`
		TimeoutMs      int              `json:"timeoutMs,omitempty"`
		IdempotencyKey string           `json:"idempotencyKey,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.WrapInvalidRequest("invalid sessions.send params", err).Response(req.ID)
	}
	if p.Key == "" {
		return rpcerr.MissingParam("key").Response(req.ID)
	}

	// Interrupt any active run and clear the pending queue for this session.
	// Without clearPending, queued user messages survive the interrupt and
	// replay after the new run completes — causing the "diary navigation" bug
	// where the user's reply is discarded and a scheduled task takes over.
	h.InterruptActiveRun(p.Key)
	h.pending.Clear(p.Key)

	runID := p.IdempotencyKey
	if runID == "" {
		runID = shortid.New("run")
	}

	return h.startAsyncRun(req.ID, RunParams{
		SessionKey:  p.Key,
		Message:     sanitizeInput(p.Message),
		Attachments: p.Attachments,
		ClientRunID: runID,
	}, false)
}

// SessionsSteer handles "sessions.steer" — patches session config, then starts a run.
func (h *Handler) SessionsSteer(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		Key          string `json:"key"`
		Message      string `json:"message,omitempty"`
		Thinking     string `json:"thinking,omitempty"`
		Model        string `json:"model,omitempty"` // role name: "main","lightweight","fallback"
		SystemPrompt string `json:"systemPrompt,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.WrapInvalidRequest("invalid sessions.steer params", err).Response(req.ID)
	}
	if p.Key == "" {
		return rpcerr.MissingParam("key").Response(req.ID)
	}

	// Interrupt any active run and clear the pending queue for this session.
	h.InterruptActiveRun(p.Key)
	h.pending.Clear(p.Key)

	runID := shortid.New("steer")

	return h.startAsyncRun(req.ID, RunParams{
		SessionKey:  p.Key,
		Message:     sanitizeInput(p.Message),
		Model:       p.Model,
		System:      p.SystemPrompt,
		ClientRunID: runID,
	}, true)
}

// SessionsAbort handles "sessions.abort" — cancels an active run by key or runId.
func (h *Handler) SessionsAbort(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		Key   string `json:"key"`
		RunID string `json:"runId,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.WrapInvalidRequest("invalid sessions.abort params", err).Response(req.ID)
	}
	if p.Key == "" && p.RunID == "" {
		return rpcerr.New(protocol.ErrMissingParam, "key or runId is required").Response(req.ID)
	}

	var abortedRunID string
	var sessionKey string
	if p.RunID != "" {
		sessionKey, abortedRunID = h.abort.CancelByRunID(p.RunID)
	} else {
		sessionKey = p.Key
		abortedRunID = h.abort.CancelBySessionKey(p.Key)
	}

	if abortedRunID == "" {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":     true,
			"status": "no-active-run",
		})
		return resp
	}

	// Clear pending queue and transition session out of running state.
	if sessionKey != "" {
		h.pending.Clear(sessionKey)
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
	}

	resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
		"ok":           true,
		"abortedRunId": abortedRunID,
		"status":       "aborted",
	})
	return resp
}

const (
	btwTimeout         = 30 * time.Second
	btwSystemPrompt    = "You are a helpful side-question assistant. Answer concisely in Korean unless the question is in another language."
	btwTranscriptLimit = 20
	btwResponseTag     = "\n\n— BTW"
)

// HandleBtw processes a side question without affecting the main session
// context. It runs synchronously on an ephemeral session that carries a
// snapshot of the parent transcript, using the same model as the parent.
func (h *Handler) HandleBtw(ctx context.Context, sessionKey, question string) (string, error) {
	if h.sessions == nil {
		return "", fmt.Errorf("chat handler not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, btwTimeout)
	defer cancel()

	// Ephemeral session — isolates writes from the caller's session.
	btwKey := "btw:" + shortid.New("btw")
	defer func() {
		h.sessions.Delete(btwKey)
		if h.transcript != nil {
			_ = h.transcript.Delete(btwKey)
		}
	}()

	// Clone recent transcript so btw has conversation context.
	if h.transcript != nil && sessionKey != "" {
		_ = h.transcript.CloneRecent(sessionKey, btwKey, btwTranscriptLimit)
	}

	// Inherit the parent session's model (empty = use default).
	var model string
	if parent := h.sessions.Get(sessionKey); parent != nil {
		model = parent.Model
	}

	maxTokens := 2048
	result, err := h.SendSync(ctx, btwKey, question, model, &SyncOptions{
		SystemPrompt: btwSystemPrompt,
		ToolPreset:   "conversation",
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("btw timeout: no response within %s", btwTimeout)
		}
		return "", fmt.Errorf("btw failed: %w", err)
	}

	text := strings.TrimSpace(result.Text)
	if text != "" {
		text += btwResponseTag
	}
	return text, nil
}

// History handles "chat.history" — returns capped, sanitized transcript.
func (h *Handler) History(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey string `json:"sessionKey"`
		Limit      int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.InvalidRequest("invalid chat.history params").Response(req.ID)
	}
	if p.SessionKey == "" {
		return rpcerr.MissingParam("sessionKey").Response(req.ID)
	}

	limit := p.Limit
	if limit <= 0 || limit > h.maxHistoryCount {
		limit = h.maxHistoryCount
	}

	// Use native transcript store when available.
	if h.transcript != nil {
		msgs, total, err := h.transcript.Load(p.SessionKey, limit)
		if err != nil {
			return rpcerr.WrapDependencyFailed("transcript load error", err).Response(req.ID)
		}
		if msgs == nil {
			msgs = []ChatMessage{}
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"messages": msgs,
			"total":    total,
		})
		return resp
	}

	// No transcript store available — return empty history.
	resp := protocol.MustResponseOK(req.ID, map[string]any{
		"messages": []ChatMessage{},
		"total":    0,
	})
	return resp
}

// Abort handles "chat.abort" — cancels a running chat session.
func (h *Handler) Abort(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		ClientRunID string `json:"clientRunId"`
		SessionKey  string `json:"sessionKey"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.InvalidRequest("invalid chat.abort params").Response(req.ID)
	}
	if p.ClientRunID == "" && p.SessionKey == "" {
		return rpcerr.New(protocol.ErrMissingParam, "clientRunId or sessionKey is required").Response(req.ID)
	}

	var resolvedKey string
	var found bool
	if p.ClientRunID != "" {
		key, runID := h.abort.CancelByRunID(p.ClientRunID)
		resolvedKey = key
		found = runID != ""
	} else {
		resolvedKey = p.SessionKey
		runID := h.abort.CancelBySessionKey(p.SessionKey)
		found = runID != ""
	}

	if !found {
		return rpcerr.NotFound("no active run").Response(req.ID)
	}

	// Clear pending queue and transition session to killed.
	key := resolvedKey
	if key == "" {
		key = p.SessionKey
	}
	if key != "" {
		h.pending.Clear(key)
		h.sessions.ApplyLifecycleEvent(key, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		if h.broadcast != nil {
			h.broadcast("sessions.changed", map[string]any{
				"sessionKey": key,
				"reason":     "aborted",
				"status":     "killed",
			})
		}
	}

	resp := protocol.MustResponseOK(req.ID, map[string]bool{"aborted": true})
	return resp
}

// SendDirect programmatically sends a message to a session and triggers an
// LLM run, just like chat.send but without going through RPC. Used by the
// bridge injector so the main agent automatically responds to bridge messages.
// Delivery context is derived from the session key (e.g., "telegram:123" →
// channel="telegram", to="123") so the response reaches the user's device.
func (h *Handler) SendDirect(sessionKey, message string) {
	params := RunParams{
		SessionKey: sessionKey,
		Message:    sanitizeInput(message),
		Delivery:   deliveryFromSessionKey(sessionKey),
	}

	if h.abort.HasActiveRun(sessionKey) {
		h.pending.Enqueue(sessionKey, params)
		h.logger.Info("bridge: queued message for active run", "sessionKey", sessionKey)
		return
	}

	h.startAsyncRun("bridge", params, false)
}

// deliveryFromSessionKey extracts a DeliveryContext from a session key.
// "telegram:7074071666" → Channel="telegram", To="7074071666".
func deliveryFromSessionKey(key string) *DeliveryContext {
	idx := strings.Index(key, ":")
	if idx < 0 {
		return nil
	}
	return &DeliveryContext{
		Channel: key[:idx],
		To:      key[idx+1:],
	}
}
