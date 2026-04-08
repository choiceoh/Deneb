package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestBuildMux_RegistersExpectedRoutes verifies that buildMux registers all
// expected routes. This is a component-level test that runs independently of
// the full server startup sequence.
func TestBuildMux_RegistersExpectedRoutes(t *testing.T) {
	srv := testutil.Must(New(":0"))
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

// TestHandleRoot_ContentType verifies that root handler sets JSON content type.
