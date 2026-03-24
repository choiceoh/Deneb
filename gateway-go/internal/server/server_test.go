package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpoint(t *testing.T) {
	srv := New(":0")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %s", resp["status"])
	}
	if resp["runtime"] != "go" {
		t.Errorf("expected runtime go, got %s", resp["runtime"])
	}
}

func TestRPCEndpoint_ValidRequest(t *testing.T) {
	srv := New(":0")
	body := `{"method":"chat.send","id":"test-1","params":{"text":"hello"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rpc", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleRPC(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if resp["ok"] != true {
		t.Error("expected ok=true")
	}

	payload := resp["payload"].(map[string]any)
	if payload["echo"] != "chat.send" {
		t.Errorf("expected echo=chat.send, got %v", payload["echo"])
	}
}

func TestRPCEndpoint_MissingMethod(t *testing.T) {
	srv := New(":0")
	body := `{"id":"test-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rpc", strings.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleRPC(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRPCEndpoint_WrongHTTPMethod(t *testing.T) {
	srv := New(":0")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/rpc", nil)
	w := httptest.NewRecorder()

	srv.handleRPC(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	srv := New("127.0.0.1:0")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := srv.Run(ctx)
	if err != nil {
		t.Fatalf("server run error: %v", err)
	}
}
