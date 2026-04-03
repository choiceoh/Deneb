package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── Test: Missing token returns 401 ───────────────────────────────────

func TestHooksHTTP_MissingTokenReturns401(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{"text":"hi"}`))
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected hooks path to be handled")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ─── Test: Invalid token returns 401 ───────────────────────────────────

func TestHooksHTTP_InvalidTokenReturns401(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected hooks path to be handled")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ─── Test: X-Deneb-Token header auth ───────────────────────────────────

func TestHooksHTTP_XDenebTokenHeader(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("X-Deneb-Token", "test-secret-token")
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected hooks path to be handled")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ─── Test: Query param token rejected ──────────────────────────────────

func TestHooksHTTP_QueryParamTokenRejected(t *testing.T) {
	h, _ := testHooksHandler()
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake?token=test-secret-token", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected hooks path to be handled")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ─── Test: Rate limiting after failures returns 429 ────────────────────

func TestHooksHTTP_RateLimitingReturns429(t *testing.T) {
	h, _ := testHooksHandler()

	// Exhaust the failure limit.
	for i := 0; i < hookAuthFailureLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer wrong")
		req.RemoteAddr = "1.2.3.4:12345"
		w := httptest.NewRecorder()
		h.Handle(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, w.Code)
		}
	}

	// Next attempt should be rate-limited.
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	req.RemoteAddr = "1.2.3.4:12345"
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

// ─── Test: Rate limit is per-IP ────────────────────────────────────────

func TestHooksHTTP_RateLimitPerIP(t *testing.T) {
	h, _ := testHooksHandler()

	// Exhaust limit for 1.2.3.4.
	for i := 0; i < hookAuthFailureLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer wrong")
		req.RemoteAddr = "1.2.3.4:12345"
		w := httptest.NewRecorder()
		h.Handle(w, req)
	}

	// Different IP should not be rate-limited.
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	req.RemoteAddr = "5.6.7.8:12345"
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for different IP, got %d", w.Code)
	}
}

// ─── Test: Session key validation (allowRequestSessionKey=false) ───────

func TestHooksHTTP_SessionKeyRejectedWhenNotAllowed(t *testing.T) {
	h, _ := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.AllowRequestSessionKey = false
	})
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"test","sessionKey":"custom-key"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	errMsg, _ := resp["error"].(string)
	if !strings.Contains(errMsg, "sessionKey") {
		t.Errorf("expected error about sessionKey, got %q", errMsg)
	}
}

// ─── Test: Session key accepted when allowed ───────────────────────────

func TestHooksHTTP_SessionKeyAcceptedWhenAllowed(t *testing.T) {
	h, dispatchers := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.AllowRequestSessionKey = true
	})
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"test","sessionKey":"custom-key"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()

	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	dispatchers.mu.Lock()
	defer dispatchers.mu.Unlock()
	if len(dispatchers.agentJobs) != 1 {
		t.Fatal("expected 1 agent job")
	}
	if dispatchers.agentJobs[0].SessionKey != "custom-key" {
		t.Errorf("expected sessionKey 'custom-key', got %q", dispatchers.agentJobs[0].SessionKey)
	}
}

// ─── Test: Session key prefix validation ───────────────────────────────

func TestHooksHTTP_SessionKeyPrefixValidation(t *testing.T) {
	h, _ := testHooksHandler(func(cfg *HooksHTTPConfig) {
		cfg.AllowRequestSessionKey = true
		cfg.AllowedSessionKeyPrefixes = []string{"hook:", "ci:"}
	})

	// Allowed prefix.
	req := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"test","sessionKey":"hook:abc"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed prefix, got %d", w.Code)
	}

	// Disallowed prefix.
	req2 := httptest.NewRequest(http.MethodPost, "/hooks/agent",
		strings.NewReader(`{"message":"test","sessionKey":"admin:evil"}`))
	req2.Header.Set("Authorization", "Bearer test-secret-token")
	w2 := httptest.NewRecorder()
	h.Handle(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for disallowed prefix, got %d", w2.Code)
	}
}

// ─── Test: Successful auth resets rate limiter ─────────────────────────

func TestHooksHTTP_SuccessfulAuthResetsRateLimit(t *testing.T) {
	h, _ := testHooksHandler()

	// Record some failures.
	for i := 0; i < hookAuthFailureLimit-1; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer wrong")
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		h.Handle(w, req)
	}

	// Successful auth should reset.
	req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{"text":"ok"}`))
	req.Header.Set("Authorization", "Bearer test-secret-token")
	req.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// After reset, failures should start from 0 again.
	for i := 0; i < hookAuthFailureLimit; i++ {
		req := httptest.NewRequest(http.MethodPost, "/hooks/wake", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer wrong")
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		h.Handle(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d after reset: expected 401, got %d", i+1, w.Code)
		}
	}
}

// ─── Test: Constant-time compare ───────────────────────────────────────

func TestConstantTimeEqual(t *testing.T) {
	if !constantTimeEqual("abc", "abc") {
		t.Error("equal strings should match")
	}
	if constantTimeEqual("abc", "def") {
		t.Error("different strings should not match")
	}
	if constantTimeEqual("", "abc") {
		t.Error("empty vs non-empty should not match")
	}
	if !constantTimeEqual("", "") {
		t.Error("two empty strings should match")
	}
}
