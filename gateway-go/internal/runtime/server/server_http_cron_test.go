package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// installCleanCronService attaches a fresh cron.Service backed by a temp
// store path, so HTTP handler tests don't depend on whether os.UserHomeDir
// was set in the test environment.
func installCleanCronService(t *testing.T, srv *Server) *cron.Service {
	t.Helper()
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	svc := cron.NewService(cron.ServiceConfig{
		StorePath:      storePath,
		DefaultChannel: "telegram",
		Enabled:        true,
	}, nil, srv.logger)
	srv.cronService = svc
	return svc
}

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
	installCleanCronService(t, srv)
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

func TestHandleCronRun_Success(t *testing.T) {
	srv := testutil.Must(New(":0"))
	svc := installCleanCronService(t, srv)
	if err := svc.Add(context.Background(), cron.StoreJob{
		ID:       "job-success",
		Name:     "happy-path",
		Enabled:  true,
		Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  cron.StorePayload{Kind: "agentTurn", Message: "hi"},
	}); err != nil {
		t.Fatalf("svc.Add: %v", err)
	}
	mux := srv.buildMux()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newCronRunRequest(`{"name":"happy-path"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want \"ok\"", body["status"])
	}
	if body["jobId"] != "job-success" {
		t.Errorf("jobId = %v, want \"job-success\"", body["jobId"])
	}
}

func TestHandleCronRun_ServiceUnavailable(t *testing.T) {
	srv := testutil.Must(New(":0"))
	srv.cronService = nil
	mux := srv.buildMux()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newCronRunRequest(`{"name":"anything"}`))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "cron service unavailable" {
		t.Errorf("error = %v, want \"cron service unavailable\"", body["error"])
	}
}

// TestHandleCronRun_CorruptStore guards PR #1630 review feedback: when
// the cron store cannot be parsed, the handler must surface 500 rather
// than translating the load failure into a misleading 404 "job not found".
func TestHandleCronRun_CorruptStore(t *testing.T) {
	srv := testutil.Must(New(":0"))
	storePath := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(storePath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.cronService = cron.NewService(cron.ServiceConfig{
		StorePath:      storePath,
		DefaultChannel: "telegram",
		Enabled:        true,
	}, nil, srv.logger)
	mux := srv.buildMux()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newCronRunRequest(`{"name":"anything"}`))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want %d (body: %s)", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "cron store unavailable" {
		t.Errorf("error = %v, want \"cron store unavailable\"", body["error"])
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
