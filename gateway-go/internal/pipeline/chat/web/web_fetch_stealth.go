// web_fetch_stealth.go — Browser-like request profiles and bot-block evasion.
//
// Most websites block non-browser User-Agents immediately. This module provides
// realistic browser request profiles and a multi-stage escalation strategy:
//
//	Stage 0: Standard browser profile (Chrome on macOS)
//	Stage 1: Alternate profile (Firefox on Windows) + cookie jar
//	Stage 2: Resident browser sidecar render (web_fetch_browser.go — local
//	         JS-executing render; beats SPA / JS / bot walls without the URL
//	         leaving the machine)
//	Stage 3: Jina Reader fallback (external last resort, only when the local
//	         sidecar is down or its render fails)
//
// Each profile includes the full set of headers a real browser sends:
// User-Agent, Accept, Accept-Language, Accept-Encoding, Sec-Fetch-*, etc.
//
// Per-domain tier memory (web_fetch_tier.go) starts the ladder at the stage
// that last worked for the domain, skipping the lower stages that were
// observed to fail — with a daily down-probe so domains drift back to cheaper
// tiers when they recover.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
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
//	1: Firefox profile + cookie jar (handles cookie-gated soft-blocks)
//	2: Resident browser sidecar render (local headless-equivalent; recovers
//	   SPA/JS/bot-walled pages without an external call)
//	3: Jina Reader fallback (external last resort)
//
// Fixed inter-stage sleeps are intentionally omitted — they only added latency
// on already-failing paths. Soft-blocks still try Firefox (cookie jar); SPA
// shells (js_required/empty_body) skip Firefox and go straight to the render
// stages. Per-domain tier memory picks the start stage; every stage that can
// run from a cold start is individually SSRF-safe (profile stages via the
// guarded transport, the sidecar via ValidatePublicTarget, Jina by fetching
// from Jina's own network).
//
// Returns on first successful non-blocked response.
func stealthFetch(ctx context.Context, targetURL string, maxBytes int64) (*media.FetchResult, error) {
	stages := []struct {
		profile browserProfile
		jar     bool
		browser bool
		jina    bool
	}{
		{chromeProfile, false, false, false},
		{firefoxProfile, true, false, false},
		{chromeProfile, false, true, false},
		{chromeProfile, false, false, true},
	}

	host := stealthHost(targetURL)
	start := stealthTiers.startStage(host, time.Now())
	if start >= len(stages) {
		start = len(stages) - 1
	}

	var fetchErrors []error
	var spaFallback *media.FetchResult
	skipFirefox := false

	for i, stage := range stages {
		if i < start {
			continue
		}
		if stage.jar && skipFirefox {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if stage.browser {
			result, err := browserRenderFn(ctx, targetURL, maxBytes)
			if err != nil {
				fetchErrors = append(fetchErrors, fmt.Errorf("stage %d (browser-sidecar): %w", i, err))
				slog.Debug("stealth browser render failed, escalating",
					"stage", i, "url", targetURL, "error", err)
				continue
			}
			stealthTiers.recordSuccess(host, i, time.Now())
			slog.Info("web stealth fetch done",
				"url", targetURL, "stage", i, "profile", "browser-sidecar", "jina", false, "startStage", start)
			return result, nil
		}

		fetchURL := targetURL
		headers := stage.profile.headers
		if stage.jina {
			// Jina Reader proxies a headless-rendered, plain-text view of the
			// page. We send the bare target through r.jina.ai and ask for
			// text/plain — no auth, no browser fingerprint games. This is the
			// only stage that leaves the origin/local network.
			fetchURL = jinaReaderURL(targetURL)
			headers = map[string]string{
				"User-Agent":      stage.profile.headers["User-Agent"],
				"Accept":          "text/plain",
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
			fetchErrors = append(fetchErrors, fmt.Errorf("stage %d (%s): %w", i, stage.profile.name, err))
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
			fetchErrors = append(fetchErrors, fmt.Errorf("stage %d (%s): soft block detected", i, stage.profile.name))
			continue
		}

		// SPA shell after Chrome: Firefox won't execute JS either — skip
		// straight to the render stages (sidecar, then Jina).
		if i == 0 && !stage.jina && isSPAShellResult(result) {
			slog.Debug("spa shell detected, skipping firefox for render stages",
				"stage", i, "url", targetURL)
			spaFallback = result
			skipFirefox = true
			fetchErrors = append(fetchErrors, fmt.Errorf("stage %d (%s): spa shell (js_required/empty_body)", i, stage.profile.name))
			continue
		}

		stealthTiers.recordSuccess(host, i, time.Now())
		slog.Info("web stealth fetch done",
			"url", targetURL, "stage", i, "profile", stage.profile.name, "jina", stage.jina, "startStage", start)
		return result, nil
	}

	// Render stages failed after an SPA shell — return the Chrome body so
	// thin-content escalation (or the agent) still has something rather than a
	// hard error.
	if spaFallback != nil {
		slog.Info("web stealth fetch spa fallback",
			"url", targetURL, "stage", 0, "profile", chromeProfile.name)
		return spaFallback, nil
	}

	return nil, errors.Join(fetchErrors...)
}

// stealthHost extracts the lowercased hostname for tier-memory keying.
// Unparseable URLs yield "" (tier memory no-ops).
func stealthHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// isSPAShellResult reports whether a successful origin fetch is an unrendered
// JS shell (js_required / empty_body). Soft-blocks are handled separately and
// must still try the Firefox+cookie path.
func isSPAShellResult(result *media.FetchResult) bool {
	if result == nil || len(result.Data) == 0 {
		return false
	}
	if !strings.Contains(result.ContentType, "text/html") &&
		!strings.Contains(result.ContentType, "application/xhtml") {
		return false
	}
	for _, s := range detectSignals(string(result.Data)) {
		if s == "js_required" || s == "empty_body" {
			return true
		}
	}
	return false
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

// jinaReaderBase is the default Jina Reader endpoint. Jina renders the target
// page headlessly (executing JS) and returns clean text, which recovers SPA,
// JS-required, and bot-walled pages that defeat our browser-profile attempts.
const jinaReaderBase = "https://r.jina.ai"

// jinaReaderURL builds the Jina Reader proxy URL for a target page:
//
//	https://r.jina.ai/https://example.com/path?q=1
//
// Jina expects the *raw* target URL appended to the base (NOT query-escaped) —
// it parses everything after the base host as the URL to fetch. The base is
// overridable via DENEB_JINA_URL (env override + sane default, per the sidecar
// model convention) for tests or a self-hosted Reader.
func jinaReaderURL(originalURL string) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("DENEB_JINA_URL")), "/")
	if base == "" {
		base = jinaReaderBase
	}
	return base + "/" + originalURL
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
