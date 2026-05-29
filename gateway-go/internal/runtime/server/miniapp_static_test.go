package server

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// swapMiniappFS replaces the package-level embed FS with one supplied by the
// test, restoring it on cleanup. Useful for testing fallback paths without
// rebuilding the binary.
func swapMiniappFS(t *testing.T, replacement fs.FS) {
	t.Helper()
	original := miniappSubFS
	miniappSubFS = replacement
	t.Cleanup(func() { miniappSubFS = original })
}

func newServerForStatic(t *testing.T) *Server {
	t.Helper()
	s, err := New(":0")
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return s
}

func getMiniappPath(t *testing.T, s *Server, urlPath string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, urlPath, nil)
	rec := httptest.NewRecorder()
	s.serveMiniappStatic(rec, req)
	return rec
}

func TestServeMiniappStatic_IndexAtRoot(t *testing.T) {
	s := newServerForStatic(t)
	rec := getMiniappPath(t, s, "/app/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store, no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q, want no-store, no-cache, must-revalidate", cc)
	}
	if p := rec.Header().Get("Pragma"); p != "no-cache" {
		t.Errorf("Pragma = %q, want no-cache", p)
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("response body does not look like HTML: %q", rec.Body.String())
	}
}

func TestServeMiniappStatic_IndexExplicit(t *testing.T) {
	s := newServerForStatic(t)
	rec := getMiniappPath(t, s, "/app/index.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestServeMiniappStatic_SPAFallback(t *testing.T) {
	s := newServerForStatic(t)
	rec := getMiniappPath(t, s, "/app/some/deep/route/that/does/not/exist")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (SPA fallback)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("SPA fallback did not serve HTML: %q", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store, no-cache, must-revalidate" {
		t.Errorf("Cache-Control on fallback = %q, want no-store, no-cache, must-revalidate", cc)
	}
}

func TestServeMiniappStatic_MissingHashedAssetReturns404(t *testing.T) {
	// A redeploy rewrites every content-hashed chunk filename. A bundle
	// still in memory (or cached by Telegram's WebView) keeps requesting an
	// old hash that the new embed FS no longer carries. That must surface as
	// a real 404 — NOT the SPA HTML fallback. If we returned index.html with
	// 200, the browser's ES-module loader would receive an HTML body where
	// it expected JS and choke with "Failed to fetch dynamically imported
	// module", which is exactly the operator-visible bug this guards against.
	swapMiniappFS(t, fstest.MapFS{
		"index.html":             &fstest.MapFile{Data: []byte("<!doctype html><html><body>app</body></html>")},
		"assets/calendar-new.js": &fstest.MapFile{Data: []byte("export const x = 1;")},
	})
	s := newServerForStatic(t)

	rec := getMiniappPath(t, s, "/app/assets/calendar-BMaDBaGq.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for missing hashed asset", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<html") {
		t.Errorf("missing asset served HTML fallback (would break the module loader): %q", rec.Body.String())
	}
}

func TestServeMiniappStatic_PathTraversalRejected(t *testing.T) {
	s := newServerForStatic(t)
	for _, p := range []string{
		"/app/../secret",
		"/app/./../etc/passwd",
		"/app/assets/../../oops",
	} {
		rec := getMiniappPath(t, s, p)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("path %q: status = %d, want 400", p, rec.Code)
		}
	}
}

func TestServeMiniappStatic_FallsBackToPlaceholderWhenIndexMissing(t *testing.T) {
	// Simulate a fresh clone: only placeholder.html is in the embed FS
	// (no index.html, no assets/) because the operator never ran
	// `make embed-frontend`.
	swapMiniappFS(t, fstest.MapFS{
		"placeholder.html": &fstest.MapFile{
			Data: []byte("<!doctype html><h1>placeholder</h1>"),
		},
	})
	s := newServerForStatic(t)

	rec := getMiniappPath(t, s, "/app/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "placeholder") {
		t.Errorf("expected placeholder content, got %q", body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store, no-cache, must-revalidate" {
		t.Errorf("Cache-Control = %q, want no-store, no-cache, must-revalidate", cc)
	}
}

func TestServeMiniappStatic_PrefersIndexOverPlaceholder(t *testing.T) {
	// Both files present (operator ran embed-frontend). index.html wins.
	swapMiniappFS(t, fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<!doctype html><h1>real bundle</h1>"),
		},
		"placeholder.html": &fstest.MapFile{
			Data: []byte("<!doctype html><h1>placeholder</h1>"),
		},
	})
	s := newServerForStatic(t)

	rec := getMiniappPath(t, s, "/app/")
	body := rec.Body.String()
	if !strings.Contains(body, "real bundle") {
		t.Errorf("expected real bundle, got %q", body)
	}
	if strings.Contains(body, "placeholder") {
		t.Errorf("placeholder leaked into response: %q", body)
	}
}

