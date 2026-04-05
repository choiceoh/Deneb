// web_html.go — HTML → text conversion: FFI, local AI extraction pipeline.
//
// processHTML orchestrates the full extraction pipeline for HTML content.
// ffiConvert is the FFI-backed HTML→Markdown baseline.
// LocalAIExtractor is the optional AI-powered extraction layer (Qwen via local AI).
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
// 4. Convert to Markdown (local AI or FFI fallback)
func processHTML(ctx context.Context, html string, url string, localAI *LocalAIExtractor, meta *webFetchMeta) string {
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
	if localAI.available() {
		extracted, err := localAI.extract(ctx, cleaned, url, meta.Language)
		if err != nil {
			slog.Warn("localai extraction failed, falling back to FFI",
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

// --- local AI-powered content extraction ---

type LocalAIExtractor struct {
	mu      sync.Mutex
	client  *http.Client
	baseURL string
	apiKey  string
	model   string
	state   int // 0=unknown, 1=available, -1=unavailable
	probeAt time.Time
}

const (
	localAIUnknown     = 0
	localAIAvailable   = 1
	localAIUnavailable = -1
	// Re-probe interval when previously unavailable.
	localAIReprobeInterval = 5 * time.Minute
)

func NewLocalAIExtractor() *LocalAIExtractor {
	baseURL := firstEnv("LOCAL_AI_BASE_URL", "SGLANG_BASE_URL")
	if baseURL == "" {
		baseURL = modelrole.DefaultVllmBaseURL
	}
	apiKey := firstEnv("LOCAL_AI_API_KEY", "SGLANG_API_KEY")
	if apiKey == "" {
		apiKey = "local"
	}
	model := firstEnv("LOCAL_AI_MODEL", "SGLANG_MODEL")
	if model == "" {
		model = modelrole.DefaultVllmModel
	}
	return &LocalAIExtractor{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}
}

// available checks if local AI is reachable. Probes on first call,
// then re-probes periodically if previously unavailable.
func (s *LocalAIExtractor) available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == localAIAvailable {
		return true
	}
	if s.state == localAIUnavailable && time.Since(s.probeAt) < localAIReprobeInterval {
		return false
	}

	// Probe the server.
	s.probeAt = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/models", nil)
	if err != nil {
		s.state = localAIUnavailable
		return false
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		slog.Info("localai not available", "url", s.baseURL, "error", err)
		s.state = localAIUnavailable
		return false
	}
	resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.state = localAIAvailable
		slog.Info("localai available", "url", s.baseURL, "model", s.model)
		return true
	}
	s.state = localAIUnavailable
	return false
}

const localAISystemPrompt = `You are a precision web content extractor for AI agents. Your output becomes the agent's sole understanding of the webpage.

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

// extract calls local AI for intelligent content extraction from pre-cleaned HTML.
func (s *LocalAIExtractor) extract(ctx context.Context, html string, url string, language string) (string, error) {
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
			{"role": "system", "content": localAISystemPrompt},
			{"role": "user", "content": userMsg.String()},
		},
		"max_tokens":  16384,
		"temperature": 0,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal local AI request: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create local AI request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("localai request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("localai HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode local AI response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("localai returned no choices")
	}

	extracted := result.Choices[0].Message.Content
	extracted = jsonutil.StripThinkingTags(extracted)

	return strings.TrimSpace(extracted), nil
}

// firstEnv returns the first non-empty environment variable value.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
