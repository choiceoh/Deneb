// web_fetch.go — Agent-oriented web content extraction tool.
//
// Designed for AI agent consumption, not human browsing. Key principles:
//   - Structured metadata (title, final URL, language, publish date, signals)
//   - Machine-readable errors with classification codes
//   - Aggressive noise removal (nav, ads, cookie banners, comments)
//   - Intelligent truncation with content density awareness
//   - Optional SGLang AI-powered extraction for HTML content
//   - Quality signals (login wall, JS-required, bot detection)
package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/media"
)

// --- Tool schema ---

func webFetchToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "HTTP or HTTPS URL to fetch",
			},
			"maxChars": map[string]any{
				"type":        "number",
				"description": "Maximum content characters to return (default: 50000)",
			},
		},
		"required": []string{"url"},
	}
}

// --- Structured output types ---

// webFetchMeta holds machine-readable metadata about the fetched page.
type webFetchMeta struct {
	Title        string   `json:"title,omitempty"`
	URL          string   `json:"url"`
	FinalURL     string   `json:"final_url,omitempty"`
	CanonicalURL string   `json:"canonical_url,omitempty"`
	Description  string   `json:"description,omitempty"`
	Language     string   `json:"language,omitempty"`
	Published    string   `json:"published,omitempty"`
	ContentType  string   `json:"content_type"`
	StatusCode   int      `json:"status_code"`
	OrigChars    int      `json:"original_chars"`
	ExtractChars int      `json:"extracted_chars"`
	Retention    string   `json:"retention_ratio"`
	Truncated    bool     `json:"truncated"`
	Signals      []string `json:"signals,omitempty"`
}

// webFetchErr is a machine-readable error for agent consumption.
type webFetchErr struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	URL       string `json:"url"`
	Retryable bool   `json:"retryable"`
}

// --- Tool implementation ---

