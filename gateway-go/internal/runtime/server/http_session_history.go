package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// sseKeepaliveInterval is the interval for SSE keepalive comments.
	sseKeepaliveInterval = 15 * time.Second
	// maxHistoryLimit caps the number of messages returned.
	maxHistoryLimit = 1000
)

// handleSessionHistory handles GET /sessions/{key}/history — returns session transcript.
// Supports JSON and SSE modes based on Accept header.
// Mirrors src/gateway/session/sessions-history-http.ts.
func (s *Server) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok": false, "error": map[string]string{"type": "method_not_allowed", "message": "GET required"},
		})
		return
	}

	// Authenticate.
	if !s.authorizeHTTP(w, r) {
		return
	}

	// Extract session key.
	rawKey := r.PathValue("key")
	sessionKey, err := url.PathUnescape(rawKey)
	if err != nil || sessionKey == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": map[string]string{"type": "invalid_request", "message": "invalid session key"},
		})
		return
	}

	// Check session exists.
	sess := s.sessions.Get(sessionKey)
	if sess == nil {
		s.writeJSON(w, http.StatusNotFound, map[string]any{
			"ok": false, "error": map[string]string{"type": "not_found", "message": "session not found"},
		})
		return
	}

	// Parse query params.
	limit := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > maxHistoryLimit {
				limit = maxHistoryLimit
			}
		}
	}

	cursor := 0
	if c := r.URL.Query().Get("cursor"); c != "" {
		// Format: "seq:N" or just "N"
		c = strings.TrimPrefix(c, "seq:")
		if parsed, err := strconv.Atoi(c); err == nil && parsed > 0 {
			cursor = parsed
		}
	}

	// Read transcript messages.
	if s.transcript == nil {
		s.writeJSON(w, http.StatusOK, map[string]any{
			"sessionKey": sessionKey,
			"items":      []any{},
			"messages":   []any{},
			"hasMore":    false,
		})
		return
	}

	messages, err := s.transcript.ReadMessages(sessionKey)
	if err != nil {
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok": false, "error": map[string]string{"type": "internal_error", "message": "failed to read transcript: " + err.Error()},
		})
		return
	}

	// Apply pagination.
	total := len(messages)
	startIdx := 0
	endIdx := total
	hasMore := false
	nextCursor := ""

	if cursor > 0 && cursor <= total {
		endIdx = cursor
	}
	if limit > 0 && endIdx-startIdx > limit {
		startIdx = endIdx - limit
		hasMore = startIdx > 0
	} else {
		hasMore = startIdx > 0
	}
	if hasMore && startIdx > 0 {
		nextCursor = fmt.Sprintf("seq:%d", startIdx)
	}

	items := messages[startIdx:endIdx]

	// Check if SSE mode.
	isSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")

	if !isSSE {
		// JSON mode.
		result := map[string]any{
			"sessionKey": sessionKey,
			"items":      items,
			"messages":   items,
			"hasMore":    hasMore,
		}
		if nextCursor != "" {
			result["nextCursor"] = nextCursor
		}
		s.writeJSON(w, http.StatusOK, result)
		return
	}

	// SSE mode.
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"ok": false, "error": map[string]string{"type": "internal_error", "message": "streaming not supported"},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Send retry directive.
	fmt.Fprintf(w, "retry: 1000\n\n")
	flusher.Flush()

	// Send initial history event.
	historyPayload := map[string]any{
		"sessionKey": sessionKey,
		"items":      items,
		"messages":   items,
		"hasMore":    hasMore,
	}
	if nextCursor != "" {
		historyPayload["nextCursor"] = nextCursor
	}
	historyData, _ := json.Marshal(historyPayload)
	fmt.Fprintf(w, "event: history\ndata: %s\n\n", historyData)
	flusher.Flush()

	// Subscribe to transcript updates.
	msgCh := make(chan json.RawMessage, 16)
	unsubscribe := s.transcript.OnAppend(func(key string, msg json.RawMessage) {
		if key == sessionKey {
			select {
			case msgCh <- msg:
			default:
				// Drop if channel full (slow consumer).
			}
		}
	})
	defer unsubscribe()

	// SSE event loop: keepalive + new messages.
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-msgCh:
			data, _ := json.Marshal(msg)
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
