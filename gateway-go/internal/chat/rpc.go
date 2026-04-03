package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
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
		DeepWork     bool             `json:"deepWork,omitempty"` // extended autonomous mode (2-3 hours)
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.InvalidRequest("invalid chat.send params: " + err.Error()).Response(req.ID)
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

	// When a run is already active for this session, queue the message
	// instead of interrupting. The active run completes normally (preserving
	// its full context), then the queued message is processed automatically.
	// This prevents the "amnesia" bug where the assistant forgets in-progress
	// work when the user sends a message mid-execution.
	runParams := RunParams{
		SessionKey:   p.SessionKey,
		Message:      sanitizeInput(p.Message),
		Attachments:  p.Attachments,
		Delivery:     p.Delivery,
		ClientRunID:  p.ClientRunID,
		Model:        p.Model,
		WorkspaceDir: p.WorkspaceDir,
		DeepWork:     p.DeepWork,
	}

	if h.hasActiveRunForSession(p.SessionKey) {
		h.enqueuePending(p.SessionKey, runParams)
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
		return rpcerr.InvalidRequest("invalid sessions.send params: " + err.Error()).Response(req.ID)
	}
	if p.Key == "" {
		return rpcerr.MissingParam("key").Response(req.ID)
	}

	// Interrupt any active run and clear the pending queue for this session.
	// Without clearPending, queued user messages survive the interrupt and
	// replay after the new run completes — causing the "diary navigation" bug
	// where the user's reply is discarded and a scheduled task takes over.
	h.InterruptActiveRun(p.Key)
	h.clearPending(p.Key)

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
		return rpcerr.InvalidRequest("invalid sessions.steer params: " + err.Error()).Response(req.ID)
	}
	if p.Key == "" {
		return rpcerr.MissingParam("key").Response(req.ID)
	}

	// Interrupt any active run and clear the pending queue for this session.
	h.InterruptActiveRun(p.Key)
	h.clearPending(p.Key)

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
		return rpcerr.InvalidRequest("invalid sessions.abort params: " + err.Error()).Response(req.ID)
	}
	if p.Key == "" && p.RunID == "" {
		return rpcerr.New(protocol.ErrMissingParam, "key or runId is required").Response(req.ID)
	}

	h.abortMu.Lock()
	var abortedRunID string
	var sessionKey string
	if p.RunID != "" {
		if entry, ok := h.abortMap[p.RunID]; ok {
			entry.CancelFn()
			abortedRunID = p.RunID
			sessionKey = entry.SessionKey
			delete(h.abortMap, p.RunID)
		}
	} else {
		sessionKey = p.Key
		for id, entry := range h.abortMap {
			if entry.SessionKey == p.Key {
				entry.CancelFn()
				abortedRunID = id
				delete(h.abortMap, id)
			}
		}
	}
	h.abortMu.Unlock()

	if abortedRunID == "" {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":     true,
			"status": "no-active-run",
		})
		return resp
	}

	// Clear pending queue and transition session out of running state.
	if sessionKey != "" {
		h.clearPending(sessionKey)
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

// HandleBtw processes a side question (/btw) without affecting the main
// session context. It dispatches a lightweight chat.send-style request
// with the side question, using the fast model default.
func (h *Handler) HandleBtw(_ context.Context, sessionKey, question string) (string, error) {
	// Build a side-question request and dispatch through Send.
	// The answer is returned directly without persisting to the main transcript.
	req, err := protocol.NewRequestFrame("btw-internal", "chat.send", map[string]any{
		"sessionKey": sessionKey,
		"message":    question,
		"btw":        true,
	})
	if err != nil {
		return "", fmt.Errorf("btw request build failed: %w", err)
	}

	resp := h.Send(context.Background(), req)
	if resp == nil {
		return "", fmt.Errorf("btw returned nil response")
	}
	if !resp.OK {
		msg := "unknown error"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return "", fmt.Errorf("btw failed: %s", msg)
	}

	// Extract text from response payload.
	var result struct {
		Text string `json:"text"`
	}
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &result); err != nil {
			return "", fmt.Errorf("btw response unmarshal failed: %w", err)
		}
	}
	return result.Text, nil
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
			return rpcerr.DependencyFailed("transcript load error: " + err.Error()).Response(req.ID)
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

	h.abortMu.Lock()
	var found bool
	var resolvedKey string
	if p.ClientRunID != "" {
		if entry, ok := h.abortMap[p.ClientRunID]; ok {
			resolvedKey = entry.SessionKey
			entry.CancelFn()
			delete(h.abortMap, p.ClientRunID)
			found = true
		}
	} else {
		resolvedKey = p.SessionKey
		for id, entry := range h.abortMap {
			if entry.SessionKey == p.SessionKey {
				entry.CancelFn()
				delete(h.abortMap, id)
				found = true
			}
		}
	}
	h.abortMu.Unlock()

	if !found {
		return rpcerr.NotFound("no active run").Response(req.ID)
	}

	// Clear pending queue and transition session to killed.
	key := resolvedKey
	if key == "" {
		key = p.SessionKey
	}
	if key != "" {
		h.clearPending(key)
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

// Inject handles "chat.inject" — injects a message directly into the transcript.
func (h *Handler) Inject(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey string `json:"sessionKey"`
		Role       string `json:"role"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return rpcerr.InvalidRequest("invalid chat.inject params").Response(req.ID)
	}
	if p.SessionKey == "" || p.Content == "" {
		return rpcerr.New(protocol.ErrMissingParam, "sessionKey and content are required").Response(req.ID)
	}
	if p.Role == "" {
		p.Role = "assistant"
	}

	content := sanitizeInput(p.Content)

	// Use native transcript store if available.
	if h.transcript != nil {
		msg := NewTextChatMessage(p.Role, content, time.Now().UnixMilli())
		if err := h.transcript.Append(p.SessionKey, msg); err != nil {
			return rpcerr.DependencyFailed("transcript write error: " + err.Error()).Response(req.ID)
		}
		if h.broadcast != nil {
			h.broadcast("sessions.changed", map[string]any{
				"sessionKey": p.SessionKey,
				"reason":     "injected",
			})
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"injected": true})
		return resp
	}

	// No transcript store available.
	return rpcerr.DependencyFailed("no transcript store available").Response(req.ID)
}
