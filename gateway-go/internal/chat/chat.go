// Package chat implements the chat/LLM message routing RPC methods.
//
// This mirrors src/gateway/server-methods/chat/chat.ts from the TypeScript
// codebase. The Go gateway handles chat message orchestration, history
// retrieval, abort signaling, and message injection.
//
// Heavy LLM work (agent invocation, tool execution) is forwarded to the
// Node.js plugin host via the bridge. The Go gateway owns message routing,
// sanitization, history budgeting, and session transcript persistence.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Forwarder sends requests to the Node.js plugin host for agent invocation.
type Forwarder interface {
	Forward(ctx context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error)
}

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

// BroadcastRawFunc sends pre-serialized event data to all matching subscribers.
type BroadcastRawFunc func(event string, data []byte) int

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext struct {
	Channel   string `json:"channel,omitempty"`
	To        string `json:"to,omitempty"`
	AccountID string `json:"accountId,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
}

// ChatMessage represents a message in a session transcript.
type ChatMessage struct {
	Role        string           `json:"role"`
	Content     string           `json:"content,omitempty"`
	Attachments []ChatAttachment `json:"attachments,omitempty"`
	Timestamp   int64            `json:"timestamp,omitempty"`
	ParentID    string           `json:"parentId,omitempty"`
	ID          string           `json:"id,omitempty"`
}

// ChatAttachment represents an attachment on a chat message.
type ChatAttachment struct {
	Type     string `json:"type"`               // "image", "file", "audio"
	MimeType string `json:"mimeType,omitempty"`
	URL      string `json:"url,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// AbortEntry tracks an active abort controller for a running chat session.
type AbortEntry struct {
	SessionKey string
	ClientRun  string
	CancelFn   context.CancelFunc
	ExpiresAt  time.Time
}

// Handler manages chat RPC methods.
type Handler struct {
	sessions     *session.Manager
	forwarder    Forwarder
	broadcast    BroadcastFunc
	broadcastRaw BroadcastRawFunc
	logger       *slog.Logger

	abortMu     sync.Mutex
	abortMap    map[string]*AbortEntry // clientRunId -> entry
	done        chan struct{}          // signals abortGCLoop to stop

	// maxHistoryBytes caps the total JSON bytes returned by chat.history.
	maxHistoryBytes int
	// maxHistoryCount caps the number of messages returned.
	maxHistoryCount int
	// maxMessageBytes caps a single message body before truncation.
	maxMessageBytes int
}

// HandlerConfig configures the chat handler.
type HandlerConfig struct {
	MaxHistoryBytes int
	MaxHistoryCount int
	MaxMessageBytes int
}

// DefaultHandlerConfig returns sensible defaults.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		MaxHistoryBytes: 2 * 1024 * 1024, // 2 MB
		MaxHistoryCount: 200,
		MaxMessageBytes: 128 * 1024, // 128 KB
	}
}

// NewHandler creates a new chat handler.
func NewHandler(sessions *session.Manager, forwarder Forwarder, broadcast BroadcastFunc, logger *slog.Logger, cfg HandlerConfig) *Handler {
	if cfg.MaxHistoryBytes == 0 {
		cfg = DefaultHandlerConfig()
	}
	h := &Handler{
		sessions:        sessions,
		forwarder:       forwarder,
		broadcast:       broadcast,
		logger:          logger,
		abortMap:        make(map[string]*AbortEntry),
		done:            make(chan struct{}),
		maxHistoryBytes: cfg.MaxHistoryBytes,
		maxHistoryCount: cfg.MaxHistoryCount,
		maxMessageBytes: cfg.MaxMessageBytes,
	}
	go h.abortGCLoop()
	return h
}

// SetBroadcastRaw sets the raw broadcast function for streaming event relay.
func (h *Handler) SetBroadcastRaw(fn BroadcastRawFunc) {
	h.broadcastRaw = fn
}

// Close stops background goroutines and cancels all active abort entries.
func (h *Handler) Close() {
	// Signal abortGCLoop to exit.
	select {
	case <-h.done:
	default:
		close(h.done)
	}

	h.abortMu.Lock()
	defer h.abortMu.Unlock()
	for _, entry := range h.abortMap {
		entry.CancelFn()
	}
	h.abortMap = make(map[string]*AbortEntry)
}

// --- RPC method handlers ---

