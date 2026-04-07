package coremarkdown

import "strings"

// DetectFences parses fenced code block spans from raw markdown text.
// A fence opens with 3+ backticks or tildes (with up to 3 spaces indent)
// and closes with a matching or longer run of the same character.
// Unclosed fences extend to end of buffer.
//
// Mirrors core-rs/core/src/markdown/fences.rs:parse_fence_spans.
// This is a promoted version of the noffi fallback logic.
func DetectFences(text string) []FenceSpan {
	if text == "" {
		return nil
	}

	var spans []FenceSpan
	lines := strings.Split(text, "\n")
	pos := 0

	type openFence struct {
		start      int
		markerChar byte
		markerLen  int
		openLine   string
		indent     string
	}
	var current *openFence

	for _, line := range lines {
		lineEnd := pos + len(line)

		if current == nil {
			indent, marker, _ := matchFenceLine(line)
			if marker != "" {
				current = &openFence{
					start:      pos,
					markerChar: marker[0],
					markerLen:  len(marker),
					openLine:   line,
					indent:     indent,
				}
			}
		} else {
			_, marker, rest := matchFenceLine(line)
			if marker != "" && marker[0] == current.markerChar && len(marker) >= current.markerLen {
				// Closing fence: same char, >= marker length, nothing else after.
				if strings.TrimSpace(rest) == "" {
					spans = append(spans, FenceSpan{
						Start:    current.start,
						End:      lineEnd,
						OpenLine: current.openLine,
						Marker:   repeatByte(current.markerChar, current.markerLen),
						Indent:   current.indent,
					})
					current = nil
				}
			}
		}
		pos = lineEnd + 1 // +1 for the newline
	}

	// Unclosed fence extends to end of buffer.
	if current != nil {
		spans = append(spans, FenceSpan{
			Start:    current.start,
			End:      len(text),
			OpenLine: current.openLine,
			Marker:   repeatByte(current.markerChar, current.markerLen),
			Indent:   current.indent,
		})
	}

	return spans
}

// matchFenceLine matches: ^( {0,3})(`{3,}|~{3,})(.*)$
// Returns (indent, marker, rest) or ("", "", "") if no match.
func matchFenceLine(line string) (indent, marker, rest string) {
	n := len(line)
	// Count leading spaces (0-3).
	indentLen := 0
	for indentLen < n && indentLen < 4 && line[indentLen] == ' ' {
		indentLen++
	}
	if indentLen >= 4 || indentLen >= n {
		return "", "", ""
	}

	ch := line[indentLen]
	if ch != '`' && ch != '~' {
		return "", "", ""
	}

	markerEnd := indentLen
	for markerEnd < n && line[markerEnd] == ch {
		markerEnd++
	}
	markerLen := markerEnd - indentLen
	if markerLen < 3 {
		return "", "", ""
	}

	return line[:indentLen], line[indentLen:markerEnd], line[markerEnd:]
}

func repeatByte(ch byte, count int) string {
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = ch
	}
	return string(buf)
}
