// Package server — hooks_http_exec.go contains webhook sub-handlers, policy
// helpers, template rendering, and shared utility functions for the hooks HTTP
// handler. Split from hooks_http.go for readability.
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ───────────────────────────────────────────────────────────────────────
// Sub-handlers.
// ───────────────────────────────────────────────────────────────────────

func (h *HooksHTTPHandler) handleWake(w http.ResponseWriter, body map[string]any) {
	text, _ := body["text"].(string)

	// #1: Validate text after extraction.
	text = strings.TrimSpace(text)
	if text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "text is required"})
		return
	}

	mode, _ := body["mode"].(string)
	if mode == "" {
		mode = "now"
	}
	if mode != "now" && mode != "next-heartbeat" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "mode must be \"now\" or \"next-heartbeat\""})
		return
	}
	h.dispatchers.DispatchWake(text, mode)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode})
}

func (h *HooksHTTPHandler) handleAgent(w http.ResponseWriter, r *http.Request, body map[string]any, token string) {
	var payload HookAgentPayload
	// Re-marshal and unmarshal for clean type conversion.
	raw, _ := json.Marshal(body)
	if err := json.Unmarshal(raw, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid payload"})
		return
	}
	if strings.TrimSpace(payload.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "message is required"})
		return
	}

	// #13: Default name="Hook".
	if payload.Name == "" {
		payload.Name = "Hook"
	}

	// #2: Resolve wakeMode with validation.
	wakeMode := "now"
	if wm, ok := body["wakeMode"].(string); ok && (wm == "now" || wm == "next-heartbeat") {
		wakeMode = wm
	}

	// Agent policy check.
	if !h.isAgentAllowed(payload.AgentID) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "agent not allowed by hooks.allowedAgentIds policy"})
		return
	}

	// Session key resolution.
	sessionKey, err := h.resolveSessionKey(payload.SessionKey)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	// Resolve target agent ID (fall back to default if unknown).
	agentID := h.resolveAgentID(payload.AgentID)

	// #4: Resolve idempotency key from headers first, then body.
	idempotencyKey := resolveIdempotencyKey(r, body)

	// Idempotency check.
	scopeJSON, _ := json.Marshal(map[string]any{
		"pathKey":    "agent",
		"agentId":    agentID,
		"sessionKey": sessionKey,
		"message":    payload.Message,
		"name":       payload.Name,
	})
	replayKey := h.replayCache.buildKey(token, string(scopeJSON), idempotencyKey)
	if cachedRunID, ok := h.replayCache.get(replayKey); ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": cachedRunID})
		return
	}

	runID := h.dispatchers.DispatchAgent(HookAgentDispatchPayload{
		Message:        payload.Message,
		Name:           payload.Name,
		AgentID:        agentID,
		IdempotencyKey: idempotencyKey,
		SessionKey:     sessionKey,
		Deliver:        payload.Deliver,
		Channel:        payload.Channel,
		To:             payload.To,
		Model:          payload.Model,
		Thinking:       payload.Thinking,
		TimeoutSeconds: payload.TimeoutSeconds,
		WakeMode:       wakeMode,
	})

	h.replayCache.set(replayKey, runID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": runID})
}

