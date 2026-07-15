// web_social_http.go — shared JSON GET helper for platform read handlers.
//
// Reddit's public .json endpoints and X's syndication endpoint both return
// structured JSON that is far cleaner to parse than the SPA HTML the stealth
// fetcher would otherwise retrieve. Both ride the same pooled, SSRF-safe
// transport as the rest of the web tool (SharedClient → sharedTransport →
// media.SSRFSafeDialer), so this helper never bypasses the SSRF boundary —
// it only lets us set an API-appropriate User-Agent and cap the body.
package web

import (
	"context"
	"io"
	"net/http"
	"time"
)

// socialGetJSON performs a bounded GET with a caller-supplied User-Agent and
// returns the raw body plus HTTP status. Errors are transport-level only;
// non-2xx statuses are returned to the caller to translate into an envelope.
func socialGetJSON(ctx context.Context, rawURL, userAgent string, maxBytes int64) ([]byte, int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := SharedClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// collapseWS flattens whitespace runs into single spaces and trims, so
// multi-line post/comment bodies render as compact single-line entries. Long
// bodies are truncated to limit runes with an ellipsis marker.
func collapseWS(s string, limit int) string {
	b := make([]rune, 0, len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !prevSpace {
				b = append(b, ' ')
				prevSpace = true
			}
			continue
		}
		b = append(b, r)
		prevSpace = false
	}
	start, end := 0, len(b)
	for start < end && b[start] == ' ' {
		start++
	}
	for end > start && b[end-1] == ' ' {
		end--
	}
	b = b[start:end]
	if limit > 0 && len(b) > limit {
		return string(b[:limit]) + "…"
	}
	return string(b)
}
