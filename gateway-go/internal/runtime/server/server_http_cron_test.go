package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newCronRunRequest(body string) *http.Request {
	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/cron/run",
		strings.NewReader(body),
	)
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}

func TestHandleCronRun_MissingName(t *testing.T) {
	srv := testutil.Must(New(":0"))
	mux := srv.buildMux()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newCronRunRequest(`{}`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestHandleCronRun_InvalidJSON(t *testing.T) {
	srv := testutil.Must(New(":0"))
	mux := srv.buildMux()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newCronRunRequest(`{not json`))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestHandleCronRun_UnknownName(t *testing.T) {
	srv := testutil.Must(New(":0"))
	if srv.cronService == nil {
		t.Skip("cron service not initialized in this environment")
	}
	mux := srv.buildMux()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newCronRunRequest(`{"name":"does-not-exist-xyz"}`))

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusNotFound, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "job not found" {
		t.Errorf("expected 'job not found', got %v", body["error"])
	}
}

func TestHandleCronRun_NonLoopback(t *testing.T) {
	srv := testutil.Must(New(":0"))
	mux := srv.buildMux()

	req := httptest.NewRequestWithContext(
		context.Background(), http.MethodPost, "/api/cron/run",
		strings.NewReader(`{"name":"x"}`),
	)
	req.RemoteAddr = "10.0.0.5:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestIsLoopbackRemote(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:1234", true},
		{"[::1]:1234", true},
		{"10.0.0.5:1234", false},
		{"8.8.8.8:80", false},
		{"", false},
		{"not-an-ip", false},
	}
	for _, c := range cases {
		if got := isLoopbackRemote(c.addr); got != c.want {
			t.Errorf("isLoopbackRemote(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
}
