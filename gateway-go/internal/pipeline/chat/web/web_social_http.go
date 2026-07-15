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
	"strings"
	"time"
)

// socialGetJSON performs a bounded GET with a caller-supplied User-Agent and
// returns the raw body plus HTTP status. Errors are transport-level only;
// non-2xx statuses are returned to the caller to translate into an envelope.
// truncated reports that the body hit maxBytes and was cut, so callers can
// surface a clear size-limit error instead of a misleading parse failure.
func socialGetJSON(ctx context.Context, rawURL, userAgent string, maxBytes int64) (body []byte, status int, truncated bool, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := SharedClient(15 * time.Second).Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()

	// Read one byte past the cap so an oversized payload is detectable rather
	// than silently truncated into a broken JSON document.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, resp.StatusCode, false, err
	}
	if int64(len(raw)) > maxBytes {
		return raw[:maxBytes], resp.StatusCode, true, nil
	}
	return raw, resp.StatusCode, false, nil
}

// neutralizeAngleBrackets rewrites '<'/'>' in user-generated text so it cannot
// collide with the web tool's envelope markers (<content>/<error>), which
// assessFetchResult treats as control tags — an un-escaped "<error>" inside a
// post or tweet would otherwise flag the whole result as a fetch failure. HTML
// entities keep the text readable to the model.
func neutralizeAngleBrackets(s string) string {
	if !strings.ContainsAny(s, "<>") {
		return s
	}
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// collapseWS prepares user-generated text for the result envelope: it flattens
// whitespace runs into single spaces and trims (so multi-line post/comment
// bodies render as compact single-line entries), truncates to limit runes with
// an ellipsis marker, and finally neutralizes angle brackets so the text cannot
// collide with the envelope's <content>/<error> tags.
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
		return neutralizeAngleBrackets(string(b[:limit]) + "…")
	}
	return neutralizeAngleBrackets(string(b))
}
