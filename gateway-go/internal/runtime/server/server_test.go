package server

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestServerHealthEndpointLive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := testutil.Must(New("127.0.0.1:0"))
	addr := testutil.Must(srv.StartAndListen(ctx))
	defer srv.Close(context.Background())

	url := fmt.Sprintf("http://%s/health", addr.String())
	req := testutil.Must(http.NewRequestWithContext(ctx, http.MethodGet, url, nil))
	resp := testutil.Must(http.DefaultClient.Do(req))
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
