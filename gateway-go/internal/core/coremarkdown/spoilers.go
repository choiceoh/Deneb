package coremarkdown

// Spoiler preprocessing: converts ||hidden text|| into zero-width sentinels
// so goldmark doesn't misinterpret the pipes as table syntax.
// Mirrors core-rs/core/src/markdown/spoilers.rs.

const (
	// sentinelOpen / sentinelClose are zero-width character sequences that
	// replace || delimiters before goldmark parsing.
	sentinelOpen  = "\u200B\uFEFFSPOILER_OPEN\u200B"
	sentinelClose = "\u200B\uFEFFSPOILER_CLOSE\u200B"
)

// preprocessSpoilers replaces matched ||...|| pairs with sentinel markers.
// Only an even number of || delimiters are converted; an odd remainder is
// left as literal ||.
func preprocessSpoilers(text string) string {
	bytes := []byte(text)
	n := len(bytes)

	// Count total || delimiters.
	total := 0
	for i := 0; i < n-1; i++ {
		if bytes[i] == '|' && bytes[i+1] == '|' {
			total++
			i++ // skip second pipe
		}
	}
	if total < 2 {
		return text
	}

	usable := total - (total % 2)

	// Second pass: build result with sentinel substitution.
	var result []byte
	consumed := 0
	spoilerOpen := false
	i := 0
	for i < n {
		if i+1 < n && bytes[i] == '|' && bytes[i+1] == '|' {
			if consumed >= usable {
				result = append(result, '|', '|')
				i += 2
				continue
			}
			consumed++
			spoilerOpen = !spoilerOpen
			if spoilerOpen {
				result = append(result, sentinelOpen...)
			} else {
				result = append(result, sentinelClose...)
			}
			i += 2
		} else {
			result = append(result, bytes[i])
			i++
		}
	}
	return string(result)
}

// handleSpoilerText processes text containing sentinel markers, appending
// plain text segments to the render state and toggling Spoiler style spans.
func handleSpoilerText(rs *renderState, text string) {
	remaining := text
	for remaining != "" {
		openIdx := indexOf(remaining, sentinelOpen)
		closeIdx := indexOf(remaining, sentinelClose)

		if openIdx < 0 && closeIdx < 0 {
			rs.appendText(remaining)
			break
		}

		// Pick whichever sentinel comes first.
		if openIdx >= 0 && (closeIdx < 0 || openIdx <= closeIdx) {
			if openIdx > 0 {
				rs.appendText(remaining[:openIdx])
			}
			rs.openStyle(StyleSpoiler)
			remaining = remaining[openIdx+len(sentinelOpen):]
		} else {
			if closeIdx > 0 {
				rs.appendText(remaining[:closeIdx])
			}
			rs.closeStyle(StyleSpoiler)
			remaining = remaining[closeIdx+len(sentinelClose):]
		}
	}
}

// indexOf returns the byte index of sub in s, or -1 if not found.
func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// postProcessSpoilers strips sentinel markers from the render state's text
// and adds Spoiler style spans. This handles cases where goldmark splits text
// nodes across sentinel boundaries, preventing inline detection.
func postProcessSpoilers(rs *renderState) {
	txt := rs.text.String()
	if !containsSentinel(txt) {
		return
	}

	// Build new text without sentinels, tracking spoiler spans.
	var newText []byte
	var spoilerSpans []StyleSpan
	remaining := txt
	spoilerStart := -1

	for remaining != "" {
		openIdx := indexOf(remaining, sentinelOpen)
		closeIdx := indexOf(remaining, sentinelClose)

		if openIdx < 0 && closeIdx < 0 {
			newText = append(newText, remaining...)
			break
		}

		// Pick whichever sentinel comes first.
		if openIdx >= 0 && (closeIdx < 0 || openIdx <= closeIdx) {
			newText = append(newText, remaining[:openIdx]...)
			spoilerStart = len(newText)
			remaining = remaining[openIdx+len(sentinelOpen):]
		} else {
			newText = append(newText, remaining[:closeIdx]...)
			if spoilerStart >= 0 && len(newText) > spoilerStart {
				spoilerSpans = append(spoilerSpans, StyleSpan{
					Start: spoilerStart,
					End:   len(newText),
					Style: StyleSpoiler,
				})
			}
			spoilerStart = -1
			remaining = remaining[closeIdx+len(sentinelClose):]
		}
	}

	// Compute byte offset shift map for existing spans.
	oldText := txt
	newStr := string(newText)
	rs.text.Reset()
	rs.text.WriteString(newStr)

	// Remap existing style/link offsets: for each byte position in old text,
	// compute the corresponding position in new text (sentinel bytes removed).
	offsetMap := buildOffsetMap(oldText, newStr)
	for i := range rs.styles {
		rs.styles[i].Start = offsetMap(rs.styles[i].Start)
		rs.styles[i].End = offsetMap(rs.styles[i].End)
	}
	for i := range rs.links {
		rs.links[i].Start = offsetMap(rs.links[i].Start)
		rs.links[i].End = offsetMap(rs.links[i].End)
	}
	// Remove zero-width spans that collapsed.
	rs.styles = filterNonEmpty(rs.styles)
	rs.links = filterNonEmptyLinks(rs.links)

	// Add spoiler spans.
	rs.styles = append(rs.styles, spoilerSpans...)
}

func containsSentinel(s string) bool {
	return indexOf(s, sentinelOpen) >= 0 || indexOf(s, sentinelClose) >= 0
}

// buildOffsetMap returns a function that maps old byte positions to new byte
// positions after sentinel removal.
func buildOffsetMap(old, _ string) func(int) int {
	// Build a table: for each position in old text, what's the cumulative
	// number of sentinel bytes removed up to (but not including) that position.
	// Then new_pos = old_pos - removed_before[old_pos].
	type removal struct {
		pos int // position in old text where sentinel starts
		len int // length of sentinel
	}
	var removals []removal
	remaining := old
	offset := 0
	for remaining != "" {
		openIdx := indexOf(remaining, sentinelOpen)
		closeIdx := indexOf(remaining, sentinelClose)
		if openIdx < 0 && closeIdx < 0 {
			break
		}
		if openIdx >= 0 && (closeIdx < 0 || openIdx <= closeIdx) {
			removals = append(removals, removal{pos: offset + openIdx, len: len(sentinelOpen)})
			remaining = remaining[openIdx+len(sentinelOpen):]
			offset += openIdx + len(sentinelOpen)
		} else {
			removals = append(removals, removal{pos: offset + closeIdx, len: len(sentinelClose)})
			remaining = remaining[closeIdx+len(sentinelClose):]
			offset += closeIdx + len(sentinelClose)
		}
	}

	return func(oldPos int) int {
		removed := 0
		for _, r := range removals {
			if oldPos <= r.pos {
				break
			}
			if oldPos >= r.pos+r.len {
				removed += r.len
			} else {
				// Position is inside a sentinel; map to sentinel start.
				removed += oldPos - r.pos
			}
		}
		return oldPos - removed
	}
}

func filterNonEmpty(spans []StyleSpan) []StyleSpan {
	out := spans[:0]
	for _, sp := range spans {
		if sp.End > sp.Start {
			out = append(out, sp)
		}
	}
	return out
}

func filterNonEmptyLinks(spans []LinkSpan) []LinkSpan {
	out := spans[:0]
	for _, sp := range spans {
		if sp.End > sp.Start {
			out = append(out, sp)
		}
	}
	return out
}