func toolWebFetch(cache *FetchCache, sglang *sglangExtractor) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL      string `json:"url"`
			MaxChars int    `json:"maxChars"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return formatFetchError(webFetchErr{
				Code: "invalid_params", Message: err.Error(), Retryable: false,
			}), nil
		}
		if p.URL == "" {
			return formatFetchError(webFetchErr{
				Code: "missing_url", Message: "url is required", Retryable: false,
			}), nil
		}

		maxChars := 50000
		if p.MaxChars > 0 {
			maxChars = p.MaxChars
		}

		// YouTube → delegate to transcript extraction.
		if media.IsYouTubeURL(p.URL) {
			ytCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			result, err := media.ExtractYouTubeTranscript(ytCtx, p.URL)
			if err != nil {
				return formatFetchError(webFetchErr{
					Code: "youtube_failed", Message: err.Error(),
					URL: p.URL, Retryable: true,
				}), nil
			}
			return media.FormatYouTubeResult(result), nil
		}

		// Cache lookup.
		if cached, ok := cache.Get(p.URL); ok {
			return truncateResult(cached, maxChars), nil
		}

		// Size limit: 2× maxChars raw bytes, capped at 5 MB.
		maxBytes := int64(maxChars * 2)
		if maxBytes > 5*1024*1024 {
			maxBytes = 5 * 1024 * 1024
		}

		// Fetch with retry.
		result, err := fetchWithRetry(ctx, p.URL, maxBytes)
		if err != nil {
			return formatFetchError(classifyFetchError(err, p.URL)), nil
		}

		rawContent := string(result.Data)
		origChars := len(rawContent)

		meta := webFetchMeta{
			URL:         p.URL,
			FinalURL:    result.FinalURL,
			ContentType: result.ContentType,
			StatusCode:  result.StatusCode,
			OrigChars:   origChars,
		}

		var content string
		isHTML := strings.Contains(result.ContentType, "text/html") ||
			strings.Contains(result.ContentType, "application/xhtml")

		if isHTML {
			// Extract metadata from raw HTML before conversion.
			extractHTMLMeta(rawContent, &meta)

			// Detect quality signals from raw HTML.
			meta.Signals = detectSignals(rawContent)

			// Convert HTML to Markdown.
			// Try SGLang AI extraction first for superior noise removal.
			if sglang.available() {
				extracted, err := sglang.extract(ctx, rawContent, p.URL)
				if err != nil {
					slog.Warn("sglang extraction failed, falling back to FFI",
						"url", p.URL, "error", err)
					content = ffiConvert(rawContent)
				} else {
					content = extracted
				}
			} else {
				content = ffiConvert(rawContent)
			}
		} else {
			content = rawContent
		}

		meta.ExtractChars = len(content)
		if origChars > 0 {
			ratio := float64(meta.ExtractChars) / float64(origChars) * 100
			meta.Retention = fmt.Sprintf("%.1f%%", ratio)
		} else {
			meta.Retention = "0%"
		}

		// Detect empty-after-extraction signal.
		if isHTML && len(strings.TrimSpace(content)) < 100 && origChars > 1000 {
			meta.Signals = appendUnique(meta.Signals, "low_content_yield")
		}

		// Build full result (metadata + content) and cache before truncation.
		fullResult := formatFetchResult(meta, content)
		cache.Put(p.URL, fullResult)

		return truncateResult(fullResult, maxChars), nil
	}
}

// --- Fetch with retry ---

// fetchWithRetry fetches a URL with retry on transient errors (5xx, timeouts).
// Max 3 attempts with short backoff: 0ms, 500ms, 1500ms.
func fetchWithRetry(ctx context.Context, url string, maxBytes int64) (*media.FetchResult, error) {
	backoff := [3]time.Duration{0, 500 * time.Millisecond, 1500 * time.Millisecond}

	var lastErr error
	for attempt := 0; attempt < len(backoff); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff[attempt]):
			}
		}

		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := media.Fetch(fetchCtx, media.FetchOptions{
			URL:      url,
			MaxBytes: maxBytes,
			Headers: map[string]string{
				"User-Agent": "Deneb-Gateway/1.0",
				"Accept":     "text/html,text/plain,application/json,*/*",
			},
		})
		cancel()

		if err == nil {
			return result, nil
		}
		lastErr = err

		if !isRetryableError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// isRetryableError returns true for transient errors worth retrying.
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

// --- FFI HTML→Markdown conversion ---

func ffiConvert(html string) string {
	text, _, err := ffi.HtmlToMarkdown(html)
	if err != nil {
		slog.Warn("ffi html-to-markdown failed", "error", err)
		return html
	}
	return text
}

// --- HTML metadata extraction ---

var (
	ogTitleRe     = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
	ogTitleRevRe  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:title["']`)
	ogDescRe      = regexp.MustCompile(`(?i)<meta[^>]+property=["']og:description["'][^>]+content=["']([^"']+)["']`)
	ogDescRevRe   = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']og:description["']`)
	metaDescRe    = regexp.MustCompile(`(?i)<meta[^>]+name=["']description["'][^>]+content=["']([^"']+)["']`)
	metaDescRevRe = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+name=["']description["']`)
	canonicalRe   = regexp.MustCompile(`(?i)<link[^>]+rel=["']canonical["'][^>]+href=["']([^"']+)["']`)
	canonicalRevRe = regexp.MustCompile(`(?i)<link[^>]+href=["']([^"']+)["'][^>]+rel=["']canonical["']`)
	htmlLangRe    = regexp.MustCompile(`(?i)<html[^>]+lang=["']([^"']+)["']`)
	publishRe     = regexp.MustCompile(`(?i)<meta[^>]+(?:property=["']article:published_time["']|name=["'](?:date|publish[_-]?date|DC\.date)["'])[^>]+content=["']([^"']+)["']`)
	publishRevRe  = regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+(?:property=["']article:published_time["']|name=["'](?:date|publish[_-]?date|DC\.date)["'])`)
	titleTagRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// extractHTMLMeta parses HTML meta tags into the metadata struct.
// Only reads the first ~8K to avoid scanning huge documents.
func extractHTMLMeta(html string, meta *webFetchMeta) {
	// Limit scan to head section (typically < 8K).
	scan := html
	if len(scan) > 8192 {
		scan = scan[:8192]
	}

	// Title: prefer OG, fallback to <title>.
	if m := ogTitleRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Title = m[1]
	} else if m := ogTitleRevRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Title = m[1]
	} else if m := titleTagRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Title = strings.TrimSpace(m[1])
	}

	// Description: prefer OG, fallback to meta name="description".
	if m := ogDescRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Description = m[1]
	} else if m := ogDescRevRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Description = m[1]
	} else if m := metaDescRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Description = m[1]
	} else if m := metaDescRevRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Description = m[1]
	}

	// Canonical URL.
	if m := canonicalRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.CanonicalURL = m[1]
	} else if m := canonicalRevRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.CanonicalURL = m[1]
	}

	// Language.
	if m := htmlLangRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Language = m[1]
	}

	// Publish date.
	if m := publishRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Published = m[1]
	} else if m := publishRevRe.FindStringSubmatch(scan); len(m) > 1 {
		meta.Published = m[1]
	}
}

