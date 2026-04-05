// web_fetch_stealth.go — Browser-like request profiles and bot-block evasion.
//
// Most websites block non-browser User-Agents immediately. This module provides
// realistic browser request profiles and a multi-stage escalation strategy:
//
//	Stage 0: Standard browser profile (Chrome on macOS)
//	Stage 1: Alternate profile (Firefox on Windows) + cookie jar
//	Stage 2: Google cache fallback
//
// Each profile includes the full set of headers a real browser sends:
// User-Agent, Accept, Accept-Language, Accept-Encoding, Sec-Fetch-*, etc.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

// browserProfile defines a complete set of HTTP headers that mimic a real browser.
type browserProfile struct {
	name    string
	headers map[string]string
}

// Primary profile: Chrome 131 on macOS (most common browser worldwide).
var chromeProfile = browserProfile{
	name: "chrome-macos",
	headers: map[string]string{
		"User-Agent":                "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8",
		"Accept-Language":           "ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7",
		"Accept-Encoding":           "identity", // no gzip — we need raw bytes for size limits
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Sec-Ch-Ua":                 `"Chromium";v="131", "Not_A Brand";v="24"`,
		"Sec-Ch-Ua-Mobile":          "?0",
		"Sec-Ch-Ua-Platform":        `"macOS"`,
		"Upgrade-Insecure-Requests": "1",
		"Cache-Control":             "max-age=0",
	},
}

// Alternate profile: Firefox 133 on Windows (different TLS/header fingerprint).
var firefoxProfile = browserProfile{
	name: "firefox-windows",
	headers: map[string]string{
		"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language":           "ko-KR,ko;q=0.8,en-US;q=0.5,en;q=0.3",
		"Accept-Encoding":           "identity",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
		"DNT":                       "1",
	},
}

// stealthFetch fetches a URL with browser-like headers and bot-block evasion.
// Escalation stages:
//
//	0: Chrome profile
//	1: Firefox profile + cookie jar (handles cookie-gated blocks)
//	2: Google webcache fallback (bypasses origin server entirely)
//
// Returns on first successful non-blocked response.
func stealthFetch(ctx context.Context, targetURL string, maxBytes int64) (*media.FetchResult, error) {
	stages := []struct {
		profile  browserProfile
		jar      bool
		cacheURL bool
		backoff  time.Duration
	}{
		{chromeProfile, false, false, 0},
		{firefoxProfile, true, false, 800 * time.Millisecond},
		{chromeProfile, false, true, 1200 * time.Millisecond},
	}

	var lastErr error
	for i, stage := range stages {
		if stage.backoff > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(stage.backoff):
			}
		}

		fetchURL := targetURL
		headers := stage.profile.headers
		if stage.cacheURL {
			fetchURL = googleCacheURL(targetURL)
			// Google cache uses a simpler header set.
			headers = map[string]string{
				"User-Agent":      stage.profile.headers["User-Agent"],
				"Accept":          stage.profile.headers["Accept"],
				"Accept-Language": stage.profile.headers["Accept-Language"],
				"Accept-Encoding": "identity",
			}
		}

		var client *http.Client
		if stage.jar {
			client = newCookieClient()
		} else {
			// Reuse the shared pooled transport for non-cookie requests.
			client = SharedClient(30 * time.Second)
		}

		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := media.Fetch(fetchCtx, media.FetchOptions{
			URL:      fetchURL,
			MaxBytes: maxBytes,
			Headers:  headers,
			Client:   client,
		})
		cancel()

		if err != nil {
			lastErr = err
			// Don't escalate on non-retryable errors (SSRF, DNS, max bytes).
			if !isRetryableError(err) && !isBlockError(err) {
				return nil, err
			}
			slog.Debug("stealth fetch failed, escalating",
				"stage", i, "profile", stage.profile.name,
				"url", targetURL, "error", err)
			continue
		}

		// Check if the response body indicates a soft block (200 with challenge page).
		if isSoftBlock(result) {
			slog.Debug("soft block detected, escalating",
				"stage", i, "profile", stage.profile.name, "url", targetURL)
			lastErr = &media.MediaFetchError{
				Code:    media.ErrHTTPError,
				Status:  403,
				Message: "soft block detected (challenge page)",
			}
			continue
		}

		return result, nil
	}

	return nil, lastErr
}

// isBlockError returns true for HTTP errors that indicate bot blocking.
func isBlockError(err error) bool {
	var mfe *media.MediaFetchError
	if !errors.As(err, &mfe) {
		return false
	}
	if mfe.Code != media.ErrHTTPError {
		return false
	}
	// Common block status codes.
	switch mfe.Status {
	case 403, 429, 451, 503:
		return true
	default:
		return false
	}
}

// isSoftBlock detects when a 200 response is actually a challenge/block page.
// Some CDNs (Cloudflare, Akamai, PerimeterX) return 200 with a challenge body.
func isSoftBlock(result *media.FetchResult) bool {
	if result == nil || len(result.Data) == 0 {
		return false
	}
	// Only check HTML responses.
	if !strings.Contains(result.ContentType, "text/html") {
		return false
	}
	// Challenge pages are typically small (< 15KB). Skip check on larger
	// responses to avoid false positives on real small pages.
	if result.Size > 15000 {
		return false
	}

	lower := strings.ToLower(string(result.Data))

	// Cloudflare challenge indicators.
	cfIndicators := []string{
		"cf-challenge-running",
		"cf_chl_opt",
		"challenge-platform",
		"/cdn-cgi/challenge-platform/",
		"just a moment...",
		"checking your browser",
		"enable javascript and cookies to continue",
	}
	for _, ind := range cfIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}

	// Generic CAPTCHA/challenge indicators.
	challengeIndicators := []string{
		"g-recaptcha",
		"h-captcha",
		"cf-turnstile",
		"please verify you are a human",
		"please complete the security check",
		"access to this page has been denied",
		"pardon our interruption",
		"one more step",
	}
	for _, ind := range challengeIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}

	// PerimeterX / DataDome / Imperva markers.
	botMgmtIndicators := []string{
		"perimeterx",
		"_px_captcha",
		"datadome",
		"dd_challenge",
		"imperva",
		"incapsula",
		"_incap_",
	}
	for _, ind := range botMgmtIndicators {
		if strings.Contains(lower, ind) {
			return true
		}
	}

	return false
}

// googleCacheURL returns the Google webcache URL for a given page.
// Google Cache serves a snapshot of the page without triggering the origin's
// bot protection. Useful as a last-resort fallback.
func googleCacheURL(originalURL string) string {
	return "https://webcache.googleusercontent.com/search?q=cache:" + url.QueryEscape(originalURL)
}

// newCookieClient creates an http.Client with a cookie jar backed by the shared
// transport. Some sites block requests that don't accept/send cookies,
// interpreting missing cookies as a bot signal.
func newCookieClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:       jar,
		Timeout:   60 * time.Second,
		Transport: sharedTransport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects (5)")
			}
			return nil
		},
	}
}
