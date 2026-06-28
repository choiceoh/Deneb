package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/sparkfleet"
)

func TestFleetPathAllowed(t *testing.T) {
	allowed := [][2]string{
		{http.MethodGet, "/api/state"},
		{http.MethodGet, "/api/recipes"},
		{http.MethodGet, "/api/jobs"},
		{http.MethodGet, "/api/jobs/job-12"},
		{http.MethodGet, "/api/hf/search"},
		{http.MethodPost, "/api/recipes/action"},
		{http.MethodPost, "/api/control"},
		{http.MethodPost, "/api/models/download"},
		{http.MethodPost, "/api/assist/logs"},
		{http.MethodGet, "/api/recipes/qwen36/drift"},
	}
	for _, a := range allowed {
		if !fleetPathAllowed(a[0], a[1]) {
			t.Errorf("%s %s should be allowed", a[0], a[1])
		}
	}
	denied := [][2]string{
		{http.MethodPost, "/api/recipes/save"},   // recipe editing = arbitrary command execution
		{http.MethodPost, "/api/recipes/delete"}, // dashboard-only
		{http.MethodGet, "/api/jobs/"},           // empty id
		{http.MethodGet, "/api/jobs/x/y"},        // nested path
		{http.MethodGet, "/api/recipes/x/raw"},
		{http.MethodGet, "/api/recipes/a/b/drift"}, // nested name
		{http.MethodPost, "/api/state"},            // wrong method
		{http.MethodDelete, "/api/jobs"},           // unsupported method
		{http.MethodGet, "/healthz"},
	}
	for _, d := range denied {
		if fleetPathAllowed(d[0], d[1]) {
			t.Errorf("%s %s must be denied", d[0], d[1])
		}
	}
}

// fleetTestServer is a minimal Server wired to a stub SparkFleet upstream.
func fleetTestServer(upstreamURL string) *Server {
	return &Server{
		logger: slog.Default(),
		fleet:  sparkfleet.New(upstreamURL, slog.Default()),
	}
}

func TestFleetProxyForwards(t *testing.T) {
	var gotPath, gotQuery, gotBody, gotCT string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jobId":"job-7"}`))
	}))
	defer upstream.Close()
	s := fleetTestServer(upstream.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/api/recipes/action?x=1",
		strings.NewReader(`{"recipe":"qwen36","action":"launch"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.fleetProxy(w, req)

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "job-7") {
		t.Fatalf("proxy response: %d %s", w.Code, w.Body.String())
	}
	if gotPath != "/api/recipes/action" || gotQuery != "x=1" {
		t.Errorf("upstream saw %s?%s", gotPath, gotQuery)
	}
	if !strings.Contains(gotBody, "qwen36") || gotCT != "application/json" {
		t.Errorf("body/content-type not forwarded: %q %q", gotBody, gotCT)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("response content type not relayed: %q", ct)
	}
}

func TestFleetProxyDeniesUnlistedPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for a denied path")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	s := fleetTestServer(upstream.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/fleet/api/recipes/save", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	s.fleetProxy(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("recipe save through the proxy: got %d want 403", w.Code)
	}
}

func TestFleetProxyOffWithoutURL(t *testing.T) {
	s := &Server{logger: slog.Default()} // fleet nil — integration disabled
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil)
	w := httptest.NewRecorder()
	s.fleetProxy(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("disabled integration: got %d want 503", w.Code)
	}
}

// The routed handler (auth wrapper) refuses without a client token.
func TestFleetProxyRequiresToken(t *testing.T) {
	s := fleetTestServer("http://127.0.0.1:1")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/fleet/api/state", nil)
	w := httptest.NewRecorder()
	s.handleFleetProxy(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing token: got %d want 401", w.Code)
	}
}

func TestFleetPathAllowedJobCancel(t *testing.T) {
	if !fleetPathAllowed(http.MethodPost, "/api/jobs/job-12/cancel") {
		t.Error("job cancel must be allowed")
	}
	for _, bad := range []string{"/api/jobs//cancel", "/api/jobs/a/b/cancel", "/api/jobs/cancel"} {
		if fleetPathAllowed(http.MethodPost, bad) {
			t.Errorf("%s must be denied", bad)
		}
	}
}

// The fleet webhook relays SparkFleet's generic alerts to connected clients,
// loopback-only.
func TestFleetHook(t *testing.T) {
	s := &Server{logger: slog.Default(), pushHub: newClientPushHub()}
	ch, unsub := s.pushHub.subscribe(kindMobile)
	defer unsub()

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/fleet",
		strings.NewReader(`{"source":"sparkfleet","level":"bad","title":"node down: srv3","message":"ssh unreachable"}`))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleFleetHook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("hook: %d %s", w.Code, w.Body.String())
	}
	select {
	case ev := <-ch:
		if !strings.Contains(ev.Title, "플릿") || !strings.Contains(ev.Title, "node down: srv3") || ev.Body != "ssh unreachable" {
			t.Errorf("unexpected push frame: %+v", ev)
		}
		if ev.Kind != pushKindFleet {
			t.Errorf("fleet push Kind = %q, want %q", ev.Kind, pushKindFleet)
		}
	default:
		t.Fatal("no push frame published")
	}

	// Non-loopback callers are refused (SparkFleet posts from this host).
	req = httptest.NewRequest(http.MethodPost, "/api/hooks/fleet", strings.NewReader(`{"title":"x"}`))
	req.RemoteAddr = "100.105.145.6:5555"
	w = httptest.NewRecorder()
	s.handleFleetHook(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("non-loopback: got %d want 403", w.Code)
	}
}
