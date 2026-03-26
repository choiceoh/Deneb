// Package server — hooks_http.go implements the HTTP webhook handler for /hooks/*.
//
// This mirrors the TypeScript implementation at src/gateway/http/server-http.ts
// (createHooksRequestHandler) and src/gateway/hooks.ts. It provides:
//   - Bearer/header token auth with constant-time comparison
//   - Rate limiting on auth failures (20 per 60s per IP → 429)
//   - /hooks/wake — dispatch wake events
//   - /hooks/agent — dispatch agent jobs with idempotency
//   - /hooks/<custom> — match against configured mappings
//   - SHA256-based replay cache (5-minute TTL, max 1000 entries)
//   - Session key resolution with 3-stage fallback
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ───────────────────────────────────────────────────────────────────────
// Defaults (matching TypeScript constants).
// ───────────────────────────────────────────────────────────────────────

const (
	defaultHooksBasePath    = "/hooks"
	defaultHooksMaxBodyBytes = 256 * 1024 // 256 KB
	hookAuthFailureLimit    = 20
	hookAuthFailureWindowMs = 60_000
	hookReplayCacheTTL      = 5 * time.Minute
	hookReplayCacheMax      = 1000
	maxIdempotencyKeyLen    = 256
)

// ───────────────────────────────────────────────────────────────────────
// Configuration types.
// ───────────────────────────────────────────────────────────────────────

// HooksHTTPConfig holds resolved webhook HTTP configuration.
type HooksHTTPConfig struct {
	BasePath                  string
	Token                     string
	MaxBodyBytes              int64
	DefaultSessionKey         string
	AllowRequestSessionKey    bool
	AllowedSessionKeyPrefixes []string
	AllowedAgentIds           []string // nil = allow all
	Mappings                  []HookMapping
}

// HookMapping defines a single URL-to-action mapping for custom hook endpoints.
type HookMapping struct {
	ID              string
	MatchPath       string
	MatchSource     string
	Action          string // "wake" or "agent"
	WakeMode        string
	Name            string
	AgentID         string
	SessionKey      string
	MessageTemplate string
	TextTemplate    string
	Deliver         *bool
	Channel         string
	To              string
	Model           string
	Thinking        string
	TimeoutSeconds  *int
}

// ───────────────────────────────────────────────────────────────────────
// Payload types (JSON wire format).
// ───────────────────────────────────────────────────────────────────────

// HookWakePayload is the request body for /hooks/wake.
type HookWakePayload struct {
	Text string `json:"text"`
	Mode string `json:"mode,omitempty"` // "now" or "next-heartbeat"
}

