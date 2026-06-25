package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFilesDownloadRouteRegistered guards the route wiring: GET
// /api/v1/files/download must resolve to its handler (not the catch-all), and
// a non-GET must fall to the method-not-allowed handler.
func TestFilesDownloadRouteRegistered(t *testing.T) {
	s := &Server{}
	mux := s.buildMux()

	get := httptest.NewRequest(http.MethodGet, "/api/v1/files/download?path=/x", nil)
	if _, pattern := mux.Handler(get); !strings.Contains(pattern, "/api/v1/files/download") {
		t.Errorf("GET routed to %q, want the files download handler", pattern)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/v1/files/download", nil)
	w := httptest.NewRecorder()
	h, _ := mux.Handler(post)
	h.ServeHTTP(w, post)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d, want 405", w.Code)
	}
}

func TestFilesDownloadRejectsSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak.txt")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	t.Setenv("DENEB_FILES_DIR", dir)
	token := withClientToken(t)
	s := newTestServer(t)

	q := url.Values{"path": {"/leak.txt"}, "clientToken": {token}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/download?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	s.buildMux().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("symlinked file must be rejected, got %d: %s", w.Code, w.Body.String())
	}
}
