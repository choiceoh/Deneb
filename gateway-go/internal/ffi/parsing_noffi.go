//go:build no_ffi || !cgo

package ffi

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

var bareURLRe = regexp.MustCompile(`https?://\S+`)
var markdownLinkRe = regexp.MustCompile(`\[[^\]]*\]\(https?://\S+?\)`)
var htmlTagRe = regexp.MustCompile(`<[^>]+>`)
var multiSpaceRe = regexp.MustCompile(`[ \t]{2,}`)
var multiNewlineRe = regexp.MustCompile(`\n{3,}`)

// Structural HTML conversion regexps (Go fallback only).
var (
	strongRe = regexp.MustCompile(`(?is)<(?:strong)(?:\s[^>]*)?>(.+?)</(?:strong)>`)
	boldRe   = regexp.MustCompile(`(?is)<b(?:\s[^>]*)?>((?s).+?)</b>`)
	emRe     = regexp.MustCompile(`(?is)<(?:em)(?:\s[^>]*)?>(.+?)</(?:em)>`)
	italicRe = regexp.MustCompile(`(?is)<i(?:\s[^>]*)?>((?s).+?)</i>`)
	codeRe   = regexp.MustCompile(`(?is)<code(?:\s[^>]*)?>(.+?)</code>`)
	linkRe   = regexp.MustCompile(`(?is)<a\s[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	h1Re     = regexp.MustCompile(`(?is)<h1(?:\s[^>]*)?>(.+?)</h1>`)
	h2Re     = regexp.MustCompile(`(?is)<h2(?:\s[^>]*)?>(.+?)</h2>`)
	h3Re     = regexp.MustCompile(`(?is)<h3(?:\s[^>]*)?>(.+?)</h3>`)
	h4Re     = regexp.MustCompile(`(?is)<h4(?:\s[^>]*)?>(.+?)</h4>`)
	h5Re     = regexp.MustCompile(`(?is)<h5(?:\s[^>]*)?>(.+?)</h5>`)
	h6Re     = regexp.MustCompile(`(?is)<h6(?:\s[^>]*)?>(.+?)</h6>`)
	liRe     = regexp.MustCompile(`(?is)<li(?:\s[^>]*)?>(.+?)</li>`)
)

// ExtractLinks is a pure-Go fallback for URL extraction.
func ExtractLinks(text string, maxLinks int) ([]string, error) {
	if len(strings.TrimSpace(text)) == 0 {
		return nil, nil
	}
	if maxLinks <= 0 {
		maxLinks = 5
	}

	// Strip markdown links.
	sanitized := markdownLinkRe.ReplaceAllString(text, " ")

	seen := make(map[string]bool)
	var results []string
	for _, match := range bareURLRe.FindAllString(sanitized, -1) {
		raw := strings.TrimSpace(match)
		if raw == "" || seen[raw] {
			continue
		}
		if !IsSafeURL(raw) {
			continue
		}
		seen[raw] = true
		results = append(results, raw)
		if len(results) >= maxLinks {
			break
		}
	}
	return results, nil
}

// HtmlToMarkdown is a pure-Go fallback for HTML to Markdown conversion.
func HtmlToMarkdown(html string) (text string, title string, err error) {
	if len(html) == 0 {
		return "", "", nil
	}

	// Extract title.
	titleStart := strings.Index(strings.ToLower(html), "<title")
	if titleStart >= 0 {
		gtIdx := strings.Index(html[titleStart:], ">")
		if gtIdx >= 0 {
			afterTag := titleStart + gtIdx + 1
			endIdx := strings.Index(strings.ToLower(html[afterTag:]), "</title>")
			if endIdx >= 0 {
				raw := html[afterTag : afterTag+endIdx]
				title = strings.TrimSpace(htmlTagRe.ReplaceAllString(raw, ""))
			}
		}
	}

	// Strip script/style/noscript.
	result := html
	for _, tag := range []string{"script", "style", "noscript"} {
		open := "<" + tag
		close := "</" + tag + ">"
		for {
			lower := strings.ToLower(result)
			start := strings.Index(lower, open)
			if start < 0 {
				break
			}
			end := strings.Index(lower[start:], close)
			if end < 0 {
				result = result[:start]
				break
			}
			result = result[:start] + result[start+end+len(close):]
		}
	}

	// Convert structural elements to markdown before stripping all tags.
	// Links: <a href="X">Y</a> → [Y](X)
	result = linkRe.ReplaceAllString(result, "[$2]($1)")
	// Bold: <strong>/<b> → **...**
	result = strongRe.ReplaceAllString(result, "**$1**")
	result = boldRe.ReplaceAllString(result, "**$1**")
	// Italic: <em>/<i> → *...*
	result = emRe.ReplaceAllString(result, "*$1*")
	result = italicRe.ReplaceAllString(result, "*$1*")
	// Inline code: <code> → `...`
	result = codeRe.ReplaceAllString(result, "`$1`")
	// Headings: <h1-6> → # prefix
	result = h1Re.ReplaceAllString(result, "\n# $1\n")
	result = h2Re.ReplaceAllString(result, "\n## $1\n")
	result = h3Re.ReplaceAllString(result, "\n### $1\n")
	result = h4Re.ReplaceAllString(result, "\n#### $1\n")
	result = h5Re.ReplaceAllString(result, "\n##### $1\n")
	result = h6Re.ReplaceAllString(result, "\n###### $1\n")
	// List items: <li> → "- "
	result = liRe.ReplaceAllString(result, "\n- $1")

	// Strip remaining tags.
	result = htmlTagRe.ReplaceAllString(result, " ")

	// Decode basic entities.
	result = strings.ReplaceAll(result, "&nbsp;", " ")
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	result = strings.ReplaceAll(result, "&#39;", "'")
	result = strings.ReplaceAll(result, "&apos;", "'")

	// Normalize whitespace.
	result = multiSpaceRe.ReplaceAllString(result, " ")
	result = multiNewlineRe.ReplaceAllString(result, "\n\n")
	result = strings.TrimSpace(result)

	return result, title, nil
}

// Base64Estimate is a pure-Go fallback for base64 decoded size estimation.
func Base64Estimate(input string) (int64, error) {
	if len(input) == 0 {
		return 0, nil
	}

	effectiveLen := 0
	for i := 0; i < len(input); i++ {
		if input[i] > 0x20 {
			effectiveLen++
		}
	}
	if effectiveLen == 0 {
		return 0, nil
	}

	// Detect padding from end.
	padding := 0
	end := len(input) - 1
	for end >= 0 && input[end] <= 0x20 {
		end--
	}
	if end >= 0 && input[end] == '=' {
		padding = 1
		end--
		for end >= 0 && input[end] <= 0x20 {
			end--
		}
		if end >= 0 && input[end] == '=' {
			padding = 2
		}
	}

	estimated := (effectiveLen * 3) / 4
	estimated -= padding
	if estimated < 0 {
		estimated = 0
	}
	return int64(estimated), nil
}

// Base64Canonicalize is a pure-Go fallback for base64 validation.
func Base64Canonicalize(input string) (string, error) {
	if len(input) == 0 {
		return "", errors.New("ffi: base64_canonicalize: empty input")
	}

	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		b.WriteByte(c)
	}
	cleaned := b.String()

	if len(cleaned) == 0 || len(cleaned)%4 != 0 {
		return "", errors.New("ffi: base64_canonicalize: invalid base64")
	}

	// Validate using stdlib.
	_, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", errors.New("ffi: base64_canonicalize: invalid base64")
	}

	return cleaned, nil
}

// HtmlToMarkdownStripNoise is a pure-Go fallback.
// In no-FFI mode, noise stripping is not applied (the Go caller should
// pre-strip noise elements via StripNoiseElements before calling).
func HtmlToMarkdownStripNoise(html string) (text string, title string, err error) {
	return HtmlToMarkdown(html)
}

// ParseMediaTokens is a pure-Go fallback for MEDIA: token extraction.
func ParseMediaTokens(text string) (cleanText string, mediaURLs []string, audioAsVoice bool, err error) {
	if len(text) == 0 {
		return "", nil, false, nil
	}

	trimmed := strings.TrimRight(text, " \t\n\r")
	if !strings.Contains(strings.ToLower(trimmed), "media:") && !strings.Contains(trimmed, "[[") {
		return trimmed, nil, false, nil
	}

	lines := strings.Split(trimmed, "\n")
	var keptLines []string
	var media []string

	for _, line := range lines {
		trimmedLine := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmedLine, "MEDIA:") {
			keptLines = append(keptLines, line)
			continue
		}

		payload := strings.TrimSpace(trimmedLine[6:])
		payload = strings.Trim(payload, "`")
		payload = strings.TrimSpace(payload)

		if payload == "" {
			keptLines = append(keptLines, line)
			continue
		}

		valid := false
		for _, part := range strings.Fields(payload) {
			candidate := strings.Trim(part, "`\"'[]{}(),\\")
			if strings.HasPrefix(candidate, "file://") {
				candidate = candidate[7:]
			}
			if isValidMediaGo(candidate) {
				media = append(media, candidate)
				valid = true
			}
		}

		if !valid {
			// Try whole payload as path.
			candidate := strings.Trim(payload, "`\"'[]{}(),\\")
			if strings.HasPrefix(candidate, "file://") {
				candidate = candidate[7:]
			}
			if isValidMediaPathGo(candidate) {
				media = append(media, candidate)
				valid = true
			}
		}

		// If valid, strip the line; otherwise keep it.
		if !valid {
			keptLines = append(keptLines, line)
		}
	}

	result := strings.Join(keptLines, "\n")

	// Strip [[audio_as_voice]].
	if idx := strings.Index(result, "[[audio_as_voice]]"); idx >= 0 {
		audioAsVoice = true
		result = result[:idx] + result[idx+18:]
	}

	result = strings.TrimSpace(result)
	return result, media, audioAsVoice, nil
}

func isValidMediaGo(candidate string) bool {
	if candidate == "" || len(candidate) > 4096 {
		return false
	}
	if strings.ContainsAny(candidate, " \t\n\r") {
		return false
	}
	if strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
		return true
	}
	return isValidMediaPathGo(candidate)
}

func isValidMediaPathGo(candidate string) bool {
	if strings.HasPrefix(candidate, "/") ||
		strings.HasPrefix(candidate, "./") ||
		strings.HasPrefix(candidate, "../") ||
		strings.HasPrefix(candidate, "~") ||
		strings.HasPrefix(candidate, "\\\\") {
		return true
	}
	if len(candidate) >= 3 &&
		((candidate[0] >= 'a' && candidate[0] <= 'z') || (candidate[0] >= 'A' && candidate[0] <= 'Z')) &&
		candidate[1] == ':' &&
		(candidate[2] == '\\' || candidate[2] == '/') {
		return true
	}
	return false
}
