package telegram

import (
	"strings"
	"unicode/utf16"
)

// Telegram message limits.
const (
	// MaxTextLength is the Telegram API hard limit for message text (4096 chars).
	// For outbound chunking, use TextChunkLimit (4000) which leaves headroom for HTML overhead.
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

		// Spoiler: ||...||
		if i+1 < n && ch == '|' && runes[i+1] == '|' {
			end := findDouble(runes, '|', i+2)
			if end >= 0 {
				b.WriteString("<tg-spoiler>")
				b.WriteString(FormatHTML(string(runes[i+2 : end])))
				b.WriteString("</tg-spoiler>")
				i = end + 2
				continue
			}
		}

		// Default: escape and write.
		b.WriteString(escapeHTMLRune(ch))
		i++
	}

	return b.String()
}

// MarkdownToTelegramHTML converts multiline markdown to Telegram HTML.
// Handles line-level constructs (headings, blockquotes, fenced code blocks)
// in addition to inline formatting via FormatHTML.
func MarkdownToTelegramHTML(markdown string) string {
	if markdown == "" {
		return ""
	}
	lines := strings.Split(markdown, "\n")
	var out strings.Builder
	out.Grow(len(markdown) + len(markdown)/4)

	inCodeBlock := false
	var codeBlockBuf strings.Builder
	inTable := false
	var tableBuf strings.Builder

	for i, line := range lines {
		// Handle fenced code blocks.
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				out.WriteString("<pre><code>")
				out.WriteString(escapeHTML(codeBlockBuf.String()))
				out.WriteString("</code></pre>")
				codeBlockBuf.Reset()
				inCodeBlock = false
				if i < len(lines)-1 {
					out.WriteByte('\n')
				}
				continue
			}
			inCodeBlock = true
			// Skip the language tag line.
			continue
		}
		if inCodeBlock {
			if codeBlockBuf.Len() > 0 {
				codeBlockBuf.WriteByte('\n')
			}
			codeBlockBuf.WriteString(line)
			continue
		}

		// Handle markdown tables: buffer lines and render as <pre>.
		if isTableLine(line) {
			if tableBuf.Len() > 0 {
				tableBuf.WriteByte('\n')
			}
			tableBuf.WriteString(line)
			inTable = true
			continue
		}
		if inTable {
			out.WriteString("<pre>")
			out.WriteString(escapeHTML(tableBuf.String()))
			out.WriteString("</pre>")
			tableBuf.Reset()
			inTable = false
			out.WriteByte('\n')
			// Fall through to process current non-table line.
		}

		// Blockquote: "> text"
		if strings.HasPrefix(line, "> ") {
			out.WriteString("<blockquote>")
			out.WriteString(FormatHTML(line[2:]))
			out.WriteString("</blockquote>")
			if i < len(lines)-1 {
				out.WriteByte('\n')
			}
			continue
		}

		// Heading: render as bold (Telegram has no heading tags).
		if headingContent := parseHeading(line); headingContent != "" {
			out.WriteString("<b>")
			out.WriteString(FormatHTML(headingContent))
			out.WriteString("</b>")
			if i < len(lines)-1 {
				out.WriteByte('\n')
			}
			continue
		}

		out.WriteString(FormatHTML(line))
		if i < len(lines)-1 {
			out.WriteByte('\n')
		}
	}

	// Handle unclosed table.
	if inTable {
		out.WriteString("<pre>")
		out.WriteString(escapeHTML(tableBuf.String()))
		out.WriteString("</pre>")
	}

	// Handle unclosed code block.
	if inCodeBlock {
		out.WriteString("<pre><code>")
		out.WriteString(escapeHTML(codeBlockBuf.String()))
		out.WriteString("</code></pre>")
	}

	return out.String()
}

// parseHeading returns the heading content if line is a markdown heading.
func parseHeading(line string) string {
	if !strings.HasPrefix(line, "#") {
		return ""
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i > 6 || i >= len(line) || line[i] != ' ' {
		return ""
	}
	return strings.TrimSpace(line[i+1:])
}

// MarkdownToTelegramChunks converts markdown to chunked Telegram HTML.
func MarkdownToTelegramChunks(markdown string, limit int) []string {
	html := MarkdownToTelegramHTML(markdown)
	return ChunkHTML(html, limit)
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
		} else {
			// Fallback: ensure we don't split a multi-byte UTF-8 character.
			splitAt = len(truncateUTF8(remaining, maxLen))
		}

		chunks = append(chunks, remaining[:splitAt])
		remaining = remaining[splitAt:]
	}

	return chunks
}