// handleMapping tries to match a subPath against configured mappings.
// Returns true if a mapping matched and the response was written.
func (h *HooksHTTPHandler) handleMapping(w http.ResponseWriter, r *http.Request, subPath string, body map[string]any, token string, tmplCtx templateContext) bool {
	for _, m := range h.config.Mappings {
		if !matchesMapping(m, subPath, body) {
			continue
		}

		// #10: If mapping has no action, return 204 No Content.
		if m.Action == "" {
			w.WriteHeader(http.StatusNoContent)
			return true
		}

		switch m.Action {
		case "wake":
			text := resolveTemplate(m.TextTemplate, tmplCtx)

			// #9: Validate text is non-empty after template resolution.
			text = strings.TrimSpace(text)
			if text == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "hook mapping requires text"})
				return true
			}

			mode := m.WakeMode
			if mode == "" {
				mode = "now"
			}
			h.dispatchers.DispatchWake(text, mode)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode})
			return true

		case "agent":
			message := resolveTemplate(m.MessageTemplate, tmplCtx)
			if message == "" {
				// Fall back to body's message field.
				message, _ = body["message"].(string)
			}
			if strings.TrimSpace(message) == "" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "message is required"})
				return true
			}

			agentID := m.AgentID
			if !h.isAgentAllowed(agentID) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "agent not allowed by hooks.allowedAgentIds policy"})
				return true
			}

			sessionKey, err := h.resolveSessionKey(m.SessionKey)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
				return true
			}

			agentID = h.resolveAgentID(agentID)

			// #4: Resolve idempotency key from headers first, then body.
			idempotencyKey := resolveIdempotencyKey(r, body)

			// #12: Apply template rendering to mapping fields.
			name := resolveTemplate(m.Name, tmplCtx)
			if name == "" {
				name = "Hook"
			}
			channel := resolveTemplate(m.Channel, tmplCtx)
			to := resolveTemplate(m.To, tmplCtx)
			model := resolveTemplate(m.Model, tmplCtx)
			thinking := resolveTemplate(m.Thinking, tmplCtx)

			scopeJSON, _ := json.Marshal(map[string]any{
				"pathKey":    subPath,
				"agentId":    agentID,
				"sessionKey": sessionKey,
				"message":    message,
				"name":       name,
			})
			replayKey := h.replayCache.buildKey(token, string(scopeJSON), idempotencyKey)
			if cachedRunID, ok := h.replayCache.get(replayKey); ok {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": cachedRunID})
				return true
			}

			runID := h.dispatchers.DispatchAgent(HookAgentDispatchPayload{
				Message:        message,
				Name:           name,
				AgentID:        agentID,
				IdempotencyKey: idempotencyKey,
				SessionKey:     sessionKey,
				Deliver:        m.Deliver,
				Channel:        channel,
				To:             to,
				Model:          model,
				Thinking:       thinking,
				TimeoutSeconds: m.TimeoutSeconds,
			})
			h.replayCache.set(replayKey, runID)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": runID})
			return true
		}
	}
	return false
}

// ───────────────────────────────────────────────────────────────────────
// Policy helpers.
// ───────────────────────────────────────────────────────────────────────

// isAgentAllowed checks whether the given agentID passes the allowedAgentIds policy.
// nil AllowedAgentIDs means all agents are allowed.
func (h *HooksHTTPHandler) isAgentAllowed(agentID string) bool {
	if h.config.AllowedAgentIDs == nil {
		return true
	}
	if agentID == "" {
		// Empty = default agent; always allowed.
		return true
	}
	for _, id := range h.config.AllowedAgentIDs {
		if id == agentID {
			return true
		}
	}
	return false
}

// resolveAgentID maps the requested agentID to one known by the gateway.
// Empty string means "use default" — the dispatcher is expected to handle that.
func (h *HooksHTTPHandler) resolveAgentID(agentID string) string {
	return agentID
}

// resolveSessionKey implements the 3-stage session key fallback:
//  1. Explicit (if AllowRequestSessionKey is true and value passes prefix checks)
//  2. DefaultSessionKey from config
//  3. Generated "hook:<uuid>"
func (h *HooksHTTPHandler) resolveSessionKey(requestKey string) (string, error) {
	key := strings.TrimSpace(requestKey)

	// Stage 1: Explicit key from request.
	if key != "" {
		if !h.config.AllowRequestSessionKey {
			return "", fmt.Errorf("sessionKey in request body is not allowed (hooks.allowRequestSessionKey is false)")
		}
		if !h.isSessionKeyAllowed(key) {
			return "", fmt.Errorf("sessionKey does not match hooks.allowedSessionKeyPrefixes")
		}
		return key, nil
	}

	// Stage 2: Default from config.
	if h.config.DefaultSessionKey != "" {
		return h.config.DefaultSessionKey, nil
	}

	// Stage 3: Generate "hook:<uuid>".
	return "hook:" + generateUUID(), nil
}

