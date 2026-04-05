// web_http.go — HTTP fetch layer: error type, retry, error classification, YouTube.
//
// Wraps stealthFetch (web_fetch_stealth.go) and translates HTTP/transport errors
// into machine-readable webFetchErr codes for agent consumption.
package web

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

// webFetchErr is a machine-readable fetch error for agent consumption.
type webFetchErr struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	Retryable bool   `json:"retryable"`
	Hint      string `json:"hint,omitempty"`
}

// fetchWithRetry fetches a URL using browser-like stealth profiles.
// Delegates to stealthFetch which handles bot-block detection and escalation.
func fetchWithRetry(ctx context.Context, url string, maxBytes int64) (*media.FetchResult, error) {
	return stealthFetch(ctx, url, maxBytes)
}

func isRetryableError(err error) bool {
	var mfe *media.MediaFetchError
	if errors.As(err, &mfe) {
		if mfe.Code == media.ErrHTTPError && mfe.Status >= 500 {
			return true
		}
		if mfe.Code == media.ErrFetchFailed {
			return true
		}
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func fetchYouTube(ctx context.Context, url string) (string, error) {
	ytCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := media.ExtractYouTubeTranscript(ytCtx, url)
	if err != nil {
		return formatFetchError(webFetchErr{
			Code: "youtube_failed", Message: err.Error(),
			URL: url, Retryable: true,
		}), nil
	}
	return media.FormatYouTubeResult(result), nil
}

// classifyFetchError maps transport and HTTP errors to agent-readable codes.
func classifyFetchError(err error, url string) webFetchErr {
	var mfe *media.MediaFetchError
	if errors.As(err, &mfe) {
		switch mfe.Code {
		case media.ErrHTTPError:
			e := webFetchErr{
				Code:      "http_" + strconv.Itoa(mfe.Status),
				Message:   mfe.Message,
				URL:       url,
				Retryable: mfe.Status >= 500,
			}
			e.Hint = hintForHTTPStatus(mfe.Status)
			return e
		case media.ErrMaxBytes:
			return webFetchErr{
				Code: "content_too_large", Message: mfe.Message,
				URL: url, Retryable: false,
				Hint: "Too large. Use maxChars to limit, or fetch a specific section URL",
			}
		case media.ErrFetchFailed:
			code := "fetch_failed"
			msg := mfe.Message
			retryable := true
			hint := ""
			switch {
			case strings.Contains(msg, "SSRF"):
				code, retryable = "ssrf_blocked", false
				hint = "Internal/private URL blocked. Use a public URL"
			case strings.Contains(msg, "no such host") || strings.Contains(msg, "no addresses"):
				code, retryable = "dns_failure", false
				hint = "Domain not found. Check URL for typos"
			case strings.Contains(msg, "too many redirects"):
				code, retryable = "redirect_loop", false
			case strings.Contains(msg, "certificate"):
				code, retryable = "tls_error", false
			case strings.Contains(msg, "connection refused"):
				code, retryable = "connection_refused", true
				hint = "Server not accepting connections. It may be down"
			case strings.Contains(msg, "connection reset"):
				code, retryable = "connection_reset", true
			}
			return webFetchErr{Code: code, Message: msg, URL: url, Retryable: retryable, Hint: hint}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return webFetchErr{Code: "timeout", Message: "request timed out", URL: url, Retryable: true,
			Hint: "Timed out. Retry or try a different source"}
	}
	if errors.Is(err, context.Canceled) {
		return webFetchErr{Code: "canceled", Message: "request canceled", URL: url, Retryable: false}
	}
	return webFetchErr{Code: "unknown", Message: err.Error(), URL: url, Retryable: false}
}

// hintForHTTPStatus returns an actionable hint for common HTTP error status codes.
func hintForHTTPStatus(status int) string {
	switch {
	case status == 403:
		return "Site blocked the request. Try http tool with custom headers, or search for cached version"
	case status == 429:
		return "Rate limited. Wait and retry, or try a different source"
	case status >= 500:
		return "Server error. Retry later"
	default:
		return ""
	}
}
