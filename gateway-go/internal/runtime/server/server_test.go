package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestHealthEndpoint(t *testing.T) {
	srv := testutil.Must(New(":0"))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("got %v, want status ok", resp["status"])
	}
	if _, ok := resp["subsystems"]; !ok {
		t.Errorf("expected subsystems field in health response")
	}
}

func TestReadyEndpoint(t *testing.T) {
	srv := testutil.Must(New(":0"))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	srv.handleReady(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", w.Code)
	}

	srv.ready.Store(true)
	w = httptest.NewRecorder()
	srv.handleReady(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	srv := testutil.Must(New("127.0.0.1:0"))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := srv.Run(ctx)
	testutil.NoError(t, err)
}