func TestServeMiniappStatic_NoEntryFile500(t *testing.T) {
	// Pathological case: neither index.html nor placeholder.html present.
	// The handler should refuse cleanly rather than serve an empty body.
	swapMiniappFS(t, fstest.MapFS{
		"random.txt": &fstest.MapFile{Data: []byte("nope")},
	})
	s := newServerForStatic(t)

	rec := getMiniappPath(t, s, "/app/")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// --- Content-Encoding negotiation ---

func TestServeMiniappStatic_ServesBrotliWhenAccepted(t *testing.T) {
	swapMiniappFS(t, fstest.MapFS{
		"index.html":       &fstest.MapFile{Data: []byte("RAW")},
		"assets/app.js":    &fstest.MapFile{Data: []byte("RAW JS BODY")},
		"assets/app.js.br": &fstest.MapFile{Data: []byte("BR BODY")},
		"assets/app.js.gz": &fstest.MapFile{Data: []byte("GZ BODY")},
	})
	s := newServerForStatic(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/app/assets/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	s.serveMiniappStatic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Errorf("Content-Encoding = %q, want br (preferred over gzip)", got)
	}
	if rec.Body.String() != "BR BODY" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "BR BODY")
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Errorf("Vary = %q, want Accept-Encoding", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript prefix", got)
	}
}

func TestServeMiniappStatic_FallsBackToGzipWhenBrotliUnavailable(t *testing.T) {
	swapMiniappFS(t, fstest.MapFS{
		"assets/app.js":    &fstest.MapFile{Data: []byte("RAW JS BODY")},
		"assets/app.js.gz": &fstest.MapFile{Data: []byte("GZ BODY")},
		// No .br sibling.
	})
	s := newServerForStatic(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/app/assets/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	s.serveMiniappStatic(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip", got)
	}
	if rec.Body.String() != "GZ BODY" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "GZ BODY")
	}
}

func TestServeMiniappStatic_ServesRawWhenNoEncodingAccepted(t *testing.T) {
	swapMiniappFS(t, fstest.MapFS{
		"assets/app.js":    &fstest.MapFile{Data: []byte("RAW JS BODY")},
		"assets/app.js.br": &fstest.MapFile{Data: []byte("BR BODY")},
		"assets/app.js.gz": &fstest.MapFile{Data: []byte("GZ BODY")},
	})
	s := newServerForStatic(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/app/assets/app.js", nil)
	// No Accept-Encoding header at all — server must serve raw.
	rec := httptest.NewRecorder()
	s.serveMiniappStatic(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty", got)
	}
	if rec.Body.String() != "RAW JS BODY" {
		t.Errorf("body = %q, want raw", rec.Body.String())
	}
}

func TestServeMiniappStatic_RespectsExplicitQZero(t *testing.T) {
	// A client that explicitly rejects brotli via q=0 must not receive
	// the .br body even if it's available.
	swapMiniappFS(t, fstest.MapFS{
		"assets/app.js":    &fstest.MapFile{Data: []byte("RAW JS BODY")},
		"assets/app.js.br": &fstest.MapFile{Data: []byte("BR BODY")},
		"assets/app.js.gz": &fstest.MapFile{Data: []byte("GZ BODY")},
	})
	s := newServerForStatic(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/app/assets/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br;q=0")
	rec := httptest.NewRecorder()
	s.serveMiniappStatic(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Errorf("Content-Encoding = %q, want gzip (br rejected via q=0)", got)
	}
}

func TestServeMiniappStatic_DirectCompressedRequest404s(t *testing.T) {
	// Compressed siblings are an implementation detail — asking for
	// them directly over the wire must be rejected so we can't be
	// tricked into serving a body the client doesn't know how to
	// decode (no Content-Encoding negotiation on a .br path).
	swapMiniappFS(t, fstest.MapFS{
		"assets/app.js":    &fstest.MapFile{Data: []byte("RAW JS BODY")},
		"assets/app.js.br": &fstest.MapFile{Data: []byte("BR BODY")},
	})
	s := newServerForStatic(t)

	rec := getMiniappPath(t, s, "/app/assets/app.js.br")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestParseEncodingPreference(t *testing.T) {
	cases := []struct {
		header   string
		wantBr   bool
		wantGzip bool
	}{
		{"", false, false},
		{"identity", false, false},
		{"gzip", false, true},
		{"br", true, false},
		{"gzip, br", true, true},
		{"br, gzip, deflate", true, true},
		{"*", true, true},
		{"br;q=0, gzip", false, true},
		{"br;q=0.5, gzip;q=0.9", true, true},
		{"br;q=0, gzip;q=0", false, false},
		{" GZIP ,  BR ", true, true}, // case + whitespace tolerance
	}
	for _, c := range cases {
		t.Run(c.header, func(t *testing.T) {
			gotBr, gotGz := parseEncodingPreference(c.header)
			if gotBr != c.wantBr || gotGz != c.wantGzip {
				t.Errorf(
					"parseEncodingPreference(%q) = (br=%v, gz=%v), want (br=%v, gz=%v)",
					c.header, gotBr, gotGz, c.wantBr, c.wantGzip,
				)
			}
		})
	}
}

func TestContentTypeForMiniappAsset(t *testing.T) {
	cases := map[string]string{
		"index.html":              "text/html; charset=utf-8",
		"assets/index-abcd.js":    "application/javascript; charset=utf-8",
		"assets/style.css":        "text/css; charset=utf-8",
		"data.json":               "application/json; charset=utf-8",
		"icon.svg":                "image/svg+xml",
		"logo.png":                "image/png",
		"photo.jpg":               "image/jpeg",
		"font.woff2":              "font/woff2",
		"weird.unknown-extension": "application/octet-stream",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			if got := contentTypeForMiniappAsset(name); got != want {
				t.Errorf("contentTypeForMiniappAsset(%q) = %q, want %q", name, got, want)
			}
		})
	}
}
