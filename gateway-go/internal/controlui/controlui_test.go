package controlui

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrap(t *testing.T) {
	h := New(Config{
		AgentID:       "test-agent",
		AssistantName: "TestBot",
		Version:       "1.0.0",
	}, slog.Default())

	req := httptest.NewRequest("GET", "/api/control-ui/bootstrap", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["agentId"] != "test-agent" {
		t.Errorf("expected agentId=test-agent, got %v", body["agentId"])
	}
	identity := body["assistantIdentity"].(map[string]any)
	if identity["name"] != "TestBot" {
		t.Errorf("expected name=TestBot, got %v", identity["name"])
	}

	// Check security headers.
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("missing X-Frame-Options header")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing X-Content-Type-Options header")
	}
}

func TestBootstrap_MethodNotAllowed(t *testing.T) {
	h := New(Config{}, slog.Default())
	req := httptest.NewRequest("POST", "/api/control-ui/bootstrap", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestStaticServing(t *testing.T) {
	dir := t.TempDir()
	indexHTML := `<!DOCTYPE html><html><body>Hello</body></html>`
	os.WriteFile(filepath.Join(dir, "index.html"), []byte(indexHTML), 0644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log('hi')"), 0644)

	h := New(Config{Root: dir}, slog.Default())

	// Serve index.html at root.
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for root, got %d", rec.Code)
	}

	// Serve JS file.
	req = httptest.NewRequest("GET", "/app.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for app.js, got %d", rec.Code)
	}

	// SPA fallback: unknown path without extension serves index.html.
	req = httptest.NewRequest("GET", "/settings", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for SPA fallback, got %d", rec.Code)
	}

	// Unknown static file returns 404.
	req = httptest.NewRequest("GET", "/nonexistent.css", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing static file, got %d", rec.Code)
	}
}

func TestStaticServing_NoRoot(t *testing.T) {
	h := New(Config{}, slog.Default())
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no root, got %d", rec.Code)
	}
}

func TestAvatar_InvalidID(t *testing.T) {
	h := New(Config{}, slog.Default())
	req := httptest.NewRequest("GET", "/control-ui/avatar/!!!invalid", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid agent ID, got %d", rec.Code)
	}
}

func TestAvatar_Meta(t *testing.T) {
	h := New(Config{}, slog.Default())
	req := httptest.NewRequest("GET", "/control-ui/avatar/default?meta=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for avatar meta, got %d", rec.Code)
	}
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	if body["kind"] != "none" {
		t.Errorf("expected kind=none when no avatar dir, got %v", body["kind"])
	}
}

func TestResolveRoot(t *testing.T) {
	if ResolveRoot("") != RootMissing {
		t.Error("empty path should be missing")
	}
	if ResolveRoot("/nonexistent") != RootMissing {
		t.Error("nonexistent path should be missing")
	}

	dir := t.TempDir()
	if ResolveRoot(dir) != RootInvalid {
		t.Error("dir without index.html should be invalid")
	}

	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0644)
	if ResolveRoot(dir) != RootResolved {
		t.Error("dir with index.html should be resolved")
	}
}
