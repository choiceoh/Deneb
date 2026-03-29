package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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
		Model        string           `json:"model,omitempty"` // model ID or role name ("main","lightweight","fallback","image")
		WorkspaceDir string           `json:"workspaceDir,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.send params: "+err.Error()))
	}
	if p.SessionKey == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "sessionKey is required"))
	}
	if p.Message == "" && len(p.Attachments) == 0 {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "message or attachments required"))
	}
	if len(p.Message) > h.maxMessageBytes {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest,
			fmt.Sprintf("message too large: %d bytes exceeds limit of %d", len(p.Message), h.maxMessageBytes)))
	}

	// Pre-process slash commands before dispatching to agent.
	if slashResult := ParseSlashCommand(p.Message); slashResult != nil && slashResult.Handled {
		return h.handleSlashCommand(req.ID, p.SessionKey, p.Delivery, slashResult)
	}

	// Interrupt any active run on this session to prevent concurrent runs.
	h.InterruptActiveRun(p.SessionKey)

	return h.startAsyncRun(req.ID, RunParams{
		SessionKey:   p.SessionKey,
		Message:      sanitizeInput(p.Message),
		Attachments:  p.Attachments,
		Delivery:     p.Delivery,
		ClientRunID:  p.ClientRunID,
		Model:        p.Model,
		WorkspaceDir: p.WorkspaceDir,
	}, false)
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
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid sessions.send params: "+err.Error()))
	}
	if p.Key == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "key is required"))
	}

	// Interrupt any active run for this session.
	h.InterruptActiveRun(p.Key)

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
		Model        string `json:"model,omitempty"` // model ID or role name ("main","lightweight","fallback","image")
		SystemPrompt string `json:"systemPrompt,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid sessions.steer params: "+err.Error()))
	}
	if p.Key == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "key is required"))
	}

	// Interrupt any active run for this session.
	h.InterruptActiveRun(p.Key)

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
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid sessions.abort params: "+err.Error()))
	}
	if p.Key == "" && p.RunID == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "key or runId is required"))
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

	// Transition session out of running state.
	if sessionKey != "" {
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
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.history params"))
	}
	if p.SessionKey == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "sessionKey is required"))
	}

	limit := p.Limit
	if limit <= 0 || limit > h.maxHistoryCount {
		limit = h.maxHistoryCount
	}

	// Use native transcript store when available.
	if h.transcript != nil {
		msgs, total, err := h.transcript.Load(p.SessionKey, limit)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "transcript load error: "+err.Error()))
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
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.abort params"))
	}
	if p.ClientRunID == "" && p.SessionKey == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "clientRunId or sessionKey is required"))
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
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrNotFound, "no active run found"))
	}

	// Transition session to killed.
	key := resolvedKey
	if key == "" {
		key = p.SessionKey
	}
	if key != "" {
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
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.inject params"))
	}
	if p.SessionKey == "" || p.Content == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "sessionKey and content are required"))
	}
	if p.Role == "" {
		p.Role = "assistant"
	}

	content := sanitizeInput(p.Content)

	// Use native transcript store if available.
	if h.transcript != nil {
		msg := ChatMessage{
			Role:      p.Role,
			Content:   content,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := h.transcript.Append(p.SessionKey, msg); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "transcript write error: "+err.Error()))
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
	return protocol.NewResponseError(req.ID, protocol.NewError(
		protocol.ErrDependencyFailed, "no transcript store available"))
}
