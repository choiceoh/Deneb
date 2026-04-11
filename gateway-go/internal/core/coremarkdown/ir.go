package coremarkdown

import "strings"

// MarkdownToIR parses markdown into IR.
func MarkdownToIR(markdown string, opts *ParseOptions) MarkdownIR {
	if opts == nil {
		d := DefaultParseOptions()
		opts = &d
	}

	input := markdown
	if opts.EnableSpoilers {
		input = preprocessSpoilers(markdown)
	}

	rs := newRenderState(opts)
	parseMarkdown(input, rs, opts)
	rs.closeRemainingStyles()

	// Post-process spoiler sentinels that survived parsing.
	if opts.EnableSpoilers {
		postProcessSpoilers(rs)
	}

	// Final trimming (matching Rust behavior).
	fullText := rs.text.String()
	trimmedLen := len(strings.TrimRight(fullText, " \t\n\r"))
	codeBlockEnd := 0
	for _, sp := range rs.styles {
		if sp.Style == StyleCodeBlock && sp.End > codeBlockEnd {
			codeBlockEnd = sp.End
		}
	}
	finalLen := trimmedLen
	if codeBlockEnd > finalLen {
		finalLen = codeBlockEnd
	}
	if finalLen < len(fullText) {
		fullText = fullText[:finalLen]
	}

	styles := mergeStyleSpans(clampStyleSpans(rs.styles, finalLen))
	links := clampLinkSpans(rs.links, finalLen)

	return MarkdownIR{Text: fullText, Styles: styles, Links: links}
}
