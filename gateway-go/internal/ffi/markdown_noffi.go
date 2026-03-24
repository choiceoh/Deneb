//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"regexp"
	"strings"
)

var mdBoldRe = regexp.MustCompile(`\*\*(.+?)\*\*|__(.+?)__`)
var mdItalicRe = regexp.MustCompile(`\*(.+?)\*|_(.+?)_`)
var mdCodeRe = regexp.MustCompile("`([^`]+)`")
var mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]+\)`)
var mdHeadingRe = regexp.MustCompile(`(?m)^#{1,6}\s+`)
var mdFenceOpenRe = regexp.MustCompile("(?m)^\\s{0,3}(`{3,}|~{3,})")

// MarkdownToIR is a pure-Go fallback that strips markdown to plain text.
// Returns a minimal IR structure. Full parsing requires the Rust implementation.
func MarkdownToIR(markdown string, _ string) (json.RawMessage, error) {
	if len(markdown) == 0 {
		return json.RawMessage(`{"text":"","styles":[],"links":[],"has_code_blocks":false}`), nil
	}

	// Strip common markdown syntax.
	text := markdown
	text = mdBoldRe.ReplaceAllString(text, "$1$2")
	text = mdItalicRe.ReplaceAllString(text, "$1$2")
	text = mdCodeRe.ReplaceAllString(text, "$1")
	text = mdLinkRe.ReplaceAllString(text, "$1")
	text = mdHeadingRe.ReplaceAllString(text, "")

	hasCodeBlocks := mdFenceOpenRe.MatchString(markdown)

	result := struct {
		Text          string `json:"text"`
		Styles        []any  `json:"styles"`
		Links         []any  `json:"links"`
		HasCodeBlocks bool   `json:"has_code_blocks"`
	}{
		Text:          strings.TrimSpace(text),
		Styles:        []any{},
		Links:         []any{},
		HasCodeBlocks: hasCodeBlocks,
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// MarkdownDetectFences is a pure-Go fallback for fence detection.
func MarkdownDetectFences(text string) (json.RawMessage, error) {
	if len(text) == 0 {
		return json.RawMessage("[]"), nil
	}

	type fenceSpan struct {
		Start    int    `json:"start"`
		End      int    `json:"end"`
		OpenLine string `json:"openLine"`
		Marker   string `json:"marker"`
		Indent   string `json:"indent"`
	}

	var spans []fenceSpan
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
			// Look for opening fence.
			trimmed := strings.TrimLeft(line, " ")
			indent := line[:len(line)-len(trimmed)]
			if len(indent) <= 3 && len(trimmed) >= 3 {
				ch := trimmed[0]
				if ch == '`' || ch == '~' {
					count := 0
					for count < len(trimmed) && trimmed[count] == ch {
						count++
					}
					if count >= 3 {
						current = &openFence{
							start:      pos,
							markerChar: ch,
							markerLen:  count,
							openLine:   line,
							indent:     indent,
						}
					}
				}
			}
		} else {
			// Look for closing fence.
			trimmed := strings.TrimLeft(line, " ")
			closingIndent := line[:len(line)-len(trimmed)]
			if len(closingIndent) <= 3 && len(trimmed) >= current.markerLen {
				ch := trimmed[0]
				if ch == current.markerChar {
					count := 0
					for count < len(trimmed) && trimmed[count] == ch {
						count++
					}
					// Closing fence: same char, >= marker length, nothing else.
					rest := strings.TrimSpace(trimmed[count:])
					if count >= current.markerLen && rest == "" {
						spans = append(spans, fenceSpan{
							Start:    current.start,
							End:      lineEnd,
							OpenLine: current.openLine,
							Marker:   string(make([]byte, current.markerLen, current.markerLen)),
							Indent:   current.indent,
						})
						current = nil
					}
				}
			}
		}
		pos = lineEnd + 1 // +1 for newline
	}

	// Unclosed fence extends to end of buffer.
	if current != nil {
		spans = append(spans, fenceSpan{
			Start:    current.start,
			End:      len(text),
			OpenLine: current.openLine,
			Marker:   string(make([]byte, current.markerLen, current.markerLen)),
			Indent:   current.indent,
		})
	}

	data, err := json.Marshal(spans)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// MarkdownToPlainText is a convenience wrapper that strips markdown formatting.
func MarkdownToPlainText(markdown string) (string, error) {
	raw, err := MarkdownToIR(markdown, "")
	if err != nil {
		return "", err
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	return result.Text, nil
}