// Send handles "chat.send" — the primary message ingestion endpoint.
// Sanitizes input, resolves delivery context, forwards to the agent via bridge,
// and broadcasts session events.
func (h *Handler) Send(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey  string           `json:"sessionKey"`
		Message     string           `json:"message"`
		Attachments []ChatAttachment `json:"attachments,omitempty"`
		Delivery    *DeliveryContext `json:"delivery,omitempty"`
		ClientRunID string           `json:"clientRunId,omitempty"`
		Model       string           `json:"model,omitempty"`
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

	// Sanitize input.
	p.Message = sanitizeInput(p.Message)

	// Ensure session exists.
	sess := h.sessions.Get(p.SessionKey)
	if sess == nil {
		sess = h.sessions.Create(p.SessionKey, session.KindDirect)
	}

	// Check if session is already running (prevent concurrent sends).
	if sess.Status == session.StatusRunning {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrConflict, "session is already running"))
	}

	// Transition session to running.
	h.sessions.ApplyLifecycleEvent(p.SessionKey, session.LifecycleEvent{
		Phase: session.PhaseStart,
		Ts:    time.Now().UnixMilli(),
	})

	// Create abort context for this run.
	runCtx, runCancel := context.WithCancel(ctx)
	if p.ClientRunID != "" {
		h.abortMu.Lock()
		h.abortMap[p.ClientRunID] = &AbortEntry{
			SessionKey: p.SessionKey,
			ClientRun:  p.ClientRunID,
			CancelFn:   runCancel,
			ExpiresAt:  time.Now().Add(30 * time.Minute),
		}
		h.abortMu.Unlock()
	}

	// Broadcast session start event.
	if h.broadcast != nil {
		h.broadcast("sessions.changed", map[string]any{
			"sessionKey": p.SessionKey,
			"reason":     "message_sent",
			"status":     "running",
		})
	}

	// Forward to Node.js agent via bridge.
	if h.forwarder == nil {
		runCancel()
		h.cleanupAbort(p.ClientRunID)
		h.sessions.ApplyLifecycleEvent(p.SessionKey, session.LifecycleEvent{
			Phase: session.PhaseError,
			Ts:    time.Now().UnixMilli(),
		})
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrDependencyFailed, "no agent bridge available"))
	}

	// Build the forwarded request with sanitized params.
	forwardParams, _ := json.Marshal(map[string]any{
		"sessionKey":  p.SessionKey,
		"message":     p.Message,
		"attachments": p.Attachments,
		"delivery":    p.Delivery,
		"clientRunId": p.ClientRunID,
		"model":       p.Model,
	})
	forwardReq := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     req.ID,
		Method: "chat.send",
		Params: forwardParams,
	}
	resp, err := h.forwarder.Forward(runCtx, forwardReq)
	runCancel()
	h.cleanupAbort(p.ClientRunID)

	if err != nil {
		h.logger.Error("chat.send bridge forward failed", "session", p.SessionKey, "error", err)
		h.sessions.ApplyLifecycleEvent(p.SessionKey, session.LifecycleEvent{
			Phase: session.PhaseError,
			Ts:    time.Now().UnixMilli(),
		})
		if h.broadcast != nil {
			h.broadcast("sessions.changed", map[string]any{
				"sessionKey": p.SessionKey,
				"reason":     "error",
				"status":     "failed",
			})
		}
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrDependencyFailed, "agent bridge error: "+err.Error()))
	}

	// Mark session done.
	h.sessions.ApplyLifecycleEvent(p.SessionKey, session.LifecycleEvent{
		Phase: session.PhaseEnd,
		Ts:    time.Now().UnixMilli(),
	})
	if h.broadcast != nil {
		h.broadcast("sessions.changed", map[string]any{
			"sessionKey": p.SessionKey,
			"reason":     "completed",
			"status":     "done",
		})
	}

	return resp
}

// History handles "chat.history" — returns capped, sanitized transcript.
func (h *Handler) History(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
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

	// Forward to Node.js to read transcript from disk.
	if h.forwarder == nil {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"messages": []ChatMessage{},
			"total":    0,
		})
		return resp
	}

	forwardParams, _ := json.Marshal(map[string]any{
		"sessionKey": p.SessionKey,
		"limit":      limit,
	})
	forwardReq := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     req.ID,
		Method: "chat.history",
		Params: forwardParams,
	}
	resp, err := h.forwarder.Forward(ctx, forwardReq)
	if err != nil {
		h.logger.Error("chat.history bridge forward failed", "session", p.SessionKey, "error", err)
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrDependencyFailed, "bridge error: "+err.Error()))
	}

	// Budget enforcement: cap total response bytes.
	if len(resp.Payload) > h.maxHistoryBytes {
		resp = h.budgetHistory(req.ID, resp.Payload)
	}

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
		// Abort by session key — cancel all runs for the session.
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

	// Transition session to killed. Use the key from the abort entry
	// (not params) when aborting by clientRunId.
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

	resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"aborted": true})
	return resp
}

// Inject handles "chat.inject" — injects an assistant message directly into the transcript.
func (h *Handler) Inject(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
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

	// Forward to Node.js for transcript persistence.
	if h.forwarder == nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrDependencyFailed, "no agent bridge available"))
	}

	forwardParams, _ := json.Marshal(map[string]any{
		"sessionKey": p.SessionKey,
		"role":       p.Role,
		"content":    sanitizeInput(p.Content),
	})
	forwardReq := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     req.ID,
		Method: "chat.inject",
		Params: forwardParams,
	}
	resp, err := h.forwarder.Forward(ctx, forwardReq)
	if err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrDependencyFailed, "bridge error: "+err.Error()))
	}

	if h.broadcast != nil {
		h.broadcast("sessions.changed", map[string]any{
			"sessionKey": p.SessionKey,
			"reason":     "injected",
		})
	}

	return resp
}

// --- Helpers ---

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
		resp, _ := protocol.NewResponseOK(reqID, map[string]any{
			"messages":  []any{},
			"total":     0,
			"truncated": true,
			"error":     "failed to parse history for budgeting",
		})
		return resp
	}

	// Keep messages from the end (most recent) until budget exhausted.
	// Collect in reverse order, then reverse once to avoid O(n²) prepend.
	reversed := make([]json.RawMessage, 0, len(parsed.Messages))
	totalBytes := 0
	truncatedCount := 0
	for i := len(parsed.Messages) - 1; i >= 0; i-- {
		msgBytes := len(parsed.Messages[i])
		if msgBytes > h.maxMessageBytes {
			// Replace oversized message with placeholder.
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
	budgeted := make([]json.RawMessage, len(reversed))
	for i, msg := range reversed {
		budgeted[len(reversed)-1-i] = msg
	}

	resp, _ := protocol.NewResponseOK(reqID, map[string]any{
		"messages":       budgeted,
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
