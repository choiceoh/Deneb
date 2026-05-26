package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
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
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control on fallback = %q, want no-cache", cc)
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
