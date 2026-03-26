package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestControlUI_BootstrapConfig(t *testing.T) {
	h := NewControlUIHandler("/ui/", "/nonexistent", "1.2.3", true, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/__deneb/control-ui-config.json", nil)
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected bootstrap config to be handled")
	}

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var cfg controlUIBootstrapConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("failed to decode bootstrap config: %v", err)
	}

	if cfg.BasePath != "/ui" {
		t.Errorf("expected basePath '/ui', got %q", cfg.BasePath)
	}
	if cfg.AssistantName != "Deneb" {
		t.Errorf("expected assistantName 'Deneb', got %q", cfg.AssistantName)
	}
	if cfg.ServerVersion != "1.2.3" {
		t.Errorf("expected serverVersion '1.2.3', got %q", cfg.ServerVersion)
	}

	// Verify security headers.
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options DENY, got %q", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected X-Content-Type-Options nosniff, got %q", got)
	}
}

func TestControlUI_StaticFileServing(t *testing.T) {
	// Create temp UI directory with test files.
	dir := t.TempDir()
	indexContent := []byte("<html><body>Deneb UI</body></html>")
	jsContent := []byte("console.log('app');")

	if err := os.WriteFile(filepath.Join(dir, "index.html"), indexContent, 0o644); err != nil {
		t.Fatal(err)
	}
	assetsDir := filepath.Join(dir, "assets")
	if err := os.Mkdir(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "app.js"), jsContent, 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewControlUIHandler("/", dir, "1.0.0", true, slog.Default())

	// Test serving index.html at root.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected root request to be handled")
	}
	body, _ := io.ReadAll(w.Result().Body)
	if string(body) == "" {
		t.Error("expected non-empty body for index.html")
	}

	// Test serving a JS file.
	req = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	w = httptest.NewRecorder()
	handled = h.Handle(w, req)
	if !handled {
		t.Fatal("expected JS file request to be handled")
	}
	resp := w.Result()
	ct := resp.Header.Get("Content-Type")
	if ct != "application/javascript; charset=utf-8" {
		t.Errorf("expected JS content type, got %q", ct)
	}
}

func TestControlUI_SPAFallback(t *testing.T) {
	dir := t.TempDir()
	indexHTML := []byte("<html>SPA</html>")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), indexHTML, 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewControlUIHandler("/", dir, "1.0.0", true, slog.Default())

	// A path without file extension should serve index.html (SPA routing).
	req := httptest.NewRequest(http.MethodGet, "/sessions/abc123", nil)
	w := httptest.NewRecorder()
	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected SPA fallback to be handled")
	}
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for SPA fallback, got %d", resp.StatusCode)
	}
}

func TestControlUI_PathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := NewControlUIHandler("/", dir, "1.0.0", true, slog.Default())

	// Test dot-dot traversal.
	t.Run("dot-dot", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/../etc/passwd", nil)
		w := httptest.NewRecorder()
		handled := h.Handle(w, req)
		if !handled {
			t.Fatal("expected traversal attempt to be handled (blocked)")
		}
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for traversal, got %d", w.Result().StatusCode)
		}
	})

	t.Run("encoded-dot-dot", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/../../etc/passwd", nil)
		w := httptest.NewRecorder()
		handled := h.Handle(w, req)
		if !handled {
			t.Fatal("expected traversal attempt to be handled (blocked)")
		}
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for traversal, got %d", w.Result().StatusCode)
		}
	})

	// Null byte test uses a raw http.Request because httptest.NewRequest
	// rejects URLs containing control characters.
	t.Run("null-byte", func(t *testing.T) {
		req := &http.Request{
			Method: http.MethodGet,
			URL:    &url.URL{Path: "/index.html\x00.js"},
			Header: make(http.Header),
		}
		w := httptest.NewRecorder()
		handled := h.Handle(w, req)
		if !handled {
			t.Fatal("expected null byte attempt to be handled (blocked)")
		}
		if w.Result().StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for null byte, got %d", w.Result().StatusCode)
		}
	})
}

func TestControlUI_DisabledReturnsNotHandled(t *testing.T) {
	h := NewControlUIHandler("/", "/tmp", "1.0.0", false, slog.Default())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handled := h.Handle(w, req)
	if handled {
		t.Error("expected disabled handler to return false")
	}

	// Bootstrap config should also not be handled when disabled.
	req = httptest.NewRequest(http.MethodGet, "/__deneb/control-ui-config.json", nil)
	w = httptest.NewRecorder()
	handled = h.Handle(w, req)
	if handled {
		t.Error("expected bootstrap config to not be handled when disabled")
	}
}

func TestControlUI_MissingRootReturnsNotHandled(t *testing.T) {
	h := NewControlUIHandler("/", "/nonexistent/path/that/does/not/exist", "1.0.0", true, slog.Default())

	// Non-bootstrap requests should fall through when root is missing.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handled := h.Handle(w, req)
	if handled {
		t.Error("expected missing root to return false for static file requests")
	}

	// But bootstrap config should still work.
	req = httptest.NewRequest(http.MethodGet, "/__deneb/control-ui-config.json", nil)
	w = httptest.NewRecorder()
	handled = h.Handle(w, req)
	if !handled {
		t.Error("expected bootstrap config to be served even with missing root")
	}
}

func TestControlUI_BasePathNormalization(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"", "/"},
		{"/", "/"},
		{"ui", "/ui/"},
		{"/ui", "/ui/"},
		{"/ui/", "/ui/"},
	}

	for _, tc := range cases {
		h := NewControlUIHandler(tc.input, "", "1.0.0", true, slog.Default())
		if h.basePath != tc.expected {
			t.Errorf("input %q: expected basePath %q, got %q", tc.input, tc.expected, h.basePath)
		}
	}
}
