package web

import (
	"context"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
)

// processHTML runs the full HTML extraction pipeline:
// 1. Extract metadata from raw HTML
// 2. Detect quality signals
// 3. Strip noise elements (nav, aside, footer, ads, cookie banners)
// 4. Convert to Markdown (SGLang AI or FFI fallback)
func processHTML(ctx context.Context, html string, url string, sglang *SGLangExtractor, meta *webFetchMeta) string {
	// Step 1: Extract metadata from raw HTML (before any stripping).
	extractHTMLMeta(html, meta)

	// Step 2: Detect quality signals from raw HTML.
	meta.Signals = detectSignals(html)

	// Step 3: Strip noise elements — the critical preprocessing step.
	// This removes nav, aside, footer, ads, cookie banners, comments, etc.
	// Even when SGLang is available, pre-stripping reduces input tokens
	// and prevents noise from confusing the AI extraction.
	cleaned := StripNoiseElements(html)

	// Step 4: Convert to Markdown.
	var content string
	if sglang.available() {
		extracted, err := sglang.extract(ctx, cleaned, url, meta.Language)
		if err != nil {
			slog.Warn("sglang extraction failed, falling back to FFI",
				"url", url, "error", err)
			content = ffiConvert(cleaned)
		} else {
			content = extracted
		}
	} else {
		content = ffiConvert(cleaned)
	}

	// Step 5: Post-extraction quality check.
	trimmedLen := len(strings.TrimSpace(content))
	if trimmedLen < 100 && meta.OrigChars > 1000 {
		meta.Signals = appendUnique(meta.Signals, "low_content_yield")
	}

	return content
}

// ffiConvert performs FFI-backed HTML -> Markdown conversion.
func ffiConvert(html string) string {
	text, _, err := ffi.HtmlToMarkdown(html)
	if err != nil {
		slog.Warn("ffi html-to-markdown failed", "error", err)
		return html
	}
	return text
}
