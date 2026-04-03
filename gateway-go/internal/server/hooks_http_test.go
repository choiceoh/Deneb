package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// testHooksHandler creates a HooksHTTPHandler with sensible test defaults.
func testHooksHandler(opts ...func(*HooksHTTPConfig)) (*HooksHTTPHandler, *hookTestDispatchers) {
	dispatchers := &hookTestDispatchers{
		wakeEvents: make([]hookTestWakeEvent, 0),
		agentJobs:  make([]HookAgentDispatchPayload, 0),
	}
	cfg := &HooksHTTPConfig{
		BasePath:     "/hooks",
		Token:        "test-secret-token",
		MaxBodyBytes: 256 * 1024,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	h := NewHooksHTTPHandler(cfg, HookDispatchers{
		DispatchWake: func(text, mode string) {
			dispatchers.mu.Lock()
			defer dispatchers.mu.Unlock()
			dispatchers.wakeEvents = append(dispatchers.wakeEvents, hookTestWakeEvent{text, mode})
		},
		DispatchAgent: func(payload HookAgentDispatchPayload) string {
			dispatchers.mu.Lock()
			defer dispatchers.mu.Unlock()
			dispatchers.agentJobs = append(dispatchers.agentJobs, payload)
			dispatchers.lastRunID = "run-" + payload.Message
			return dispatchers.lastRunID
		},
	}, nil) // nil logger uses default
	// Replace nil logger with a noop.
	if h.logger == nil {
		h.logger = discardLogger()
	}
	return h, dispatchers
}

type hookTestWakeEvent struct {
	text string
	mode string
}

type hookTestDispatchers struct {
	mu         sync.Mutex
	wakeEvents []hookTestWakeEvent
	agentJobs  []HookAgentDispatchPayload
	lastRunID  string
}

func discardLogger() *slog.Logger {
	return slog.Default()
}

// ─── Test: Non-hooks path returns false ────────────────────────────────

func TestHooksHTTP_NonHooksPath(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rpc", nil)
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if handled {
		t.Error("expected non-hooks path to return false (not handled)")
	}
}

// ─── Test: GET method returns 405 ──────────────────────────────────────

func TestHooksHTTP_GetMethodReturns405(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodGet, "/hooks/wake", nil)
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected hooks path to be handled")
	}
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
	if w.Header().Get("Allow") != "POST" {
		t.Errorf("expected Allow: POST header")
	}
}

// ─── Test: Wake endpoint with valid payload ────────────────────────────

func TestHooksHTTP_WakeEndpoint(t *testing.T) {
	h, dispatchers := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake",
		strings.NewReader(`{"text":"hello world","mode":"now"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected handled")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Error("expected ok=true")
	}
	if resp["mode"] != "now" {
		t.Errorf("expected mode=now, got %v", resp["mode"])
	}

	dispatchers.mu.Lock()
	defer dispatchers.mu.Unlock()
	if len(dispatchers.wakeEvents) != 1 {
		t.Fatalf("expected 1 wake event, got %d", len(dispatchers.wakeEvents))
	}
	if dispatchers.wakeEvents[0].text != "hello world" {
		t.Errorf("expected text 'hello world', got %q", dispatchers.wakeEvents[0].text)
	}
}

// ─── Test: Wake with default mode ──────────────────────────────────────

func TestHooksHTTP_WakeDefaultMode(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake",
		strings.NewReader(`{"text":"test"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["mode"] != "now" {
		t.Errorf("expected default mode=now, got %v", resp["mode"])
	}
}

// ─── Test: Wake with invalid mode ──────────────────────────────────────

func TestHooksHTTP_WakeInvalidMode(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake",
		strings.NewReader(`{"text":"test","mode":"invalid"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── Test: Agent endpoint with valid payload ───────────────────────────

func TestHooksHTTP_AgentEndpoint(t *testing.T) {
	h, dispatchers := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"run task","name":"my-job"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected handled")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["ok"] != true {
		t.Error("expected ok=true")
	}
	runID, ok := resp["runId"].(string)
	if !ok || runID == "" {
		t.Error("expected non-empty runId")
	}

	dispatchers.mu.Lock()
	defer dispatchers.mu.Unlock()
	if len(dispatchers.agentJobs) != 1 {
		t.Fatalf("expected 1 agent job, got %d", len(dispatchers.agentJobs))
	}
	if dispatchers.agentJobs[0].Message != "run task" {
		t.Errorf("expected message 'run task', got %q", dispatchers.agentJobs[0].Message)
	}
}

// ─── Test: Agent missing message returns 400 ───────────────────────────

func TestHooksHTTP_AgentMissingMessage(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"name":"my-job"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── Test: Idempotency — same request twice returns same runId ─────────

func TestHooksHTTP_Idempotency(t *testing.T) {
	h, _ := testHooksHandler()
	body := `{"message":"run task","idempotencyKey":"unique-123"}`

	// First request.
	req1 := httptest.NewRequest(http.MethodPost, "/hooks/agent", strings.NewReader(body))
	req1.Header.Set("Authorization", "Bearer test-secret-token")
	w1 := httptest.NewRecorder()
	h.Handle(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}
	var resp1 map[string]any
	_ = json.NewDecoder(w1.Body).Decode(&resp1)
	runID1 := resp1["runId"].(string)

	// Second request with same idempotency key.
	req2 := httptest.NewRequest(http.MethodPost, "/hooks/agent", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer test-secret-token")
	w2 := httptest.NewRecorder()
	h.Handle(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", w2.Code)
	}
	var resp2 map[string]any
	_ = json.NewDecoder(w2.Body).Decode(&resp2)
	runID2 := resp2["runId"].(string)

	if runID1 != runID2 {
		t.Errorf("expected same runId for idempotent request, got %q and %q", runID1, runID2)
	}
}

// ─── Test: Max body size enforcement ───────────────────────────────────

func TestHooksHTTP_MaxBodySizeReturns413(t *testing.T) {
	h, _ := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.MaxBodyBytes = 32 // Very small limit for testing.
	})
	largeBody := `{"message":"` + strings.Repeat("x", 100) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent", strings.NewReader(largeBody))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", w.Code)
	}
}

// ─── Test: Empty sub-path returns 404 ──────────────────────────────────

func TestHooksHTTP_EmptySubpathReturns404(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ─── Test: Unknown hook endpoint returns 404 ───────────────────────────

func TestHooksHTTP_UnknownEndpointReturns404(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/nonexistent", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// ─── Test: Agent policy check ──────────────────────────────────────────

func TestHooksHTTP_AgentPolicyRejectsDisallowedAgent(t *testing.T) {
	h, _ := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.AllowedAgentIds = []string{"agent-a", "agent-b"}
	})
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"test","agentId":"agent-c"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── Test: Agent policy allows empty agentId (default) ─────────────────

func TestHooksHTTP_AgentPolicyAllowsDefaultAgent(t *testing.T) {
	h, _ := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.AllowedAgentIds = []string{"agent-a"}
	})
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"test"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for default agent, got %d", w.Code)
	}
}

