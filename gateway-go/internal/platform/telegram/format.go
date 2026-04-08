package telegram

import (
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coremarkdown"
)

// Telegram message limits.
const (
	// MaxTextLength is the Telegram API hard limit for message text (4096 chars).
	// For outbound chunking, use TextChunkLimit (4000) which leaves headroom for HTML overhead.
	MaxTextLength    = 4096
	MaxCaptionLength = 1024
)

// telegramParseOpts configures coremarkdown for Telegram HTML output.
var telegramParseOpts = &coremarkdown.ParseOptions{
	Linkify:        true,
	EnableSpoilers: true,
	HeadingStyle:   "bold",
	Autolink:       true,
	TableMode:      "code",
}

// FormatHTML converts basic markdown to Telegram-compatible HTML.
// Supports: **bold**, *italic*, `code`, ```code blocks```, ~~strikethrough~~,
// ||spoiler||, [text](url) links.
func FormatHTML(text string) string {
	return MarkdownToTelegramHTML(text)
}

// MarkdownToTelegramHTML converts multiline markdown to Telegram HTML.
// Parses markdown via coremarkdown IR, then renders style/link spans as HTML.
func MarkdownToTelegramHTML(markdown string) string {
	if markdown == "" {
		return ""
	}
	ir := coremarkdown.MarkdownToIR(markdown, telegramParseOpts)
	return renderIRToTelegramHTML(ir)
}

// spanEvent represents an HTML tag open/close at a byte position in the IR text.
type spanEvent struct {
	pos       int
	isClose   bool
	tag       string
	spanStart int // original span Start (for close ordering)
	spanEnd   int // original span End (for open ordering)
}

// renderIRToTelegramHTML renders a coremarkdown IR to Telegram HTML.
func renderIRToTelegramHTML(ir coremarkdown.MarkdownIR) string {
	text := ir.Text
	if text == "" {
		return ""
	}

	var events []spanEvent

	for _, s := range ir.Styles {
		open, close := styleTags(s.Style)
		if open == "" {
			continue
		}
		events = append(events,
			spanEvent{pos: s.Start, isClose: false, tag: open, spanStart: s.Start, spanEnd: s.End},
			spanEvent{pos: s.End, isClose: true, tag: close, spanStart: s.Start, spanEnd: s.End},
		)
	}

	for _, l := range ir.Links {
		events = append(events,
			spanEvent{pos: l.Start, isClose: false, tag: `<a href="` + escapeHTML(l.Href) + `">`, spanStart: l.Start, spanEnd: l.End},
			spanEvent{pos: l.End, isClose: true, tag: "</a>", spanStart: l.Start, spanEnd: l.End},
		)
	}

	// Sort: by position, closes before opens, then LIFO for closes / outer-first for opens.
	sort.Slice(events, func(i, j int) bool {
		if events[i].pos != events[j].pos {
			return events[i].pos < events[j].pos
		}
		if events[i].isClose != events[j].isClose {
			return events[i].isClose // closes before opens
		}
		if events[i].isClose {
			return events[i].spanStart > events[j].spanStart // LIFO: inner closes first
		}
		return events[i].spanEnd > events[j].spanEnd // outer opens first
	})

	var b strings.Builder
	b.Grow(len(text) + len(text)/4)
	eventIdx := 0
	textBytes := []byte(text)

	for i := 0; i < len(textBytes); {
		for eventIdx < len(events) && events[eventIdx].pos == i {
			b.WriteString(events[eventIdx].tag)
			eventIdx++
		}
		r, size := utf8.DecodeRune(textBytes[i:])
		b.WriteString(escapeHTMLRune(r))
		i += size
	}
	for eventIdx < len(events) {
		b.WriteString(events[eventIdx].tag)
		eventIdx++
	}

	result := b.String()
	// Strip trailing newline inside code blocks (parser artifact).
	result = strings.ReplaceAll(result, "\n</code></pre>", "</code></pre>")
	return result
}

// styleTags maps a coremarkdown style to Telegram HTML open/close tags.
func styleTags(style coremarkdown.MarkdownStyle) (string, string) {
	switch style {
	case coremarkdown.StyleBold:
		return "<b>", "</b>"
	case coremarkdown.StyleItalic:
		return "<i>", "</i>"
	case coremarkdown.StyleStrikethrough:
		return "<s>", "</s>"
	case coremarkdown.StyleCode:
		return "<code>", "</code>"
	case coremarkdown.StyleCodeBlock:
		return "<pre><code>", "</code></pre>"
	case coremarkdown.StyleSpoiler:
		return "<tg-spoiler>", "</tg-spoiler>"
	case coremarkdown.StyleBlockquote:
		return "<blockquote>", "</blockquote>"
	default:
		return "", ""
	}
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

	for remaining != "" {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find a good split point.
		var splitAt int
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

	for remaining != "" {
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
func SplitCaptionAndBody(text string, captionMax, bodyMax int) (caption string, bodyChunks []string) {
	if len(text) <= captionMax {
		return text, nil
	}

	// Split at a good point within caption limit.
	var splitAt int
	if idx := strings.LastIndex(text[:captionMax], "\n"); idx > captionMax/4 {
		splitAt = idx
	} else if idx := strings.LastIndex(text[:captionMax], " "); idx > captionMax/4 {
		splitAt = idx
	} else {
		// Fallback: ensure we don't split a multi-byte UTF-8 character.
		splitAt = len(truncateUTF8(text, captionMax))
	}

	caption = text[:splitAt]
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
		switch ch {
		case '>':
			inTag = true
		case '<':
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
