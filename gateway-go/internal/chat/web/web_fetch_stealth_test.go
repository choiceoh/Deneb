package web

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

func TestIsSoftBlock_CloudflareChallenge(t *testing.T) {
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

func TestGoogleCacheURL(t *testing.T) {
	got := googleCacheURL("https://example.com/page?q=test")
	want := "https://webcache.googleusercontent.com/search?q=cache:https%3A%2F%2Fexample.com%2Fpage%3Fq%3Dtest"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBrowserProfiles(t *testing.T) {
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
