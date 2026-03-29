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
			return webFetchErr{
				Code:      "http_" + strconv.Itoa(mfe.Status),
				Message:   mfe.Message,
				URL:       url,
				Retryable: mfe.Status >= 500,
			}
		case media.ErrMaxBytes:
			return webFetchErr{
				Code: "content_too_large", Message: mfe.Message,
				URL: url, Retryable: false,
			}
		case media.ErrFetchFailed:
			code := "fetch_failed"
			msg := mfe.Message
			retryable := true
			switch {
			case strings.Contains(msg, "SSRF"):
				code, retryable = "ssrf_blocked", false
			case strings.Contains(msg, "no such host") || strings.Contains(msg, "no addresses"):
				code, retryable = "dns_failure", false
			case strings.Contains(msg, "too many redirects"):
				code, retryable = "redirect_loop", false
			case strings.Contains(msg, "certificate"):
				code, retryable = "tls_error", false
			case strings.Contains(msg, "connection refused"):
				code, retryable = "connection_refused", true
			case strings.Contains(msg, "connection reset"):
				code, retryable = "connection_reset", true
			}
			return webFetchErr{Code: code, Message: msg, URL: url, Retryable: retryable}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return webFetchErr{Code: "timeout", Message: "request timed out", URL: url, Retryable: true}
	}
	if errors.Is(err, context.Canceled) {
		return webFetchErr{Code: "canceled", Message: "request canceled", URL: url, Retryable: false}
	}
	return webFetchErr{Code: "unknown", Message: err.Error(), URL: url, Retryable: false}
}
