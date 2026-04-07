package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpoint(t *testing.T) {
	srv, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
	if _, ok := resp["subsystems"]; !ok {
		t.Errorf("expected subsystems field in health response")
	}
}

func TestReadyEndpoint(t *testing.T) {
	srv, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	srv.handleReady(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}

	srv.ready.Store(true)
	w = httptest.NewRecorder()
	srv.handleReady(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRPCEndpoint_ValidRequest(t *testing.T) {
	srv, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"method":"health","id":"test-1"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/rpc", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleRPC(w, req)

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
}

func TestRPCEndpoint_MissingMethod(t *testing.T) {
	srv, err := New(":0")
	if err != nil {
		t.Fatal(err)
	}
	body := `{"id":"test-1"}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/rpc", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleRPC(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err = srv.Run(ctx)
	if err != nil {
		t.Fatalf("server run error: %v", err)
	}
}

func TestServerHealthEndpointLive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/health", addr.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

func TestRPCEndpointLive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/api/v1/rpc", addr.String())

	// Valid health request.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader(`{"method":"health","id":"live-1"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != true {
		t.Errorf("ok = %v, want true", body["ok"])
	}

	// Unknown method returns NOT_FOUND.
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader(`{"method":"nonexistent","id":"live-2"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp2.Body.Close()

	var body2 map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&body2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body2["ok"] != false {
		t.Errorf("ok = %v, want false", body2["ok"])
	}

	// Malformed JSON returns 400.
	req3, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
		strings.NewReader(`{invalid`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req3.Header.Set("Content-Type", "application/json")
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp3.StatusCode)
	}
}

// TestPhase1MethodsReachableViaRPC verifies that Phase 1 RPC methods
// (registered via rpc.RegisterBuiltinMethods) are reachable through the server.
func TestPhase1MethodsReachableViaRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := New("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr, err := srv.StartAndListen(ctx)
	if err != nil {
		t.Fatalf("StartAndListen: %v", err)
	}
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/api/v1/rpc", addr.String())

	// Test a selection of Phase 1 methods that should be registered.
	methods := []struct {
		method string
		body   string
	}{
		{"health.check", `{"method":"health.check","id":"p1"}`},
		{"system.info", `{"method":"system.info","id":"p2"}`},
		{"sessions.list", `{"method":"sessions.list","id":"p3"}`},
		{"telegram.status", `{"method":"telegram.status","id":"p4"}`},
		{"telegram.health", `{"method":"telegram.health","id":"p5"}`},
		{"security.is_safe_url", `{"method":"security.is_safe_url","id":"p6","params":{"url":"https://example.com"}}`},
		// data is base64-encoded PNG header: 0x89 0x50 0x4E 0x47 0x0D 0x0A 0x1A 0x0A
		{"media.detect_mime", `{"method":"media.detect_mime","id":"p7","params":{"data":"iVBORw0KGgo="}}`},
	}

	for _, tc := range methods {
		t.Run(tc.method, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("new request %s: %v", tc.method, err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST %s: %v", tc.method, err)
			}
			defer resp.Body.Close()

			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["ok"] != true {
				t.Errorf("%s: expected ok=true, got %v (error: %v)", tc.method, body["ok"], body["error"])
			}
		})
	}
}
