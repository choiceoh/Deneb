// web_fetch_escalate.go — Escalation when a 200 OK hides an empty SPA shell.
//
// detectSignals() (web_html_preprocess.go) already flags js_required / empty_body
// when a page is a JavaScript-rendered shell with no server-side content. Without
// this file those signals were merely *reported* to the LLM as strings — a 200 OK
// returning a near-empty React/Next.js shell yielded an empty body and stopped.
//
// Here we turn that detection into an ACTION: when the extracted content is thin
// AND a JS/empty signal is present, we retry ONCE through a headless backend
// (Jina Reader, the same external last-resort used by the stealth pipeline's
// final stage). Exactly one retry — never a loop — and if it doesn't produce
// more content we keep the original result.
package web

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/media"
)

// thinContentThreshold is the extracted-character count below which a page is
// considered to have yielded too little to be useful. A real article clears this
// easily; an empty SPA shell does not. Tuned conservatively so we only escalate
// pages that are genuinely near-empty (avoids spending an external call on a
// merely short — but complete — page).
const thinContentThreshold = 400

// escalationSignals are the signals whose presence (together with thin content)
// justifies a headless-render retry. Both mean "the real content is behind JS we
// didn't execute" — precisely what a headless backend fixes. A bot/captcha wall
// is intentionally NOT here: the stealth pipeline already escalates those, and
// re-fetching the origin won't help once it has decided to challenge us.
var escalationSignals = []string{"js_required", "empty_body"}

// jinaFetchFn is the seam for the headless-render backend used by escalation.
// Defaults to jinaFetch (real network); tests swap it to assert the escalation
// decision and re-extraction without hitting r.jina.ai.
var jinaFetchFn = jinaFetch

// shouldEscalateThinContent reports whether a fetch result is thin enough AND
// carries a JS/empty signal, warranting a one-shot headless retry.
func shouldEscalateThinContent(meta *webFetchMeta) bool {
	if meta == nil || meta.ExtractChars >= thinContentThreshold {
		return false
	}
	for _, s := range meta.Signals {
		for _, want := range escalationSignals {
			if s == want {
				return true
			}
		}
	}
	return false
}

// escalateThinContent performs the single headless-render retry. It re-fetches
// targetURL through the Jina backend, re-runs the normal extraction pipeline on
// the rendered bytes, and returns the new content only if it is strictly richer
// than what we already had. On any failure (backend down, still thin) it returns
// ("", false) and the caller keeps the original result — graceful degradation,
// no error propagated to the agent.
//
// This runs at most once per fetch: the caller invokes it a single time and
// never feeds its output back through the predicate.
func escalateThinContent(
	ctx context.Context,
	targetURL string,
	maxBytes int64,
	localAI *LocalAIExtractor,
	meta *webFetchMeta,
) (content string, ok bool) {
	result, err := jinaFetchFn(ctx, targetURL, maxBytes)
	if err != nil || result == nil || len(result.Data) == 0 {
		slog.Debug("thin-content escalation: headless backend yielded nothing",
			"url", targetURL, "error", err)
		return "", false
	}

	rawContent := normalizeCharset(result.Data, result.ContentType)

	// Re-extract through the same pipeline. Jina returns text/plain, so this is
	// usually a passthrough, but routing through processFetchedContent keeps the
	// behavior uniform if the backend ever returns HTML.
	esc := webFetchMeta{
		URL:         meta.URL,
		FinalURL:    meta.FinalURL,
		ContentType: result.ContentType,
		StatusCode:  meta.StatusCode,
		OrigChars:   len(rawContent),
	}
	newContent := processFetchedContent(ctx, rawContent, result.Data, result.ContentType, targetURL, localAI, &esc)

	// Only accept the retry if it actually recovered more content than the
	// original thin extraction; otherwise the original (with its real metadata)
	// is no worse and we avoid swapping in an equally-empty result.
	if len(strings.TrimSpace(newContent)) <= meta.ExtractChars {
		slog.Debug("thin-content escalation: no improvement, keeping original",
			"url", targetURL, "originalChars", meta.ExtractChars, "retryChars", len(newContent))
		return "", false
	}

	// Record that escalation supplied the content so it is observable downstream.
	meta.Signals = appendUnique(meta.Signals, "escalated_headless")
	meta.ExtractChars = len(newContent)
	if esc.WordCount > 0 {
		meta.WordCount = esc.WordCount
	} else {
		meta.WordCount = estimateWordCount(newContent)
	}
	slog.Info("thin-content escalation recovered content via headless backend",
		"url", targetURL, "chars", meta.ExtractChars)
	return newContent, true
}

// jinaFetch fetches targetURL through the Jina Reader proxy (headless render →
// plain text) using the shared SSRF-safe pooled client. It is the production
// backend for both the stealth pipeline's final stage and thin-content
// escalation. Bounded by its own short timeout so a slow proxy can't hang the
// caller's turn.
func jinaFetch(ctx context.Context, targetURL string, maxBytes int64) (*media.FetchResult, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return media.Fetch(fetchCtx, media.FetchOptions{
		URL:      jinaReaderURL(targetURL),
		MaxBytes: maxBytes,
		Headers: map[string]string{
			"User-Agent":      chromeProfile.headers["User-Agent"],
			"Accept":          "text/plain",
			"Accept-Language": chromeProfile.headers["Accept-Language"],
			"Accept-Encoding": "identity",
		},
		Client: SharedClient(30 * time.Second),
	})
}