// HookAgentPayload is the request body for /hooks/agent.
type HookAgentPayload struct {
	Message        string `json:"message"`
	Name           string `json:"name,omitempty"`
	AgentID        string `json:"agentId,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	SessionKey     string `json:"sessionKey,omitempty"`
	Deliver        *bool  `json:"deliver,omitempty"`
	Channel        string `json:"channel,omitempty"`
	To             string `json:"to,omitempty"`
	Model          string `json:"model,omitempty"`
	Thinking       string `json:"thinking,omitempty"`
	TimeoutSeconds *int   `json:"timeoutSeconds,omitempty"`
}

// HookAgentDispatchPayload is the resolved payload passed to DispatchAgent.
type HookAgentDispatchPayload struct {
	Message        string
	Name           string
	AgentID        string
	IdempotencyKey string
	SessionKey     string
	Deliver        *bool
	Channel        string
	To             string
	Model          string
	Thinking       string
	TimeoutSeconds *int
}

// ───────────────────────────────────────────────────────────────────────
// Dispatcher callbacks.
// ───────────────────────────────────────────────────────────────────────

// HookDispatchers provides the callbacks that the hook handler invokes.
type HookDispatchers struct {
	// DispatchWake fires a wake event (text + mode).
	DispatchWake func(text string, mode string)
	// DispatchAgent starts an agent job and returns a runId.
	DispatchAgent func(payload HookAgentDispatchPayload) string
}

// ───────────────────────────────────────────────────────────────────────
// Rate limiter (simple per-IP sliding window for hook auth failures).
// ───────────────────────────────────────────────────────────────────────

type hookAuthRateLimiter struct {
	mu          sync.Mutex
	failures    map[string]*hookIPFailures
	maxFailures int
	windowMs    int64
}

type hookIPFailures struct {
	count   int
	firstAt int64 // unix ms
}

func newHookAuthRateLimiter(maxFailures int, windowMs int64) *hookAuthRateLimiter {
	return &hookAuthRateLimiter{
		failures:    make(map[string]*hookIPFailures),
		maxFailures: maxFailures,
		windowMs:    windowMs,
	}
}

// check returns true if the IP is rate-limited (should be rejected).
func (rl *hookAuthRateLimiter) check(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	f, ok := rl.failures[ip]
	if !ok {
		return false
	}
	now := time.Now().UnixMilli()
	// Window expired — reset.
	if now-f.firstAt > rl.windowMs {
		delete(rl.failures, ip)
		return false
	}
	return f.count >= rl.maxFailures
}

// recordFailure increments the failure count for the given IP.
func (rl *hookAuthRateLimiter) recordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now().UnixMilli()
	f, ok := rl.failures[ip]
	if !ok {
		rl.failures[ip] = &hookIPFailures{count: 1, firstAt: now}
		return
	}
	// Window expired — reset and start fresh.
	if now-f.firstAt > rl.windowMs {
		f.count = 1
		f.firstAt = now
		return
	}
	f.count++
}

// reset clears the failure record for the given IP (on successful auth).
func (rl *hookAuthRateLimiter) reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.failures, ip)
}

// ───────────────────────────────────────────────────────────────────────
// Replay cache (idempotency).
// ───────────────────────────────────────────────────────────────────────

type hookReplayEntry struct {
	ts    time.Time
	runID string
}

type hookReplayCache struct {
	mu      sync.Mutex
	entries map[string]*hookReplayEntry
	ttl     time.Duration
	maxSize int
}

func newHookReplayCache(ttl time.Duration, maxSize int) *hookReplayCache {
	return &hookReplayCache{
		entries: make(map[string]*hookReplayEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// buildKey creates a SHA256-based cache key from token, scope, and idempotency key.
// Returns empty string if idempotencyKey is empty.
func (rc *hookReplayCache) buildKey(token, scope, idempotencyKey string) string {
	idem := strings.TrimSpace(idempotencyKey)
	if idem == "" || len(idem) > maxIdempotencyKeyLen {
		return ""
	}
	tokenHash := sha256Hex(token)
	scopeHash := sha256Hex(scope)
	idemHash := sha256Hex(idem)
	return tokenHash + ":" + scopeHash + ":" + idemHash
}

// get returns the cached runId if present and not expired.
func (rc *hookReplayCache) get(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.pruneUnsafe()
	entry, ok := rc.entries[key]
	if !ok {
		return "", false
	}
	// Touch: move to "end" by refreshing.
	entry.ts = time.Now()
	return entry.runID, true
}

// set stores a runId in the cache.
func (rc *hookReplayCache) set(key, runID string) {
	if key == "" {
		return
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.entries[key] = &hookReplayEntry{ts: time.Now(), runID: runID}
	rc.pruneUnsafe()
}

// pruneUnsafe removes expired entries and enforces max size. Must be called with mu held.
func (rc *hookReplayCache) pruneUnsafe() {
	cutoff := time.Now().Add(-rc.ttl)
	for k, e := range rc.entries {
		if e.ts.Before(cutoff) {
			delete(rc.entries, k)
		}
	}
	// Evict oldest if over capacity.
	for len(rc.entries) > rc.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, e := range rc.entries {
			if oldestKey == "" || e.ts.Before(oldestTime) {
				oldestKey = k
				oldestTime = e.ts
			}
		}
		if oldestKey == "" {
			break
		}
		delete(rc.entries, oldestKey)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Main handler.
// ───────────────────────────────────────────────────────────────────────

// HooksHTTPHandler handles webhook HTTP requests at the configured base path.
type HooksHTTPHandler struct {
	config      *HooksHTTPConfig
	dispatchers HookDispatchers
	rateLimiter *hookAuthRateLimiter
	replayCache *hookReplayCache
	logger      *slog.Logger
}

// NewHooksHTTPHandler creates a new webhook HTTP handler.
func NewHooksHTTPHandler(cfg *HooksHTTPConfig, dispatchers HookDispatchers, logger *slog.Logger) *HooksHTTPHandler {
	if cfg.BasePath == "" {
		cfg.BasePath = defaultHooksBasePath
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = defaultHooksMaxBodyBytes
	}
	return &HooksHTTPHandler{
		config:      cfg,
		dispatchers: dispatchers,
		rateLimiter: newHookAuthRateLimiter(hookAuthFailureLimit, hookAuthFailureWindowMs),
		replayCache: newHookReplayCache(hookReplayCacheTTL, hookReplayCacheMax),
		logger:      logger,
	}
}

// Handle processes an HTTP request. Returns true if the request was handled
// (path matched hooks base path), false if the request should be passed to the
// next handler. This mirrors the TypeScript HooksRequestHandler signature.
func (h *HooksHTTPHandler) Handle(w http.ResponseWriter, r *http.Request) bool {
	basePath := h.config.BasePath

	// ── Path matching ──────────────────────────────────────────────
	path := r.URL.Path
	if path != basePath && !strings.HasPrefix(path, basePath+"/") {
		return false
	}

	// ── Reject query-param tokens ──────────────────────────────────
	if r.URL.Query().Has("token") {
		writeText(w, http.StatusBadRequest,
			"Hook token must be provided via Authorization: Bearer <token> or X-Deneb-Token header (query parameters are not allowed).")
		return true
	}

	// ── Method enforcement ─────────────────────────────────────────
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeText(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return true
	}

	// ── Token auth ─────────────────────────────────────────────────
	token := extractToken(r)
	clientIP := resolveClientIP(r)
	if !constantTimeEqual(token, h.config.Token) {
		// Check rate limit before recording the failure.
		if h.rateLimiter.check(clientIP) {
			w.Header().Set("Retry-After", "60")
			writeText(w, http.StatusTooManyRequests, "Too Many Requests")
			h.logger.Warn("hook auth throttled", "ip", clientIP)
			return true
		}
		h.rateLimiter.recordFailure(clientIP)
		writeText(w, http.StatusUnauthorized, "Unauthorized")
		return true
	}
	// Successful auth — reset failure counter.
	h.rateLimiter.reset(clientIP)

	// ── Sub-path resolution ────────────────────────────────────────
	subPath := strings.TrimPrefix(path, basePath)
	subPath = strings.TrimLeft(subPath, "/")
	if subPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "Not Found"})
		return true
	}

	// ── Body parsing ───────────────────────────────────────────────
	body, err := readJSONBody(r, h.config.MaxBodyBytes)
	if err != nil {
		status := http.StatusBadRequest
		msg := err.Error()
		if msg == "payload too large" {
			status = http.StatusRequestEntityTooLarge
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": msg})
		return true
	}

	// ── /hooks/wake ────────────────────────────────────────────────
	if subPath == "wake" {
		h.handleWake(w, body)
		return true
	}

	// ── /hooks/agent ───────────────────────────────────────────────
	if subPath == "agent" {
		h.handleAgent(w, body, token)
		return true
	}

	// ── Custom mappings ────────────────────────────────────────────
	if len(h.config.Mappings) > 0 {
		if h.handleMapping(w, subPath, body, token) {
			return true
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown hook endpoint"})
	return true
}

// ───────────────────────────────────────────────────────────────────────
// Sub-handlers.
// ───────────────────────────────────────────────────────────────────────

func (h *HooksHTTPHandler) handleWake(w http.ResponseWriter, body map[string]any) {
	text, _ := body["text"].(string)
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

func (h *HooksHTTPHandler) handleAgent(w http.ResponseWriter, body map[string]any, token string) {
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

	// Idempotency check.
	scopeJSON, _ := json.Marshal(map[string]any{
		"pathKey":    "agent",
		"agentId":    agentID,
		"sessionKey": sessionKey,
		"message":    payload.Message,
		"name":       payload.Name,
	})
	replayKey := h.replayCache.buildKey(token, string(scopeJSON), payload.IdempotencyKey)
	if cachedRunID, ok := h.replayCache.get(replayKey); ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": cachedRunID})
		return
	}

	runID := h.dispatchers.DispatchAgent(HookAgentDispatchPayload{
		Message:        payload.Message,
		Name:           payload.Name,
		AgentID:        agentID,
		IdempotencyKey: payload.IdempotencyKey,
		SessionKey:     sessionKey,
		Deliver:        payload.Deliver,
		Channel:        payload.Channel,
		To:             payload.To,
		Model:          payload.Model,
		Thinking:       payload.Thinking,
		TimeoutSeconds: payload.TimeoutSeconds,
	})

	h.replayCache.set(replayKey, runID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": runID})
}

// handleMapping tries to match a subPath against configured mappings.
// Returns true if a mapping matched and the response was written.
func (h *HooksHTTPHandler) handleMapping(w http.ResponseWriter, subPath string, body map[string]any, token string) bool {
	for _, m := range h.config.Mappings {
		if !matchesMapping(m, subPath) {
			continue
		}

		switch m.Action {
		case "wake":
			text := resolveTemplate(m.TextTemplate, body)
			mode := m.WakeMode
			if mode == "" {
				mode = "now"
			}
			h.dispatchers.DispatchWake(text, mode)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mode": mode})
			return true

		case "agent":
			message := resolveTemplate(m.MessageTemplate, body)
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
			idempotencyKey, _ := body["idempotencyKey"].(string)

			scopeJSON, _ := json.Marshal(map[string]any{
				"pathKey":    subPath,
				"agentId":    agentID,
				"sessionKey": sessionKey,
				"message":    message,
				"name":       m.Name,
			})
			replayKey := h.replayCache.buildKey(token, string(scopeJSON), idempotencyKey)
			if cachedRunID, ok := h.replayCache.get(replayKey); ok {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "runId": cachedRunID})
				return true
			}

			runID := h.dispatchers.DispatchAgent(HookAgentDispatchPayload{
				Message:        message,
				Name:           m.Name,
				AgentID:        agentID,
				IdempotencyKey: idempotencyKey,
				SessionKey:     sessionKey,
				Deliver:        m.Deliver,
				Channel:        m.Channel,
				To:             m.To,
				Model:          m.Model,
				Thinking:       m.Thinking,
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
// nil AllowedAgentIds means all agents are allowed.
func (h *HooksHTTPHandler) isAgentAllowed(agentID string) bool {
	if h.config.AllowedAgentIds == nil {
		return true
	}
	if agentID == "" {
		// Empty = default agent; always allowed.
		return true
	}
	for _, id := range h.config.AllowedAgentIds {
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
	_ = json.NewEncoder(w).Encode(body)
}

// writeText writes a plain text response.
func writeText(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, msg)
}

// sha256Hex returns the hex-encoded SHA256 of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// generateUUID generates a random UUID v4.
func generateUUID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// matchesMapping checks if a subPath matches a mapping's criteria.
func matchesMapping(m HookMapping, subPath string) bool {
	if m.MatchPath != "" {
		// Normalize: strip leading slash from both for comparison.
		normalized := strings.TrimLeft(m.MatchPath, "/")
		return normalized == subPath
	}
	// Source-based matching: subPath must contain the source identifier.
	if m.MatchSource != "" {
		return strings.Contains(subPath, m.MatchSource)
	}
	return false
}

// resolveTemplate applies a simple template by replacing {{key}} placeholders
// with values from the payload. This is intentionally simple — complex
// transformations should be handled by the caller/dispatcher.
func resolveTemplate(tmpl string, payload map[string]any) string {
	if tmpl == "" {
		return ""
	}
	result := tmpl
	for key, val := range payload {
		placeholder := "{{" + key + "}}"
		if strings.Contains(result, placeholder) {
			result = strings.ReplaceAll(result, placeholder, fmt.Sprintf("%v", val))
		}
	}
	return result
}
