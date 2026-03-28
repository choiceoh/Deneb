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
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ───────────────────────────────────────────────────────────────────────
// Defaults (matching TypeScript constants).
// ───────────────────────────────────────────────────────────────────────

const (
	defaultHooksBasePath     = "/hooks"
	defaultHooksMaxBodyBytes = 256 * 1024 // 256 KB
	hookAuthFailureLimit     = 20
	hookAuthFailureWindowMs  = 60_000
	hookReplayCacheTTL       = 5 * time.Minute
	hookReplayCacheMax       = 1000
	maxIdempotencyKeyLen     = 256
)

// templateExprRegex matches {{expr}} placeholders in templates.
var templateExprRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// blockedTemplateKeys prevents prototype pollution via template traversal.
var blockedTemplateKeys = map[string]bool{
	"__proto__":   true,
	"prototype":   true,
	"constructor": true,
}

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
	Text   string `json:"text"`
	Mode   string `json:"mode,omitempty"`   // "now" or "next-heartbeat"
	Target string `json:"target,omitempty"` // "autonomous" to trigger autonomous cycle
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
	Message                    string
	Name                       string
	AgentID                    string
	IdempotencyKey             string
	SessionKey                 string
	Deliver                    *bool
	Channel                    string
	To                         string
	Model                      string
	Thinking                   string
	TimeoutSeconds             *int
	WakeMode                   string
	AllowUnsafeExternalContent bool
}

// ───────────────────────────────────────────────────────────────────────
// Template context for enhanced template rendering.
// ───────────────────────────────────────────────────────────────────────

type templateContext struct {
	Payload map[string]any
	Headers map[string]string
	Query   map[string]string
	Path    string
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
	// DispatchAutonomousWake triggers an autonomous cycle (Phase 2).
	DispatchAutonomousWake func(text string)
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

// SetAutonomousWakeDispatcher sets the callback for autonomous wake triggers.
// Called after the autonomous service is initialized.
func (h *HooksHTTPHandler) SetAutonomousWakeDispatcher(fn func(text string)) {
	h.dispatchers.DispatchAutonomousWake = fn
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
		// #15: Plain text 404 for consistency with TS implementation.
		writeText(w, http.StatusNotFound, "Not Found")
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

	// Build template context from request for use by sub-handlers.
	tmplCtx := buildTemplateContext(r, subPath, body)

	// ── /hooks/wake ────────────────────────────────────────────────
	if subPath == "wake" {
		h.handleWake(w, body)
		return true
	}

	// ── /hooks/agent ───────────────────────────────────────────────
	if subPath == "agent" {
		h.handleAgent(w, r, body, token)
		return true
	}

	// ── Custom mappings ────────────────────────────────────────────
	if len(h.config.Mappings) > 0 {
		if h.handleMapping(w, r, subPath, body, token, tmplCtx) {
			return true
		}
	}

	// #15: Plain text 404 for consistency with TS implementation.
	writeText(w, http.StatusNotFound, "unknown hook endpoint")
	return true
}