// --- Quality signal detection ---

var (
	loginWallPatterns = []string{
		"login-wall", "paywall", "sign-in-gate", "subscribe-wall",
		"registration-wall", "loginRequired", "login_required",
		"metered-content", "premium-content",
	}
	jsRequiredPatterns = []string{
		"you need to enable javascript",
		"this page requires javascript",
		"please enable javascript",
		"javascript is required",
		"noscript",
	}
	botBlockPatterns = []string{
		"access denied", "blocked by cloudflare",
		"captcha", "are you a robot", "unusual traffic",
		"please verify you are a human",
	}
)

func detectSignals(html string) []string {
	lower := strings.ToLower(html)
	var signals []string

	for _, p := range loginWallPatterns {
		if strings.Contains(lower, p) {
			signals = append(signals, "login_wall_detected")
			break
		}
	}

	// Check for JS-required only if <noscript> contains a substantial message
	// or if the body is mostly empty but has JS framework indicators.
	for _, p := range jsRequiredPatterns {
		if strings.Contains(lower, p) {
			signals = append(signals, "js_required")
			break
		}
	}

	for _, p := range botBlockPatterns {
		if strings.Contains(lower, p) {
			signals = append(signals, "bot_blocked")
			break
		}
	}

	// Empty body detection: large HTML but little visible text.
	bodyIdx := strings.Index(lower, "<body")
	if bodyIdx >= 0 {
		body := lower[bodyIdx:]
		// Count non-tag characters roughly.
		textLen := 0
		inTag := false
		for _, r := range body {
			if r == '<' {
				inTag = true
			} else if r == '>' {
				inTag = false
			} else if !inTag && r > ' ' {
				textLen++
			}
		}
		if len(body) > 5000 && textLen < 200 {
			signals = appendUnique(signals, "empty_body")
		}
	}

	return signals
}

// --- SGLang AI-powered content extraction ---

// sglangExtractor calls a local SGLang server for intelligent content extraction.
type sglangExtractor struct {
	once       sync.Once
	client     *http.Client
	baseURL    string
	apiKey     string
	model      string
	isReady    bool
}

func newSGLangExtractor() *sglangExtractor {
	baseURL := os.Getenv("SGLANG_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:30000/v1"
	}
	apiKey := os.Getenv("SGLANG_API_KEY")
	if apiKey == "" {
		apiKey = "local"
	}
	model := os.Getenv("SGLANG_MODEL")
	if model == "" {
		model = "Qwen/Qwen3.5-35B-A3B"
	}
	return &sglangExtractor{
		client:  &http.Client{Timeout: 60 * time.Second},
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
	}
}

// available returns true if SGLang server is reachable.
// Probes the server once on first call, then caches the result.
func (s *sglangExtractor) available() bool {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/models", nil)
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
		resp, err := s.client.Do(req)
		if err != nil {
			slog.Info("sglang not available", "url", s.baseURL, "error", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			s.isReady = true
			slog.Info("sglang available", "url", s.baseURL, "model", s.model)
		}
	})
	return s.isReady
}

const sglangExtractionPrompt = `You are a web content extractor for AI agents. Given raw HTML-converted-to-markdown content from a webpage, extract ONLY the main article/content.

Rules:
1. REMOVE: navigation menus, headers, footers, sidebars, advertisements, cookie banners, "related articles" sections, comment sections, social media widgets, breadcrumbs, pagination
2. PRESERVE: main article text, tables (as markdown tables), code blocks, lists, images references, headings hierarchy
3. MAINTAIN the original markdown structure — headings stay as headings, tables stay as tables, lists stay as lists
4. Do NOT add commentary, summaries, or explanations — return only the extracted content
5. If the content is already clean, return it as-is
6. Output in the same language as the source content`

