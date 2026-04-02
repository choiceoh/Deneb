// web_html.go — HTML → text conversion: FFI, SGLang AI extraction pipeline.
//
// processHTML orchestrates the full extraction pipeline for HTML content.
// ffiConvert is the FFI-backed HTML→Markdown baseline.
// SGLangExtractor is the optional AI-powered extraction layer (Qwen via SGLang).
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
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

	// Step 3: Strip noise elements.
	// Tag-level noise (nav, aside, svg, iframe, form) is handled by Rust via
	// strip_noise option. Class/ID-based noise (cookie banners, ads, sidebars,
	// comments) is still handled by Go's StripNoiseElements.
	cleaned := StripNoiseElements(html)

	// Step 4: Convert to Markdown.
	var content string
	if sglang.available() {
		extracted, err := sglang.extract(ctx, cleaned, url, meta.Language)
		if err != nil {
			slog.Warn("sglang extraction failed, falling back to FFI",
				"url", url, "error", err)
			content = ffiConvertStripNoise(cleaned)
		} else {
			content = extracted
		}
	} else {
		content = ffiConvertStripNoise(cleaned)
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

// ffiConvertStripNoise performs FFI-backed HTML -> Markdown with noise stripping.
// Suppresses nav, aside, svg, iframe, form elements at the Rust level.
func ffiConvertStripNoise(html string) string {
	text, _, err := ffi.HtmlToMarkdownStripNoise(html)
	if err != nil {
		slog.Warn("ffi html-to-markdown-strip-noise failed", "error", err)
		return ffiConvert(html)
	}
	return text
}

// --- SGLang AI-powered content extraction ---

type SGLangExtractor struct {
	mu      sync.Mutex
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
	state   int // 0=unknown, 1=available, -1=unavailable
	probeAt time.Time
}

const (
	sglangUnknown     = 0
	sglangAvailable   = 1
	sglangUnavailable = -1
	// Re-probe interval when previously unavailable.
	sglangReprobeInterval = 5 * time.Minute
)

func NewSGLangExtractor() *SGLangExtractor {
	baseURL := os.Getenv("SGLANG_BASE_URL")
	if baseURL == "" {
		baseURL = modelrole.DefaultSglangBaseURL
	}
	apiKey := os.Getenv("SGLANG_API_KEY")
	if apiKey == "" {
		apiKey = "local"
	}
	model := os.Getenv("SGLANG_MODEL")
	if model == "" {
		model = modelrole.DefaultSglangModel
	}
	return &SGLangExtractor{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}
}

// available checks if SGLang is reachable. Probes on first call,
// then re-probes periodically if previously unavailable.
func (s *SGLangExtractor) available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == sglangAvailable {
		return true
	}
	if s.state == sglangUnavailable && time.Since(s.probeAt) < sglangReprobeInterval {
		return false
	}

	// Probe the server.
	s.probeAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/models", nil)
	if err != nil {
		s.state = sglangUnavailable
		return false
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		slog.Info("sglang not available", "url", s.baseURL, "error", err)
		s.state = sglangUnavailable
		return false
	}
	resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.state = sglangAvailable
		slog.Info("sglang available", "url", s.baseURL, "model", s.model)
		return true
	}
	s.state = sglangUnavailable
	return false
}

const sglangSystemPrompt = `You are a precision web content extractor for AI agents. Your output becomes the agent's sole understanding of the webpage.

REMOVE completely:
- Navigation menus, breadcrumbs, pagination elements
- Cookie banners, GDPR notices, consent dialogs
- Advertisement blocks, sponsored content, promotional banners
- "Related articles", "You might also like", "Trending" sections
- Comment sections, user reviews (unless they ARE the main content)
- Social media share buttons, follow widgets
- Site-wide headers, footers, copyright notices
- Search bars, login forms, newsletter signup forms
- Sidebar widgets, tag clouds, archive links

PRESERVE with structure:
- Main article/page body text — this is the primary output
- Headings hierarchy (# through ######) exactly as structured
- Data tables as proper markdown tables with alignment
- Code blocks with language tags (` + "```" + `lang ... ` + "```" + `)
- Ordered and unordered lists with proper nesting
- Blockquotes with > prefix
- Image references as ![alt](url) when informational
- Inline links [text](url) when they add value

RULES:
- Output ONLY the extracted content — no wrapping, no commentary
- Preserve the source language exactly
- If content is already clean, return it unchanged
- Empty extraction is better than including noise`

// extract calls SGLang for intelligent content extraction from pre-cleaned HTML.
func (s *SGLangExtractor) extract(ctx context.Context, html string, url string, language string) (string, error) {
	// Convert HTML to markdown via FFI first to reduce token count.
	mdContent := ffiConvert(html)

	// Small content: AI adds little value, return directly.
	if len(mdContent) < 2000 {
		return mdContent, nil
	}

	// Cap input to ~100K chars (well within Qwen 262K context).
	if len(mdContent) > 100000 {
		mdContent = mdContent[:100000]
	}

	// Build user message with context hints.
	var userMsg strings.Builder
	fmt.Fprintf(&userMsg, "URL: %s\n", url)
	if language != "" {
		fmt.Fprintf(&userMsg, "Language: %s\n", language)
	}
	userMsg.WriteString("\n---\n")
	userMsg.WriteString(mdContent)

	reqBody := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": sglangSystemPrompt},
			{"role": "user", "content": userMsg.String()},
		},
		"max_tokens":  16384,
		"temperature": 0,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal sglang request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create sglang request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sglang request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("sglang HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode sglang response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("sglang returned no choices")
	}

	extracted := result.Choices[0].Message.Content
	extracted = jsonutil.StripThinkingTags(extracted)

	return strings.TrimSpace(extracted), nil
}
