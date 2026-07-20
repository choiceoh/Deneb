// web_fetch_browser.go — stealth-tier page render via the resident browser
// sidecar (scripts/browser — Playwright headful Chromium on :18930, the same
// sidecar the browse tool drives; see tools/browse.go).
//
// This is the local-first replacement for what used to require r.jina.ai: a
// real JS-executing render for SPA / bot-walled pages, without the target URL
// leaving the machine. Jina remains one rung above as the external last
// resort for when the sidecar is down or the render fails.
//
// SSRF: the sidecar is a full browser with no dialer guard, and a tier-memory
// jump can reach this stage without the SSRF-safe transport ever having
// vetted the URL — so the target is validated against the same private-range
// rules (media.ValidatePublicTarget) before dispatch. The browse TOOL
// intentionally has no such guard (operator-driven browsing); the web tool's
// contract is public-web only.
//
// The sidecar carries the operator's login sessions. The browse tool already
// exposes exactly this render capability to the model, so routing stealth
// escalations through it grants no new authority — but a render can differ
// from an anonymous fetch on sites where the operator is signed in.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

const (
	// browserRenderDefaultURL mirrors tools/browse.go's sidecar default; both
	// honor DENEB_BROWSE_URL (env override + sane default, sidecar convention).
	browserRenderDefaultURL = "http://127.0.0.1:18930"
	// browserRenderTimeout bounds navigate (sidecar caps at 25s) + settle +
	// extract. Tighter than the browse tool's 75s: a stealth fetch still has
	// the Jina rung behind it and must not monopolize the turn.
	browserRenderTimeout = 45 * time.Second
)

// Seams (jinaFetchFn pattern): tests swap these to avoid network and DNS.
var (
	browserRenderFn        = browserRender
	validatePublicTargetFn = media.ValidatePublicTarget
)

func browserRenderBase() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_BROWSE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return browserRenderDefaultURL
}

// browserRender renders targetURL in the resident browser sidecar and returns
// the readable text as a text/plain FetchResult — the same shape the Jina
// stage yields, so downstream extraction is uniform. Any failure (private
// target, sidecar down, failed/empty render) is a plain error; callers
// escalate to the external fallback.
func browserRender(ctx context.Context, targetURL string, maxBytes int64) (*media.FetchResult, error) {
	if err := validatePublicTargetFn(ctx, targetURL); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"url": targetURL})
	if err != nil {
		return nil, err
	}
	renderCtx, cancel := context.WithTimeout(ctx, browserRenderTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(renderCtx, http.MethodPost,
		browserRenderBase()+"/browse", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Plain client on purpose: the sidecar lives on loopback, which the
	// SSRF-safe shared transport would (correctly) refuse to dial.
	resp, err := (&http.Client{Timeout: browserRenderTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("browser sidecar unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Title string `json:"title"`
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("browser sidecar: bad response: %w", err)
	}
	if !out.OK || strings.TrimSpace(out.Text) == "" {
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d, empty render", resp.StatusCode)
		}
		return nil, fmt.Errorf("browser sidecar render failed: %s", msg)
	}

	text := out.Text
	if t := strings.TrimSpace(out.Title); t != "" {
		text = t + "\n\n" + text
	}
	data := truncateAtRune([]byte(text), maxBytes)
	finalURL := strings.TrimSpace(out.URL)
	if finalURL == "" {
		finalURL = targetURL
	}
	return &media.FetchResult{
		Data:        data,
		ContentType: "text/plain; charset=utf-8",
		Size:        len(data),
		FinalURL:    finalURL,
		StatusCode:  http.StatusOK,
	}, nil
}

// truncateAtRune caps data at maxBytes without splitting a UTF-8 sequence.
func truncateAtRune(data []byte, maxBytes int64) []byte {
	if maxBytes <= 0 || int64(len(data)) <= maxBytes {
		return data
	}
	cut := int(maxBytes)
	for cut > 0 && !utf8.RuneStart(data[cut]) {
		cut--
	}
	return data[:cut]
}
