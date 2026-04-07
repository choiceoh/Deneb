package coremarkdown

// ---------------------------------------------------------------------------
// Span utilities (mirrors core-rs/core/src/markdown/spans.rs)
// ---------------------------------------------------------------------------

func styleSortKey(s MarkdownStyle) int {
	switch s {
	case StyleBlockquote:
		return 0
	case StyleBold:
		return 1
	case StyleCode:
		return 2
	case StyleCodeBlock:
		return 3
	case StyleItalic:
		return 4
	case StyleSpoiler:
		return 5
	case StyleStrikethrough:
		return 6
	}
	return 7
}

func mergeStyleSpans(spans []StyleSpan) []StyleSpan {
	if len(spans) == 0 {
		return spans
	}
	sorted := make([]StyleSpan, len(spans))
	copy(sorted, spans)
	// Sort by (start, end, style sort key).
	for i := 1; i < len(sorted); i++ {
		key := sorted[i]
		j := i - 1
		for j >= 0 && spanLess(key, sorted[j]) {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = key
	}
	merged := make([]StyleSpan, 0, len(sorted))
	for _, sp := range sorted {
		if len(merged) > 0 {
			prev := &merged[len(merged)-1]
			if prev.Style == sp.Style &&
				(sp.Start < prev.End || (sp.Start == prev.End && sp.Style != StyleBlockquote)) {
				if sp.End > prev.End {
					prev.End = sp.End
				}
				continue
			}
		}
		merged = append(merged, sp)
	}
	return merged
}

func spanLess(a, b StyleSpan) bool {
	if a.Start != b.Start {
		return a.Start < b.Start
	}
	if a.End != b.End {
		return a.End < b.End
	}
	return styleSortKey(a.Style) < styleSortKey(b.Style)
}

func clampStyleSpans(spans []StyleSpan, maxLen int) []StyleSpan {
	out := make([]StyleSpan, 0, len(spans))
	for _, sp := range spans {
		s := sp.Start
		if s > maxLen {
			s = maxLen
		}
		e := sp.End
		if e < s {
			e = s
		}
		if e > maxLen {
			e = maxLen
		}
		if e > s {
			out = append(out, StyleSpan{Start: s, End: e, Style: sp.Style})
		}
	}
	return out
}

func clampLinkSpans(spans []LinkSpan, maxLen int) []LinkSpan {
	out := make([]LinkSpan, 0, len(spans))
	for _, sp := range spans {
		s := sp.Start
		if s > maxLen {
			s = maxLen
		}
		e := sp.End
		if e < s {
			e = s
		}
		if e > maxLen {
			e = maxLen
		}
		if e > s {
			out = append(out, LinkSpan{Start: s, End: e, Href: sp.Href})
		}
	}
	return out
}
