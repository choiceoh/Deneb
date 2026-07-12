package fleetapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type alertGateStub struct {
	relay bool
	title string
	level string
}

func (g *alertGateStub) ShouldRelay(title, level string, _ time.Time) bool {
	g.title = title
	g.level = level
	return g.relay
}

func TestAlertHookPublishesFleetAlert(t *testing.T) {
	t.Parallel()
	gate := &alertGateStub{relay: true}
	var gotTitle, gotBody string
	hook := NewAlertHook(AlertHookConfig{
		Gate: gate,
		Publish: func(title, body string) {
			gotTitle, gotBody = title, body
		},
		Logger: fleetAlertTestLogger(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/fleet",
		strings.NewReader(`{"source":"sparkfleet","level":"bad","title":"node down: srv3","message":"ssh unreachable"}`))
	req.RemoteAddr = "127.0.0.1:5555"
	recorder := httptest.NewRecorder()

	hook.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if gotTitle != "🔴 플릿 · node down: srv3" || gotBody != "ssh unreachable" {
		t.Fatalf("published alert = (%q, %q)", gotTitle, gotBody)
	}
	if gate.title != "node down: srv3" || gate.level != "bad" {
		t.Fatalf("gate input = (%q, %q)", gate.title, gate.level)
	}
}

func TestAlertHookRejectsBeforePublishing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		remoteAddr string
		body       string
		wantStatus int
	}{
		{name: "non-loopback", remoteAddr: "100.105.145.6:5555", body: `{"title":"x"}`, wantStatus: http.StatusForbidden},
		{name: "invalid JSON", remoteAddr: "127.0.0.1:5555", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "empty alert", remoteAddr: "127.0.0.1:5555", body: `{}`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			published := false
			hook := NewAlertHook(AlertHookConfig{
				Publish: func(string, string) { published = true },
				Logger:  fleetAlertTestLogger(),
			})
			req := httptest.NewRequest(http.MethodPost, "/api/hooks/fleet", strings.NewReader(tt.body))
			req.RemoteAddr = tt.remoteAddr
			recorder := httptest.NewRecorder()
			hook.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if published {
				t.Fatal("rejected request reached publisher")
			}
		})
	}
}

func TestAlertHookReturnsSuppressedSuccess(t *testing.T) {
	t.Parallel()
	published := false
	hook := NewAlertHook(AlertHookConfig{
		Gate:    &alertGateStub{relay: false},
		Publish: func(string, string) { published = true },
		Logger:  fleetAlertTestLogger(),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hooks/fleet",
		strings.NewReader(`{"level":"warn","title":"low memory","message":"still low"}`))
	req.RemoteAddr = "[::1]:5555"
	recorder := httptest.NewRecorder()

	hook.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"suppressed":true`) {
		t.Fatalf("suppressed response = %d %s", recorder.Code, recorder.Body.String())
	}
	if published {
		t.Fatal("suppressed alert reached publisher")
	}
}

func fleetAlertTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
