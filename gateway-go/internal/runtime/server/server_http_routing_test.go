package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
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
		// GET on POST-only cron run → 405.
		{http.MethodGet, "/api/cron/run", http.StatusMethodNotAllowed},
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

func TestBuildMux_PprofOnlyOnLoopback(t *testing.T) {
	t.Run("loopback bind keeps pprof", func(t *testing.T) {
		srv := testutil.Must(New(":0", WithConfig(&config.GatewayRuntimeConfig{
			BindHost: "127.0.0.1",
		})))
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/pprof/", nil)
		rec := httptest.NewRecorder()

		srv.buildMux().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("network bind hides pprof", func(t *testing.T) {
		srv := testutil.Must(New(":0", WithConfig(&config.GatewayRuntimeConfig{
			BindHost: "0.0.0.0",
		})))
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/pprof/", nil)
		rec := httptest.NewRecorder()

		srv.buildMux().ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

// TestHandleRoot_ResponseShape verifies that the root handler returns a
// well-formed JSON response with the expected fields and values.

// TestHandleRoot_ContentType verifies that root handler sets JSON content type.
