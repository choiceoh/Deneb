//go:build no_ffi || !cgo

package ffi

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/coreparsing/htmlmd"
)

var bareURLRe = regexp.MustCompile(`https?://\S+`)
var markdownLinkRe = regexp.MustCompile(`\[[^\]]*\]\(https?://\S+?\)`)

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
// Delegates to the coreparsing/htmlmd tokenizer+emitter (ported from Rust core).
func HtmlToMarkdown(html string) (text string, title string, err error) {
	if len(html) == 0 {
		return "", "", nil
	}
	r := htmlmd.Convert(html)
	return r.Text, r.Title, nil
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

// HtmlToMarkdownStripNoise is a pure-Go fallback with noise element stripping.
// Suppresses nav, aside, svg, iframe, form in addition to script/style/noscript.
func HtmlToMarkdownStripNoise(html string) (text string, title string, err error) {
	if len(html) == 0 {
		return "", "", nil
	}
	r := htmlmd.ConvertWithOpts(html, htmlmd.Options{StripNoise: true})
	return r.Text, r.Title, nil
}

// ParseMediaTokens is a pure-Go fallback for MEDIA: token extraction.
// Respects fenced code blocks (``` or ~~~) — MEDIA lines inside fences are kept as text.
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

	// Track fenced code blocks to avoid extracting MEDIA inside them.
	inFence := false
	var fenceChar byte
	fenceLen := 0

	for _, line := range lines {
		trimmedLine := strings.TrimLeft(line, " \t")

		// Fence tracking: detect opening/closing ``` or ~~~ markers.
		if !inFence {
			if ch, n := detectFenceOpen(trimmedLine); n >= 3 {
				inFence = true
				fenceChar = ch
				fenceLen = n
			}
		} else if isFenceClose(trimmedLine, fenceChar, fenceLen) {
			inFence = false
			keptLines = append(keptLines, line)
			continue
		}

		// Inside a fence — keep line as-is, skip MEDIA extraction.
		if inFence {
			keptLines = append(keptLines, line)
			continue
		}

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

// detectFenceOpen checks if a trimmed line opens a fenced code block.
// Returns the fence character and count, or (0, 0) if not a fence.
func detectFenceOpen(trimmed string) (byte, int) {
	if len(trimmed) < 3 {
		return 0, 0
	}
	ch := trimmed[0]
	if ch != '`' && ch != '~' {
		return 0, 0
	}
	count := 0
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == ch {
			count++
		} else {
			break
		}
	}
	if count >= 3 {
		return ch, count
	}
	return 0, 0
}

// isFenceClose checks if a trimmed line closes a fenced code block.
func isFenceClose(trimmed string, fenceChar byte, fenceLen int) bool {
	if len(trimmed) < fenceLen {
		return false
	}
	count := 0
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] == fenceChar {
			count++
		} else if trimmed[i] == ' ' || trimmed[i] == '\t' {
			break
		} else {
			return false
		}
	}
	return count >= fenceLen
}
