package web

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

func TestIsSoftBlockReturnsTrueForChallengePages(t *testing.T) {
	tests := []struct {
		name string
		html string
		ct   string
		size int
		want bool
	}{
		{
			"cloudflare challenge",
			`<html><head><title>Just a moment...</title></head><body>Checking your browser</body></html>`,
			"text/html", 100, true,
		},
		{
			"cloudflare cf_chl_opt",
			`<html><script>window._cf_chl_opt={}</script></html>`,
			"text/html", 80, true,
		},
		{
			"recaptcha challenge",
			`<html><div class="g-recaptcha" data-sitekey="abc"></div></html>`,
			"text/html", 150, true,
		},
		{
			"hcaptcha",
			`<html><div class="h-captcha"></div></html>`,
			"text/html", 100, true,
		},
		{
			"perimeterx",
			`<html><script>PerimeterX challenge</script></html>`,
			"text/html", 100, true,
		},
		{
			"datadome",
			`<html><script>DataDome challenge</script><div id="dd_challenge"></div></html>`,
			"text/html", 120, true,
		},
		{
			"normal page",
			`<html><body><h1>Hello</h1><p>` + string(make([]byte, 2000)) + `</p></body></html>`,
			"text/html", 2100, false,
		},
		{
			"json response",
			`{"status": "blocked"}`,
			"application/json", 20, false,
		},
		{
			"large page not checked",
			`<html>` + string(make([]byte, 60000)) + `</html>`,
			"text/html", 60010, false,
		},
		{
			"nil result",
			"", "", 0, false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result *media.FetchResult
			if tt.html != "" {
				result = &media.FetchResult{
					Data:        []byte(tt.html),
					ContentType: tt.ct,
					Size:        tt.size,
				}
			}
			got := isSoftBlock(result)
			if got != tt.want {
				t.Errorf("isSoftBlock = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBlockError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"403 forbidden", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 403}, true},
		{"429 rate limit", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 429}, true},
		{"503 service unavail", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 503}, true},
		{"451 legal block", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 451}, true},
		{"404 not found", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 404}, false},
		{"200 ok", &media.MediaFetchError{Code: media.ErrHTTPError, Status: 200}, false},
		{"fetch failed", &media.MediaFetchError{Code: media.ErrFetchFailed}, false},
		{"max bytes", &media.MediaFetchError{Code: media.ErrMaxBytes}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBlockError(tt.err); got != tt.want {
				t.Errorf("isBlockError = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJinaReaderURL(t *testing.T) {
	const target = "https://example.com/page?q=test"

	t.Run("default base appends raw url", func(t *testing.T) {
		t.Setenv("DENEB_JINA_URL", "")
		got := jinaReaderURL(target)
		want := "https://r.jina.ai/" + target
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("env override with trailing slash trimmed", func(t *testing.T) {
		t.Setenv("DENEB_JINA_URL", "http://127.0.0.1:9999/")
		got := jinaReaderURL(target)
		want := "http://127.0.0.1:9999/" + target
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestIsSPAShellResult(t *testing.T) {
	spa := &media.FetchResult{
		Data: []byte(`<!DOCTYPE html><html><head><script src="/_next/static/chunks/main.js"></script></head>` +
			`<body><div id="__next"></div></body></html>`),
		ContentType: "text/html",
		Size:        200,
	}
	if !isSPAShellResult(spa) {
		t.Fatal("expected SPA shell with __next + thin body to signal js_required")
	}

	article := &media.FetchResult{
		Data: []byte(`<html><body><article><h1>Title</h1><p>` +
			strings.Repeat(" substantive paragraph text ", 40) +
			`</p></article></body></html>`),
		ContentType: "text/html",
		Size:        5000,
	}
	if isSPAShellResult(article) {
		t.Fatal("normal article must not be treated as SPA shell")
	}

	if isSPAShellResult(&media.FetchResult{Data: []byte(`{"ok":true}`), ContentType: "application/json"}) {
		t.Fatal("non-HTML must not be SPA shell")
	}
}

func TestBrowserProfilesEmitHeadersWithoutBotUserAgent(t *testing.T) {
	// Verify profiles have essential headers.
	for _, profile := range []browserProfile{chromeProfile, firefoxProfile} {
		t.Run(profile.name, func(t *testing.T) {
			required := []string{"User-Agent", "Accept", "Accept-Language"}
			for _, key := range required {
				if profile.headers[key] == "" {
					t.Errorf("missing header %q in profile %s", key, profile.name)
				}
			}
			ua := profile.headers["User-Agent"]
			if ua == "Deneb-Gateway/1.0" {
				t.Error("profile should not use bot User-Agent")
			}
			if len(ua) < 50 {
				t.Error("User-Agent too short to be realistic")
			}
		})
	}
}

// withFreshTiers isolates the package tier memory for a test.
func withFreshTiers(t *testing.T) *tierMemory {
	t.Helper()
	orig := stealthTiers
	fresh := newTierMemory("")
	stealthTiers = fresh
	t.Cleanup(func() { stealthTiers = orig })
	return fresh
}

func TestStealthFetchStartsAtLearnedBrowserStage(t *testing.T) {
	tiers := withFreshTiers(t)
	now := time.Now()
	tiers.recordSuccess("learned.example", 2, now)

	rendered := "학습된 도메인 렌더 본문"
	var browserCalls atomic.Int32
	origBrowser := browserRenderFn
	browserRenderFn = func(_ context.Context, url string, _ int64) (*media.FetchResult, error) {
		browserCalls.Add(1)
		if !strings.Contains(url, "learned.example") {
			t.Errorf("browser render got url %q", url)
		}
		return &media.FetchResult{
			Data:        []byte(rendered),
			ContentType: "text/plain; charset=utf-8",
			Size:        len(rendered),
			StatusCode:  200,
		}, nil
	}
	t.Cleanup(func() { browserRenderFn = origBrowser })

	// The learned tier must jump straight to the browser stage: stages 0/1
	// would hit the real network (learned.example does not resolve) and fail
	// this test with a non-render result.
	result, err := stealthFetch(context.Background(), "https://learned.example/page", 1<<20)
	if err != nil {
		t.Fatalf("stealthFetch: %v", err)
	}
	if string(result.Data) != rendered {
		t.Errorf("data = %q", result.Data)
	}
	if got := browserCalls.Load(); got != 1 {
		t.Errorf("browser render called %d times, want 1", got)
	}
	// Success re-confirms the learned tier.
	if got := tiers.startStage("learned.example", now.Add(time.Minute)); got != 2 {
		t.Errorf("post-success start = %d, want 2", got)
	}
}
