// Package urlextract extracts safe URLs from message text.
//
// Ported from core-rs/core/src/parsing/url_extract.rs.
// Strips markdown link syntax, extracts bare http/https/ftp URLs,
// deduplicates, SSRF-checks each via coresecurity.IsSafeURL, and
// limits to a configurable maximum.
package urlextract

import (
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/coresecurity"
)

// DefaultMaxLinks is the default maximum number of links to extract.
const DefaultMaxLinks = 5

// extraSchemes are recognized for URL extraction beyond http/https.
var extraSchemes = []string{"ftp://"}

// ExtractLinks extracts safe URLs from text, stripping markdown link syntax
// first. Returns a deduplicated list of safe URLs, up to maxLinks.
func ExtractLinks(text string, maxLinks int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if maxLinks <= 0 {
		maxLinks = DefaultMaxLinks
	}

	sanitized := stripMarkdownLinks(trimmed)
	seen := make(map[string]struct{})
	var results []string

	for _, raw := range findBareURLs(sanitized) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !isAllowedURL(raw) {
			continue
		}
		if _, exists := seen[raw]; exists {
			continue
		}
		seen[raw] = struct{}{}
		results = append(results, raw)
		if len(results) >= maxLinks {
			break
		}
	}
	return results
}

// stripMarkdownLinks replaces [text](url) patterns with a space so the URL
// inside the parens is NOT picked up as a bare link. Handles nested brackets
// and preserves multi-byte UTF-8 sequences (Korean, emoji).
func stripMarkdownLinks(input string) string {
	bytes := []byte(input)
	length := len(bytes)
	var out strings.Builder
	out.Grow(length)
	i := 0

	for i < length {
		if bytes[i] == '[' {
			if end, ok := matchMarkdownLink(bytes, i); ok {
				out.WriteByte(' ')
				i = end
				continue
			}
		}
		// Advance past the full UTF-8 character.
		_, size := utf8.DecodeRune(bytes[i:])
		out.Write(bytes[i : i+size])
		i += size
	}
	return out.String()
}

// matchMarkdownLink tries to match [...](...) starting at start.
// Returns (index past closing ')', true) on success.
func matchMarkdownLink(b []byte, start int) (int, bool) {
	length := len(b)
	// Find closing ']' using depth tracking for nested brackets.
	i := start + 1
	depth := 1
	for i < length && depth > 0 {
		switch b[i] {
		case '[':
			depth++
		case ']':
			depth--
		}
		i++
	}
	if depth != 0 || i >= length || b[i] != '(' {
		return 0, false
	}
	// i is at '('. Find matching ')'.
	i++
	parenStart := i
	parenDepth := 1
	for i < length && parenDepth > 0 {
		switch b[i] {
		case '(':
			parenDepth++
		case ')':
			parenDepth--
		}
		i++
	}
	if parenDepth != 0 {
		return 0, false
	}
	// Check that the URL inside starts with http:// or https://.
	urlBytes := trimASCII(b[parenStart : i-1])
	if startsWithHTTP(urlBytes) {
		return i, true
	}
	return 0, false
}

func trimASCII(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t') {
		end--
	}
	return b[start:end]
}

func startsWithHTTP(b []byte) bool {
	if len(b) >= 8 && strings.EqualFold(string(b[:8]), "https://") {
		return true
	}
	if len(b) >= 7 && strings.EqualFold(string(b[:7]), "http://") {
		return true
	}
	return false
}

// findBareURLs finds all bare http://, https://, or ftp:// URLs in text.
func findBareURLs(text string) []string {
	b := []byte(text)
	length := len(b)
	var results []string
	i := 0

	for i < length {
		remaining := b[i:]
		isHTTP := (b[i] == 'h' || b[i] == 'H') && startsWithHTTP(remaining)
		isExtra := !isHTTP && startsWithExtraScheme(remaining)
		if isHTTP || isExtra {
			start := i
			for i < length && !isASCIIWhitespace(b[i]) {
				i++
			}
			candidate := text[start:i]
			cleaned := stripURLTail(candidate)
			if len(cleaned) > 7 {
				results = append(results, cleaned)
			}
		} else {
			i++
		}
	}
	return results
}

func startsWithExtraScheme(b []byte) bool {
	for _, scheme := range extraSchemes {
		sb := []byte(scheme)
		if len(b) >= len(sb) && strings.EqualFold(string(b[:len(sb)]), scheme) {
			return true
		}
	}
	return false
}

func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

// stripURLTail strips trailing punctuation that is not part of the URL.
// Handles balanced brackets: (), [], {}, <>.
// Always strips trailing: , . ; : ! ? ' "
func stripURLTail(url string) string {
	b := []byte(url)
	end := len(b)

	for end > 0 {
		switch b[end-1] {
		case ',', '.', ';', '!', '?', '\'', '"', ':':
			end--
		case ')':
			if countByte(b[:end], '(') < countByte(b[:end], ')') {
				end--
			} else {
				return url[:end]
			}
		case ']':
			if countByte(b[:end], '[') < countByte(b[:end], ']') {
				end--
			} else {
				return url[:end]
			}
		case '}':
			if countByte(b[:end], '{') < countByte(b[:end], '}') {
				end--
			} else {
				return url[:end]
			}
		case '>':
			if countByte(b[:end], '<') < countByte(b[:end], '>') {
				end--
			} else {
				return url[:end]
			}
		default:
			return url[:end]
		}
	}
	return url[:end]
}

func countByte(b []byte, needle byte) int {
	count := 0
	for _, v := range b {
		if v == needle {
			count++
		}
	}
	return count
}

// isAllowedURL checks if a raw URL string is allowed (recognized scheme, passes SSRF).
func isAllowedURL(raw string) bool {
	// Extra schemes (ftp://) are allowed without SSRF check.
	if startsWithExtraScheme([]byte(raw)) {
		return true
	}

	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	return coresecurity.IsSafeURL(raw)
}
