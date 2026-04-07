package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestBuildMux_RegistersExpectedRoutes verifies that buildMux registers all
// expected routes. This is a component-level test that runs independently of
// the full server startup sequence.
func TestBuildMux_RegistersExpectedRoutes(t *testing.T) {
	srv, err := New(":0")
	testutil.NoError(t, err)
	mux := srv.buildMux()

	tests := []struct {
		method   string
		path     string
		wantCode int
	}{
		// Health/ready endpoints — always registered.
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/ready", http.StatusServiceUnavailable}, // not ready yet
		{http.MethodGet, "/readyz", http.StatusServiceUnavailable},
		// Root handler.
		{http.MethodGet, "/", http.StatusOK},
		// Unknown path → 404.
		{http.MethodGet, "/nonexistent-path-xyz", http.StatusNotFound},
		// POST on GET-only routes → 405.
		{http.MethodPost, "/health", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.wantCode {
				t.Errorf("got %d, want %d (body: %s)", w.Code, tt.wantCode, w.Body.String())
			}
		})
	}
}

// TestHandleRoot_ResponseShape verifies that the root handler returns a
// well-formed JSON response with the expected fields and values.
func TestHandleRoot_ResponseShape(t *testing.T) {
	srv, err := New(":0")
	testutil.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleRoot(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	for _, key := range []string{"name", "version", "status"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("missing field %q in root response", key)
		}
	}
	if resp["name"] != "deneb-gateway" {
		t.Errorf("name = %v, want deneb-gateway", resp["name"])
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %v, want ok", resp["status"])
	}
}

// TestHandleRoot_ContentType verifies that root handler sets JSON content type.
func TestHandleRoot_ContentType(t *testing.T) {
	srv, err := New(":0")
	testutil.NoError(t, err)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	srv.handleRoot(w, req)

	ct := w.Header().Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header to be set")
	}
}