// ChunkByNewline splits text on every newline boundary, merging consecutive
// lines into chunks that stay under maxLen. This matches the TypeScript
// chunkMode: "newline" behavior.
func ChunkByNewline(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		// If a single line exceeds maxLen, fall back to length-based chunking.
		if len(line) > maxLen {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			chunks = append(chunks, ChunkText(line, maxLen)...)
			continue
		}

		needed := len(line)
		if current.Len() > 0 {
			needed++ // newline separator
		}

		if current.Len()+needed > maxLen {
			chunks = append(chunks, current.String())
			current.Reset()
		}

		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}

	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}

	return chunks
}

// ChunkHTML splits HTML text into chunks, respecting tag boundaries.
// It tracks <pre><code> blocks so code fences are never left unclosed.
// When a code block must be split, it closes the block in the current
// chunk and reopens it (with the original language tag) in the next.
func ChunkHTML(html string, maxLen int) []string {
	if len(html) <= maxLen {
		return []string{html}
	}

	const closeTag = "</code></pre>"
	var chunks []string
	remaining := html

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		splitAt := findHTMLSplitPoint(remaining, maxLen)
		chunk := remaining[:splitAt]
		remaining = remaining[splitAt:]

		// Check if we split inside a <pre><code> block.
		if tag, tagPos := unclosedCodeBlock(chunk); tag != "" {
			if tagPos > maxLen/4 {
				// Enough content before the code block — split there instead.
				remaining = chunk[tagPos:] + remaining
				chunk = chunk[:tagPos]
			} else {
				// Code block is too large to avoid — close and reopen.
				chunk += closeTag
				remaining = tag + remaining
			}
		}

		chunks = append(chunks, chunk)
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
	} else {
		// Fallback: ensure we don't split a multi-byte UTF-8 character.
		splitAt = len(truncateUTF8(text, captionMax))
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

// isTableLine returns true if the line looks like a markdown table row
// (trimmed, starts and ends with |, length > 1).
func isTableLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) > 1 && trimmed[0] == '|' && trimmed[len(trimmed)-1] == '|'
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

// unclosedCodeBlock scans html for an unmatched <pre><code...> tag.
// Returns the opening tag (e.g. `<pre><code class="language-go">`) and its
// byte offset, or ("", -1) when all code blocks are properly closed.
func unclosedCodeBlock(html string) (tag string, pos int) {
	const opener = "<pre><code"
	const closer = "</code></pre>"
	openTag := ""
	openPos := -1
	i := 0
	for {
		idx := strings.Index(html[i:], opener)
		if idx < 0 {
			break
		}
		abs := i + idx
		// Find closing '>' of the opening tag.
		closeAngle := strings.IndexByte(html[abs+len(opener):], '>')
		if closeAngle < 0 {
			break
		}
		tagEnd := abs + len(opener) + closeAngle + 1
		openTag = html[abs:tagEnd]
		openPos = abs
		i = tagEnd

		// Look for the matching </code></pre>.
		closeIdx := strings.Index(html[i:], closer)
		if closeIdx >= 0 {
			i += closeIdx + len(closer)
			openTag = ""
			openPos = -1
		} else {
			break // unclosed
		}
	}
	return openTag, openPos
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
	// Ensure we don't split a multi-byte UTF-8 character.
	return len(truncateUTF8(html, lastSafe))
}

// truncateUTF8 truncates s to at most maxBytes bytes without splitting
// a multi-byte UTF-8 character. The result is always valid UTF-8.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk backward past any UTF-8 continuation bytes (10xxxxxx).
	for maxBytes > 0 && s[maxBytes]&0xC0 == 0x80 {
		maxBytes--
	}
	return s[:maxBytes]
}

// TruncateDraftHTML truncates HTML text for draft streaming edits by keeping
// the tail (most recent content). It finds a safe split point that avoids
// breaking HTML tags or multi-byte UTF-8 characters, and prepends "…\n".
func TruncateDraftHTML(html string, maxLen int) string {
	if len(html) <= maxLen {
		return html
	}
	const prefix = "…\n"
	target := maxLen - len(prefix)
	if target <= 0 {
		return prefix
	}
	// Start from the end, skip back to find start offset.
	start := len(html) - target
	// Skip past any UTF-8 continuation bytes.
	for start < len(html) && html[start]&0xC0 == 0x80 {
		start++
	}
	// Try to find a newline or space nearby to avoid mid-word/tag splits.
	searchEnd := start + 200
	if searchEnd > len(html) {
		searchEnd = len(html)
	}
	for i := start; i < searchEnd; i++ {
		if html[i] == '\n' {
			start = i + 1
			break
		}
	}
	return prefix + html[start:]
}

// UTF16Len returns the UTF-16 code unit length of a string.
// Telegram uses UTF-16 offsets for entities.
func UTF16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}