// ─── Test: Custom mapping — wake action ────────────────────────────────

func TestHooksHTTP_CustomMappingWake(t *testing.T) {
	h, dispatchers := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.Mappings = []HookMapping{
			{
				ID:           "github-push",
				MatchPath:    "github",
				Action:       "wake",
				WakeMode:     "next-heartbeat",
				TextTemplate: "Push from {{repo}}",
			},
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/hooks/github",
		strings.NewReader(`{"repo":"deneb/deneb"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["mode"] != "next-heartbeat" {
		t.Errorf("expected mode=next-heartbeat, got %v", resp["mode"])
	}

	dispatchers.mu.Lock()
	defer dispatchers.mu.Unlock()
	if len(dispatchers.wakeEvents) != 1 {
		t.Fatalf("expected 1 wake event, got %d", len(dispatchers.wakeEvents))
	}
	if dispatchers.wakeEvents[0].text != "Push from deneb/deneb" {
		t.Errorf("expected templated text, got %q", dispatchers.wakeEvents[0].text)
	}
}

// ─── Test: Custom mapping — agent action ───────────────────────────────

func TestHooksHTTP_CustomMappingAgent(t *testing.T) {
	h, dispatchers := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.Mappings = []HookMapping{
			{
				ID:              "ci-deploy",
				MatchPath:       "deploy",
				Action:          "agent",
				Name:            "deploy-job",
				MessageTemplate: "Deploy {{service}}",
			},
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/hooks/deploy",
		strings.NewReader(`{"service":"gateway"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	dispatchers.mu.Lock()
	defer dispatchers.mu.Unlock()
	if len(dispatchers.agentJobs) != 1 {
		t.Fatalf("expected 1 agent job, got %d", len(dispatchers.agentJobs))
	}
	if dispatchers.agentJobs[0].Message != "Deploy gateway" {
		t.Errorf("expected 'Deploy gateway', got %q", dispatchers.agentJobs[0].Message)
	}
}

// ─── Test: Replay cache key generation ─────────────────────────────────

func TestHooksHTTP_ReplayCacheKeyGeneration(t *testing.T) {
	rc := newHookReplayCache(hookReplayCacheTTL, hookReplayCacheMax)

	// Empty idempotency key → empty cache key.
	key := rc.buildKey("token", "scope", "")
	if key != "" {
		t.Errorf("expected empty key for empty idempotency, got %q", key)
	}

	// Non-empty produces a deterministic key.
	key1 := rc.buildKey("token", "scope", "idem-1")
	key2 := rc.buildKey("token", "scope", "idem-1")
	if key1 != key2 {
		t.Error("expected same key for same inputs")
	}

	// Different inputs produce different keys.
	key3 := rc.buildKey("token", "scope", "idem-2")
	if key1 == key3 {
		t.Error("expected different keys for different idempotency values")
	}
}

// ─── Test: UUID generation format ──────────────────────────────────────

func TestGenerateUUID(t *testing.T) {
	id := generateUUID()
	if len(id) != 36 {
		t.Errorf("expected 36-char UUID, got %d chars: %q", len(id), id)
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Errorf("expected 5 UUID segments, got %d", len(parts))
	}
}

// ─── Test: resolveClientIP ─────────────────────────────────────────────

func TestResolveClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		want       string
	}{
		{"plain IPv4", "1.2.3.4:8080", "", "1.2.3.4"},
		{"XFF present", "1.2.3.4:8080", "5.6.7.8, 1.2.3.4", "5.6.7.8"},
		{"IPv6 brackets", "[::1]:8080", "", "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			got := resolveClientIP(req)
			if got != tt.want {
				t.Errorf("resolveClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