// isSessionKeyAllowed checks the key against AllowedSessionKeyPrefixes.
// If no prefixes are configured, all keys are allowed.
func (h *HooksHTTPHandler) isSessionKeyAllowed(key string) bool {
	if len(h.config.AllowedSessionKeyPrefixes) == 0 {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(key))
	for _, prefix := range h.config.AllowedSessionKeyPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// ───────────────────────────────────────────────────────────────────────
// Idempotency key resolution.
// ───────────────────────────────────────────────────────────────────────

// resolveIdempotencyKey checks HTTP headers first, then falls back to body.
// Returns empty string if no valid key is found.
func resolveIdempotencyKey(r *http.Request, body map[string]any) string {
	// Check headers first.
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		if len(key) <= maxIdempotencyKeyLen {
			return key
		}
		return ""
	}
	if key := r.Header.Get("X-Deneb-Idempotency-Key"); key != "" {
		if len(key) <= maxIdempotencyKeyLen {
			return key
		}
		return ""
	}
	// Fall back to body.
	if key, ok := body["idempotencyKey"].(string); ok && key != "" {
		if len(key) <= maxIdempotencyKeyLen {
			return key
		}
	}
	return ""
}

// ───────────────────────────────────────────────────────────────────────
// Template rendering.
// ───────────────────────────────────────────────────────────────────────

// buildTemplateContext creates a templateContext from the HTTP request and parsed body.
func buildTemplateContext(r *http.Request, subPath string, payload map[string]any) templateContext {
	headers := make(map[string]string)
	for k := range r.Header {
		headers[strings.ToLower(k)] = r.Header.Get(k)
	}
	query := make(map[string]string)
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	return templateContext{
		Payload: payload,
		Headers: headers,
		Query:   query,
		Path:    subPath,
	}
}

// resolveTemplate applies template expressions by replacing {{expr}} placeholders.
// Supports: {{path}}, {{now}}, {{headers.<key>}}, {{query.<key>}}, {{payload.<dotpath>}}.
// Blocked keys (__proto__, prototype, constructor) are rejected for security.
func resolveTemplate(tmpl string, ctx templateContext) string {
	if tmpl == "" {
		return ""
	}
	result := templateExprRegex.ReplaceAllStringFunc(tmpl, func(match string) string {
		expr := strings.TrimSpace(match[2 : len(match)-2])
		return resolveTemplateExpr(expr, ctx)
	})
	return result
}

// resolveTemplateExpr resolves a single template expression.
func resolveTemplateExpr(expr string, ctx templateContext) string {
	if expr == "" {
		return ""
	}

	// {{path}}
	if expr == "path" {
		return ctx.Path
	}

	// {{now}}
	if expr == "now" {
		return time.Now().UTC().Format(time.RFC3339)
	}

	// {{headers.<key>}}
	if strings.HasPrefix(expr, "headers.") {
		key := strings.ToLower(strings.TrimPrefix(expr, "headers."))
		if _, blocked := blockedTemplateKeys[key]; key != "" && !blocked {
			if v, ok := ctx.Headers[key]; ok {
				return v
			}
		}
		return ""
	}

	// {{query.<key>}}
	if strings.HasPrefix(expr, "query.") {
		key := strings.TrimPrefix(expr, "query.")
		if _, blocked := blockedTemplateKeys[key]; key != "" && !blocked {
			if v, ok := ctx.Query[key]; ok {
				return v
			}
		}
		return ""
	}

	// {{payload.<dotpath>}}
	if strings.HasPrefix(expr, "payload.") {
		dotPath := strings.TrimPrefix(expr, "payload.")
		return resolvePayloadDotPath(ctx.Payload, dotPath)
	}

	// Direct key lookup in payload for backward compatibility.
	if _, blocked := blockedTemplateKeys[expr]; !blocked {
		if v, ok := ctx.Payload[expr]; ok {
			return fmt.Sprintf("%v", v)
		}
	}

	return ""
}

