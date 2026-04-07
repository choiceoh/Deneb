package chunk

import (
	"strings"
	"unicode"
)

// Mode controls how outbound messages are split.
type Mode string

const (
	ModeLength  Mode = "length"  // split only when exceeding limit
	ModeNewline Mode = "newline" // prefer breaking on paragraph boundaries
)

// DefaultLimit is the default maximum characters per outbound message chunk.
const DefaultLimit = 4000

// ResolveTextLimit returns the chunk limit for a given provider/account,
// falling back to the default. providerLimit is looked up from config externally.
func ResolveTextLimit(providerLimit, fallback int) int {
	if fallback <= 0 {
		fallback = DefaultLimit
	}
	if providerLimit > 0 {
		return providerLimit
	}
	return fallback
}

// Text splits text into chunks of at most `limit` characters, preferring
// newline and whitespace breaks. This mirrors the TS chunkText().
func Text(text string, limit int) []string {
	if text == "" {
		return nil
	}
	if limit <= 0 {
		return []string{text}
	}
	if len(text) <= limit {
		return []string{text}
	}
	return chunkTextByBreaks(text, limit)
}

func chunkTextByBreaks(text string, limit int) []string {
	var chunks []string
	start := 0

	for start < len(text) {
		if len(text)-start <= limit {
			chunks = append(chunks, text[start:])
			break
		}

		end := start + limit
		if end > len(text) {
			end = len(text)
		}
		window := text[start:end]

		bp := scanParenAwareBreakpoints(window, 0, len(window), nil)
		var breakIdx int
		switch {
		case bp.lastNewline > 0:
			breakIdx = bp.lastNewline
		case bp.lastWhitespace > 0:
			breakIdx = bp.lastWhitespace
		default:
			breakIdx = len(window)
		}

		chunks = append(chunks, text[start:start+breakIdx])

		// Skip the separator character if we broke on whitespace.
		next := start + breakIdx
		if next < len(text) && isWhitespace(rune(text[next])) {
			next++
		}
		start = next
	}
	return chunks
}

type breakpoints struct {
	lastNewline    int
	lastWhitespace int
}

func scanParenAwareBreakpoints(text string, start, end int, isAllowed func(int) bool) breakpoints {
	bp := breakpoints{lastNewline: -1, lastWhitespace: -1}
	depth := 0

	for i := start; i < end; i++ {
		if isAllowed != nil && !isAllowed(i) {
			continue
		}
		ch := text[i]
		if ch == '(' {
			depth++
			continue
		}
		if ch == ')' && depth > 0 {
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if ch == '\n' {
			bp.lastNewline = i
		} else if isWhitespace(rune(ch)) {
			bp.lastWhitespace = i
		}
	}
	return bp
}

func isWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}

// ByParagraph splits text on paragraph boundaries (blank lines),
// packing multiple paragraphs into a single chunk up to limit.
// Falls back to length-based splitting for oversized paragraphs.
func ByParagraph(text string, limit int, splitLongParagraphs bool) []string {
	if text == "" {
		return nil
	}
	if limit <= 0 {
		return []string{text}
	}

	// Normalize line endings.
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")

	// Fast-path: no paragraph separators.
	paragraphRe := strings.Contains(normalized, "\n\n") || strings.Contains(normalized, "\n \n") || strings.Contains(normalized, "\n\t\n")
	if !paragraphRe {
		if len(normalized) <= limit {
			return []string{normalized}
		}
		if !splitLongParagraphs {
			return []string{normalized}
		}
		return Text(normalized, limit)
	}

	// Split on blank lines (one or more newlines with optional whitespace between).
	parts := splitOnBlankLines(normalized)

	var chunks []string
	for _, part := range parts {
		paragraph := strings.TrimRight(part, " \t\n\r")
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		switch {
		case len(paragraph) <= limit:
			chunks = append(chunks, paragraph)
		case !splitLongParagraphs:
			chunks = append(chunks, paragraph)
		default:
			chunks = append(chunks, Text(paragraph, limit)...)
		}
	}
	return chunks
}

// splitOnBlankLines splits text at paragraph boundaries (blank lines).
func splitOnBlankLines(text string) []string {
	var parts []string
	lines := strings.Split(text, "\n")
	var current strings.Builder

	inBlank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !inBlank && current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			inBlank = true
			continue
		}
		if inBlank {
			inBlank = false
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

// ByNewline splits text on newlines, trimming line whitespace.
// Blank lines are folded into the next non-empty line as leading "\n" prefixes.
// Long lines are split by length unless splitLongLines is false.
func ByNewline(text string, maxLineLength int, splitLongLines, trimLines bool) []string {
	if text == "" {
		return nil
	}
	if maxLineLength <= 0 {
		t := strings.TrimSpace(text)
		if t == "" {
			return nil
		}
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	var chunks []string
	pendingBlankLines := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			pendingBlankLines++
			continue
		}

		maxPrefix := maxLineLength - 1
		if maxPrefix < 0 {
			maxPrefix = 0
		}
		cappedBlanks := pendingBlankLines
		if cappedBlanks > maxPrefix {
			cappedBlanks = maxPrefix
		}
		prefix := ""
		if cappedBlanks > 0 {
			prefix = strings.Repeat("\n", cappedBlanks)
		}
		pendingBlankLines = 0

		lineValue := line
		if trimLines {
			lineValue = trimmed
		}
		if !splitLongLines || len(lineValue)+len(prefix) <= maxLineLength {
			chunks = append(chunks, prefix+lineValue)
			continue
		}

		firstLimit := maxLineLength - len(prefix)
		if firstLimit < 1 {
			firstLimit = 1
		}
		first := lineValue[:firstLimit]
		chunks = append(chunks, prefix+first)
		remaining := lineValue[firstLimit:]
		if remaining != "" {
			chunks = append(chunks, Text(remaining, maxLineLength)...)
		}
	}

	if pendingBlankLines > 0 && len(chunks) > 0 {
		chunks[len(chunks)-1] += strings.Repeat("\n", pendingBlankLines)
	}
	return chunks
}

// TextWithMode dispatches to the appropriate chunking function based on mode.
func TextWithMode(text string, limit int, mode Mode) []string {
	if mode == ModeNewline {
		return ByParagraph(text, limit, true)
	}
	return Text(text, limit)
}
