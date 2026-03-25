package telegram

import (
	"strings"
	"unicode/utf16"
)

// Telegram message limits.
const (
	MaxTextLength    = 4096
	MaxCaptionLength = 1024
)

// FormatHTML converts basic markdown to Telegram-compatible HTML.
// Supports: **bold**, *italic*, `code`, ```code blocks```, ~~strikethrough~~,
// [text](url) links. Falls back to plain text on parse failure.
func FormatHTML(text string) string {
	var b strings.Builder
	b.Grow(len(text) + len(text)/10)

	i := 0
	runes := []rune(text)
	n := len(runes)

	for i < n {
		ch := runes[i]

		// Code block: ```...```
		if i+2 < n && ch == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			end := findTripleBacktick(runes, i+3)
			if end >= 0 {
				content := string(runes[i+3 : end])
				// Skip optional language tag on first line.
				if nl := strings.IndexByte(content, '\n'); nl >= 0 {
					lang := strings.TrimSpace(content[:nl])
					code := content[nl+1:]
					if isLangTag(lang) {
						b.WriteString("<pre><code class=\"language-")
						b.WriteString(escapeHTML(lang))
						b.WriteString("\">")
						b.WriteString(escapeHTML(code))
					} else {
						b.WriteString("<pre><code>")
						b.WriteString(escapeHTML(content))
					}
				} else {
					b.WriteString("<pre><code>")
					b.WriteString(escapeHTML(content))
				}
				b.WriteString("</code></pre>")
				i = end + 3
				continue
			}
		}

		// Inline code: `...`
		if ch == '`' {
			end := indexRune(runes, '`', i+1)
			if end >= 0 {
				b.WriteString("<code>")
				b.WriteString(escapeHTML(string(runes[i+1 : end])))
				b.WriteString("</code>")
				i = end + 1
				continue
			}
		}

		// Bold: **...**
		if i+1 < n && ch == '*' && runes[i+1] == '*' {
			end := findDouble(runes, '*', i+2)
			if end >= 0 {
				b.WriteString("<b>")
				b.WriteString(FormatHTML(string(runes[i+2 : end])))
				b.WriteString("</b>")
				i = end + 2
				continue
			}
		}

		// Strikethrough: ~~...~~
		if i+1 < n && ch == '~' && runes[i+1] == '~' {
			end := findDouble(runes, '~', i+2)
			if end >= 0 {
				b.WriteString("<s>")
				b.WriteString(FormatHTML(string(runes[i+2 : end])))
				b.WriteString("</s>")
				i = end + 2
				continue
			}
		}

		// Link: [text](url)
		if ch == '[' {
			textEnd := indexRune(runes, ']', i+1)
			if textEnd >= 0 && textEnd+1 < n && runes[textEnd+1] == '(' {
				urlEnd := indexRune(runes, ')', textEnd+2)
				if urlEnd >= 0 {
					linkText := string(runes[i+1 : textEnd])
					linkURL := string(runes[textEnd+2 : urlEnd])
					b.WriteString("<a href=\"")
					b.WriteString(escapeHTML(linkURL))
					b.WriteString("\">")
					b.WriteString(escapeHTML(linkText))
					b.WriteString("</a>")
					i = urlEnd + 1
					continue
				}
			}
		}

		// Italic: *...* (single asterisk, not preceded by *)
		if ch == '*' && (i == 0 || runes[i-1] != '*') && (i+1 >= n || runes[i+1] != '*') {
			end := indexRune(runes, '*', i+1)
			if end >= 0 && (end+1 >= n || runes[end+1] != '*') {
				b.WriteString("<i>")
				b.WriteString(FormatHTML(string(runes[i+1 : end])))
				b.WriteString("</i>")
				i = end + 1
				continue
			}
		}

		// Default: escape and write.
		b.WriteString(escapeHTMLRune(ch))
		i++
	}

	return b.String()
}

// ChunkText splits text into chunks that fit within Telegram's limits.
// It tries to split at newlines, then at spaces, then by character count.
func ChunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find a good split point.
		splitAt := maxLen
		// Try to split at a newline.
		if idx := strings.LastIndex(remaining[:maxLen], "\n"); idx > maxLen/4 {
			splitAt = idx + 1
		} else if idx := strings.LastIndex(remaining[:maxLen], " "); idx > maxLen/4 {
			// Try to split at a space.
			splitAt = idx + 1
		}

		chunks = append(chunks, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return chunks
}

// ChunkHTML splits HTML text into chunks, respecting tag boundaries.
// This is a simplified version that avoids splitting inside HTML tags.
func ChunkHTML(html string, maxLen int) []string {
	if len(html) <= maxLen {
		return []string{html}
	}

	var chunks []string
	remaining := html

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		splitAt := findHTMLSplitPoint(remaining, maxLen)
		chunks = append(chunks, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return chunks
}

// SplitCaptionAndBody splits text for media messages: caption (up to 1024 chars)
// and remaining body text. Returns (caption, bodyChunks).
func SplitCaptionAndBody(text string, captionMax, bodyMax int) (string, []string) {
	if len(text) <= captionMax {
		return text, nil
	}

	// Split at a good point within caption limit.
	splitAt := captionMax
	if idx := strings.LastIndex(text[:captionMax], "\n"); idx > captionMax/4 {
		splitAt = idx
	} else if idx := strings.LastIndex(text[:captionMax], " "); idx > captionMax/4 {
		splitAt = idx
	}

	caption := text[:splitAt]
	body := strings.TrimSpace(text[splitAt:])

	if body == "" {
		return caption, nil
	}

	return caption, ChunkText(body, bodyMax)
}

// --- Helpers ---

func escapeHTML(s string) string {
	var b strings.Builder
	for _, r := range s {
		b.WriteString(escapeHTMLRune(r))
	}
	return b.String()
}

func escapeHTMLRune(r rune) string {
	switch r {
	case '<':
		return "&lt;"
	case '>':
		return "&gt;"
	case '&':
		return "&amp;"
	case '"':
		return "&quot;"
	default:
		return string(r)
	}
}

func indexRune(runes []rune, target rune, start int) int {
	for i := start; i < len(runes); i++ {
		if runes[i] == target {
			return i
		}
	}
	return -1
}

func findDouble(runes []rune, ch rune, start int) int {
	for i := start; i+1 < len(runes); i++ {
		if runes[i] == ch && runes[i+1] == ch {
			return i
		}
	}
	return -1
}

func findTripleBacktick(runes []rune, start int) int {
	for i := start; i+2 < len(runes); i++ {
		if runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			return i
		}
	}
	return -1
}

func isLangTag(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '+' || r == '#') {
			return false
		}
	}
	return true
}

func findHTMLSplitPoint(html string, maxLen int) int {
	// Walk backward from maxLen looking for a safe split point.
	// Avoid splitting inside < > tags.
	inTag := false
	lastSafe := maxLen

	for i := maxLen - 1; i >= maxLen/4; i-- {
		if i >= len(html) {
			continue
		}
		ch := html[i]
		if ch == '>' {
			inTag = true
		} else if ch == '<' {
			inTag = false
		}
		if !inTag && (ch == '\n' || ch == ' ') {
			lastSafe = i + 1
			break
		}
	}
	return lastSafe
}

// UTF16Len returns the UTF-16 code unit length of a string.
// Telegram uses UTF-16 offsets for entities.
func UTF16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
