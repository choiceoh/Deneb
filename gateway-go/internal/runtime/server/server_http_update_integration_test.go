package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

// getAppUpdate drives a GET through the real mux so routing + auth + handler are
// all exercised. A header token simulates the native client's manifest check; a
// query token (inside q) simulates the browser opening the download link.
func getAppUpdate(t *testing.T, s *Server, base string, q url.Values, headerToken string) *httptest.ResponseRecorder {
	t.Helper()
	full := base
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, full, nil)
	if headerToken != "" {
		req.Header.Set(clientauth.Header, headerToken)
	}
	rec := httptest.NewRecorder()
	s.buildMux().ServeHTTP(rec, req)
	return rec
}

func TestAppUpdateManifest_Integration(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{
		"deneb-2.9.28-151-fossDebug.apk",
		"deneb-2.9.30-153-mailfix-fossDebug.apk", // newest
	} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("apk"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DENEB_APK_DIR", dir)
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	s := newTestServer(t)

	// No token → 401.
	if rec := getAppUpdate(t, s, "/api/v1/app/update/manifest", nil, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("manifest without token must be 401, got %d", rec.Code)
	}

	// Valid header token → 200 with the newest published code.
	rec := getAppUpdate(t, s, "/api/v1/app/update/manifest", nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest with token must be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var m appUpdateManifest
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if m.Code != 153 || m.File != "deneb-2.9.30-153-mailfix-fossDebug.apk" {
		t.Fatalf("want newest code=153 file, got code=%d file=%q", m.Code, m.File)
	}
}

func TestAppUpdateDownload_Integration(t *testing.T) {
	dir := t.TempDir()
	apk := "deneb-2.9.30-153-fossDebug.apk"
	want := []byte("PK\x03\x04 fake apk payload")
	if err := os.WriteFile(filepath.Join(dir, apk), want, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_APK_DIR", dir)
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	s := newTestServer(t)

	ok := url.Values{"file": {apk}, "clientToken": {token}}

	// Valid query token → 200 with the exact bytes and the apk content type.
	rec := getAppUpdate(t, s, "/api/v1/app/update/download", ok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("download must be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("apk bytes mismatch: got %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Errorf("want apk content type, got %q", ct)
	}

	// Missing token → 401.
	if rec := getAppUpdate(t, s, "/api/v1/app/update/download", url.Values{"file": {apk}}, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("download without token must be 401, got %d", rec.Code)
	}

	// Non-apk traversal target → filepath.Base strips the path, the .apk check
	// rejects it → 400 (never reads outside the serve dir).
	bad := url.Values{"file": {"../../../etc/passwd"}, "clientToken": {token}}
	if rec := getAppUpdate(t, s, "/api/v1/app/update/download", bad, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("non-apk traversal must be 400, got %d", rec.Code)
	}

	// A .apk-suffixed traversal still resolves to its base name only, so it
	// stays inside the serve dir and 404s rather than escaping.
	esc := url.Values{"file": {"../secret.apk"}, "clientToken": {token}}
	if rec := getAppUpdate(t, s, "/api/v1/app/update/download", esc, ""); rec.Code != http.StatusNotFound {
		t.Errorf("apk traversal must resolve to base name and 404, got %d", rec.Code)
	}
}
