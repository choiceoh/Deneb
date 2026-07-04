package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestWithCORS_Preflight verifies a browser CORS preflight (OPTIONS + Origin) is
// answered 204 with the headers a cross-origin client needs — crucially BEFORE
// the mux/auth, since the preflight carries no client token and would otherwise
// be rejected as unauthenticated.
func TestWithCORS_Preflight(t *testing.T) {
	h := withCORS(testutil.Must(New(":0")).buildMux())

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/miniapp/rpc", nil)
	req.Header.Set("Origin", "http://localhost:1420")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", clientauth.Header)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight → %d, want 204 (body: %s)", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:1420" {
		t.Errorf("Allow-Origin = %q, want echoed origin", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, clientauth.Header) {
		t.Errorf("Allow-Headers = %q, must include %s", got, clientauth.Header)
	}
}

// TestWithCORS_ActualMiniappRequestEchoesOrigin verifies a normal cross-origin
// request on the browser-facing surface passes through with CORS headers.
func TestWithCORS_ActualMiniappRequestEchoesOrigin(t *testing.T) {
	h := withCORS(testutil.Must(New(":0")).buildMux())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/miniapp/rpc", strings.NewReader(`{}`))
	req.Header.Set("Origin", "http://localhost:1420")
	req.Header.Set(clientauth.Header, "invalid")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/v1/miniapp/rpc → %d, want 401 (should pass through to auth)", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:1420" {
		t.Errorf("Allow-Origin = %q, want echoed origin", got)
	}
}

// TestWithCORS_LoopbackOnlyRoutesDoNotOptIn ensures loopback/admin endpoints do
// not become browser-readable or preflightable just because the server as a
// whole supports browser access for the miniapp surface.
func TestWithCORS_LoopbackOnlyRoutesDoNotOptIn(t *testing.T) {
	h := withCORS(testutil.Must(New(":0")).buildMux())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:1420")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /health → %d, want 200", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for loopback-only route", got)
	}
}

func TestWithCORS_PreflightRejectedOnLoopbackOnlyRoute(t *testing.T) {
	h := withCORS(testutil.Must(New(":0")).buildMux())

	req := httptest.NewRequest(http.MethodOptions, "/api/event/ingest", nil)
	req.Header.Set("Origin", "http://localhost:1420")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusNoContent {
		t.Fatalf("OPTIONS /api/event/ingest unexpectedly succeeded as a CORS preflight")
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for loopback-only route", got)
	}
}

// TestWithCORS_NoOriginUnaffected verifies native (Origin-less) clients are left
// untouched: no CORS headers, and an OPTIONS is NOT short-circuited.
func TestWithCORS_NoOriginUnaffected(t *testing.T) {
	h := withCORS(testutil.Must(New(":0")).buildMux())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for an Origin-less request", got)
	}
}
