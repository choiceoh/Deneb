package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

// allowAnyTarget disables the public-target guard for hermetic tests (the real
// guard resolves DNS). Restored on cleanup.
func allowAnyTarget(t *testing.T) {
	t.Helper()
	orig := validatePublicTargetFn
	validatePublicTargetFn = func(context.Context, string) error { return nil }
	t.Cleanup(func() { validatePublicTargetFn = orig })
}

func TestBrowserRenderSynthesizesTextPlainResult(t *testing.T) {
	allowAnyTarget(t)
	var gotURL atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/browse" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotURL.Store(req.URL)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "url": "https://target.example/final",
			"title": "페이지 제목", "text": "렌더된 본문",
		})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)

	result, err := browserRender(context.Background(), "https://target.example/page", 1<<20)
	if err != nil {
		t.Fatalf("browserRender: %v", err)
	}
	if gotURL.Load() != "https://target.example/page" {
		t.Errorf("sidecar got url %v", gotURL.Load())
	}
	if !strings.HasPrefix(result.ContentType, "text/plain") {
		t.Errorf("content type = %q", result.ContentType)
	}
	if string(result.Data) != "페이지 제목\n\n렌더된 본문" {
		t.Errorf("data = %q", result.Data)
	}
	if result.FinalURL != "https://target.example/final" || result.StatusCode != 200 {
		t.Errorf("finalURL=%q status=%d", result.FinalURL, result.StatusCode)
	}
}

func TestBrowserRenderErrorsSurfaceAsErrors(t *testing.T) {
	allowAnyTarget(t)
	tests := []struct {
		name string
		body map[string]any
		want string
	}{
		{"failed render", map[string]any{"ok": false, "error": "nav timeout"}, "nav timeout"},
		{"empty text", map[string]any{"ok": true, "text": "  "}, "empty render"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(tt.body)
			}))
			defer srv.Close()
			t.Setenv("DENEB_BROWSE_URL", srv.URL)
			if _, err := browserRender(context.Background(), "https://x.example/p", 1024); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want containing %q", err, tt.want)
			}
		})
	}

	t.Run("sidecar down", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close() // immediately closed → connection refused
		t.Setenv("DENEB_BROWSE_URL", srv.URL)
		if _, err := browserRender(context.Background(), "https://x.example/p", 1024); err == nil || !strings.Contains(err.Error(), "unreachable") {
			t.Errorf("err = %v, want unreachable", err)
		}
	})
}

func TestBrowserRenderBlocksPrivateTargetBeforeDispatch(t *testing.T) {
	var sidecarCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sidecarCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": "leaked"})
	}))
	defer srv.Close()
	t.Setenv("DENEB_BROWSE_URL", srv.URL)

	// Real guard: a private-literal target must be rejected without the
	// sidecar ever seeing the URL.
	if _, err := browserRender(context.Background(), "http://127.0.0.1:18789/health", 1024); err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("err = %v, want SSRF rejection", err)
	}
	if got := sidecarCalls.Load(); got != 0 {
		t.Errorf("sidecar called %d times for a private target", got)
	}
}

func TestTruncateAtRuneNeverSplitsUTF8(t *testing.T) {
	data := []byte("한국어 텍스트입니다")
	for cap := int64(1); cap < int64(len(data)); cap++ {
		out := truncateAtRune(data, cap)
		if int64(len(out)) > cap {
			t.Fatalf("cap %d: len %d", cap, len(out))
		}
		if !utf8.Valid(out) {
			t.Fatalf("cap %d: invalid UTF-8 %q", cap, out)
		}
	}
	if got := truncateAtRune(data, 0); len(got) != len(data) {
		t.Errorf("cap 0 must mean unlimited, got %d bytes", len(got))
	}
}