// resolvePayloadDotPath traverses a nested map using dot-separated keys.
// Blocks __proto__, prototype, and constructor keys for security.
func resolvePayloadDotPath(payload map[string]any, dotPath string) string {
	if dotPath == "" || payload == nil {
		return ""
	}
	parts := strings.Split(dotPath, ".")
	var current any = payload
	for _, part := range parts {
		if _, blocked := blockedTemplateKeys[part]; blocked {
			return ""
		}
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = m[part]
		if !ok {
			return ""
		}
	}
	if current == nil {
		return ""
	}
	return fmt.Sprintf("%v", current)
}

// Transform functions omitted by design — template rendering + JSONPath extraction
// covers the single-user deployment use case without requiring a scripting engine.

// ───────────────────────────────────────────────────────────────────────
// Utility functions.
// ───────────────────────────────────────────────────────────────────────

// extractToken reads the auth token from Authorization: Bearer or X-Deneb-Token.
func extractToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token := strings.TrimSpace(auth[7:])
		if token != "" {
			return token
		}
	}
	token := strings.TrimSpace(r.Header.Get("X-Deneb-Token"))
	if token != "" {
		return token
	}
	return ""
}

// constantTimeEqual compares two strings in constant time to prevent timing attacks.
func constantTimeEqual(a, b string) bool {
	if a == "" && b == "" {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// resolveClientIP extracts the client IP from the request.
func resolveClientIP(r *http.Request) string {
	// Check X-Forwarded-For first (first entry).
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	// Fall back to RemoteAddr (strip port).
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		// Handle IPv6 in brackets: [::1]:port
		if bracketIdx := strings.LastIndex(addr, "]"); bracketIdx != -1 && bracketIdx < idx {
			return addr[1:bracketIdx]
		}
		return addr[:idx]
	}
	return addr
}

// readJSONBody reads and parses the request body as JSON with a size limit.
func readJSONBody(r *http.Request, maxBytes int64) (map[string]any, error) {
	limited := io.LimitReader(r.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("failed to read body")
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("payload too large")
	}
	if len(data) == 0 {
		return make(map[string]any), nil
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	return result, nil
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // best-effort: response already committed
}

// writeText writes a plain text response.
func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg) // best-effort: response already committed
}

// sha256Hex returns the hex-encoded SHA256 of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// generateUUID generates a random UUID v4.
func generateUUID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback: timestamp-based pseudo-UUID.
		t := time.Now().UnixNano()
		for i := range buf {
			buf[i] = byte(t >> (i * 4)) //nolint:gosec // G115 — extracting individual bytes from int64 timestamp
		}
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// matchesMapping checks if a subPath and payload match a mapping's criteria.
// #7: Source matching now checks payload.source instead of subPath.
func matchesMapping(m HookMapping, subPath string, payload map[string]any) bool {
	pathMatches := true
	if m.MatchPath != "" {
		// Normalize: strip leading slash from both for comparison.
		normalized := strings.TrimLeft(m.MatchPath, "/")
		pathMatches = normalized == subPath
	}

	sourceMatches := true
	if m.MatchSource != "" {
		payloadSource, _ := payload["source"].(string)
		sourceMatches = payloadSource == m.MatchSource
	}

	// Both conditions must match if specified.
	if m.MatchPath != "" && m.MatchSource != "" {
		return pathMatches && sourceMatches
	}
	if m.MatchPath != "" {
		return pathMatches
	}
	if m.MatchSource != "" {
		return sourceMatches
	}
	return false
}