// extract calls SGLang to intelligently extract main content from HTML.
func (s *sglangExtractor) extract(ctx context.Context, html string, url string) (string, error) {
	// First convert HTML to markdown via FFI to reduce token usage.
	mdContent := ffiConvert(html)

	// If content is small enough, AI extraction adds little value.
	if len(mdContent) < 2000 {
		return mdContent, nil
	}

	// Limit input to avoid overwhelming the model.
	// Qwen3.5-35B-A3B has 262K context, but we cap at ~100K chars for extraction.
	inputLimit := 100000
	if len(mdContent) > inputLimit {
		mdContent = mdContent[:inputLimit]
	}

	userMsg := fmt.Sprintf("URL: %s\n\nContent:\n%s", url, mdContent)

	reqBody := map[string]any{
		"model": s.model,
		"messages": []map[string]string{
			{"role": "system", "content": sglangExtractionPrompt},
			{"role": "user", "content": userMsg},
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

	// Strip thinking tags if present (Qwen models with enable_thinking).
	extracted = stripThinkingTags(extracted)

	return strings.TrimSpace(extracted), nil
}

// stripThinkingTags removes <think>...</think> blocks from Qwen model output.
var thinkingTagRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

func stripThinkingTags(s string) string {
	return thinkingTagRe.ReplaceAllString(s, "")
}

// --- Error classification ---

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
				Code:      "content_too_large",
				Message:   mfe.Message,
				URL:       url,
				Retryable: false,
			}
		case media.ErrFetchFailed:
			code := "fetch_failed"
			msg := mfe.Message
			retryable := true
			if strings.Contains(msg, "SSRF") {
				code = "ssrf_blocked"
				retryable = false
			} else if strings.Contains(msg, "no such host") || strings.Contains(msg, "no addresses") {
				code = "dns_failure"
				retryable = false
			} else if strings.Contains(msg, "too many redirects") {
				code = "redirect_loop"
				retryable = false
			}
			return webFetchErr{Code: code, Message: msg, URL: url, Retryable: retryable}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return webFetchErr{
			Code: "timeout", Message: "request timed out", URL: url, Retryable: true,
		}
	}
	return webFetchErr{
		Code: "unknown", Message: err.Error(), URL: url, Retryable: false,
	}
}

// --- Output formatting ---

func formatFetchResult(meta webFetchMeta, content string) string {
	var b strings.Builder

	b.WriteString("<metadata>\n")
	if meta.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", meta.Title)
	}
	if meta.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", meta.Description)
	}
	fmt.Fprintf(&b, "URL: %s\n", meta.URL)
	if meta.FinalURL != "" && meta.FinalURL != meta.URL {
		fmt.Fprintf(&b, "FinalURL: %s\n", meta.FinalURL)
	}
	if meta.CanonicalURL != "" {
		fmt.Fprintf(&b, "Canonical: %s\n", meta.CanonicalURL)
	}
	if meta.Language != "" {
		fmt.Fprintf(&b, "Language: %s\n", meta.Language)
	}
	if meta.Published != "" {
		fmt.Fprintf(&b, "Published: %s\n", meta.Published)
	}
	fmt.Fprintf(&b, "ContentType: %s\n", meta.ContentType)
	fmt.Fprintf(&b, "StatusCode: %d\n", meta.StatusCode)
	fmt.Fprintf(&b, "ContentChars: %d (original: %d, retention: %s)\n",
		meta.ExtractChars, meta.OrigChars, meta.Retention)
	if meta.Truncated {
		b.WriteString("Truncated: true\n")
	}
	if len(meta.Signals) > 0 {
		fmt.Fprintf(&b, "Signals: %s\n", strings.Join(meta.Signals, ", "))
	}
	b.WriteString("</metadata>\n<content>\n")
	b.WriteString(content)
	b.WriteString("\n</content>")

	return b.String()
}

func formatFetchError(e webFetchErr) string {
	var b strings.Builder
	b.WriteString("<error>\n")
	fmt.Fprintf(&b, "Code: %s\n", e.Code)
	fmt.Fprintf(&b, "Message: %s\n", e.Message)
	if e.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", e.URL)
	}
	fmt.Fprintf(&b, "Retryable: %v\n", e.Retryable)
	b.WriteString("</error>")
	return b.String()
}

// truncateResult truncates a formatted result to maxChars, preserving the
// metadata section. If truncation happens, inserts a truncation marker.
func truncateResult(result string, maxChars int) string {
	if len(result) <= maxChars {
		return result
	}
	// Ensure we keep the metadata section intact.
	metaEnd := strings.Index(result, "</metadata>")
	if metaEnd >= 0 && metaEnd < maxChars {
		// Truncate only the content portion.
		truncated := result[:maxChars]
		return truncated + "\n\n[...truncated at " + strconv.Itoa(maxChars) + " chars]\n</content>"
	}
	return result[:maxChars] + "\n[...truncated]"
}

// --- Helpers ---

func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
}
