package denebui

import "strings"

// RepairFenceGlitches repairs model fence-emission glitches around deneb-ui
// blocks before validation or delivery. Models occasionally fumble the
// "```deneb-ui" opener across two lines — a bare "```" line followed by a line
// holding only the info string. Two broken shapes reach users when that
// happens:
//
//   - outside any card fence the card lands inside an info-less code block
//     ("```" / "deneb-ui" / "<column>…") and renders as raw markup;
//   - immediately after an open card fence the bare "```" closes the card
//     mid-stream and the model restarts it from the top — the feed then shows
//     a truncated card followed by duplicate plain-text markup.
//
// The repair rewrites a split opener into a canonical "```deneb-ui" line;
// when the split opener also closed an open deneb-ui fence (a restart), the
// aborted earlier attempt is dropped and the restarted card takes its place.
// Content inside non-deneb-ui code fences is never touched. Returns the
// repaired text and whether anything changed.
func RepairFenceGlitches(text string) (string, bool) {
	if !strings.Contains(text, "```") {
		return text, false
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	repaired := false
	cardStart := -1 // index in out of the open deneb-ui fence's opener line
	inOther := false
	isHTML, htmlDecided := false, false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		t := strings.TrimSpace(line)
		switch {
		case inOther:
			if isFenceClose(t) {
				inOther = false
			}
			out = append(out, line)
		case cardStart >= 0:
			if isFenceClose(t) {
				if i+1 < len(lines) && isBareFenceInfoLine(lines[i+1]) {
					// Close + orphaned info line = the model aborted this card
					// and restarted it. Drop the aborted attempt (keeping any
					// prose glued before its opener) and collect the restarted
					// card under a fresh canonical opener.
					prefix, _, _ := denebUIFenceOpenParts(strings.TrimSpace(out[cardStart]))
					out = out[:cardStart]
					if prefix != "" {
						out = append(out, prefix)
					}
					out = append(out, "```"+FenceInfo)
					cardStart = len(out) - 1
					isHTML, htmlDecided = false, false
					i++ // skip the orphaned info line
					repaired = true
					continue
				}
				cardStart = -1
				out = append(out, line)
				continue
			}
			if !htmlDecided && t != "" {
				isHTML = strings.HasPrefix(t, "<")
				htmlDecided = true
			}
			// Inside an HTML body a glued ``` run can only be the close —
			// HTML bodies escape backticks as &#96; per the authoring contract.
			if isHTML {
				if _, closed := splitGluedFenceClose(line); closed {
					cardStart = -1
				}
			}
			out = append(out, line)
		default:
			if isDenebUIFenceOpen(t) {
				rest, _ := denebUIFenceOpenSplit(t)
				cardStart = len(out)
				isHTML, htmlDecided = rest != "", rest != ""
				if rest != "" {
					if _, closed := splitGluedFenceClose(rest); closed {
						cardStart = -1 // opened and closed on the same line
					}
				}
				out = append(out, line)
				continue
			}
			if isFenceClose(t) && i+1 < len(lines) && isBareFenceInfoLine(lines[i+1]) {
				// Split opener: bare "```" with the info string on the next line.
				cardStart = len(out)
				out = append(out, "```"+FenceInfo)
				isHTML, htmlDecided = false, false
				i++ // skip the orphaned info line
				repaired = true
				continue
			}
			if strings.HasPrefix(t, "```") {
				inOther = true
			}
			out = append(out, line)
		}
	}
	if !repaired {
		return text, false
	}
	return strings.Join(out, "\n"), true
}

// isBareFenceInfoLine reports whether a line holds nothing but the deneb-ui
// info string — the orphaned half of a fence opener the model split in two.
func isBareFenceInfoLine(line string) bool {
	return strings.EqualFold(strings.TrimSpace(line), FenceInfo)
}
