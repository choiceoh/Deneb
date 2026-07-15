package denebui

import "strings"

// PlainText projects a deneb-ui HTML body to the reader-facing text of the
// rendered card, in document order — what a person would read off the screen.
// Consumers are prose pipelines (work-feed title/summary extraction, push
// previews) that would otherwise stare at raw markup. Legacy JSON bodies and
// unparseable input project to "".
func PlainText(body string) string {
	body = strings.TrimSpace(body)
	if !IsHTMLBody(body) {
		return ""
	}
	root, _ := ParseHTML(body)
	if root == nil {
		return ""
	}
	var lines []string
	collectPlainText(root, &lines)
	return strings.Join(lines, "\n")
}

func collectPlainText(v any, out *[]string) {
	switch n := v.(type) {
	case []any:
		for _, e := range n {
			collectPlainText(e, out)
		}
	case map[string]any:
		appendPlain := func(key string) {
			if s, ok := n[key].(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					*out = append(*out, t)
				}
			}
		}
		switch n["type"] {
		case "text", "markdown", "badge":
			appendPlain("value")
		case "quote":
			appendPlain("text")
		case "alert":
			appendPlain("title")
			appendPlain("message")
		case "stat":
			label, _ := n["label"].(string)
			value, _ := n["value"].(string)
			if s := strings.TrimSpace(strings.TrimSpace(label) + " " + strings.TrimSpace(value)); s != "" {
				*out = append(*out, s)
			}
		case "progress", "countdown":
			appendPlain("label")
		}
		collectPlainText(n["children"], out)
		collectPlainText(n["items"], out)
		if tabs, ok := n["tabs"].([]any); ok {
			for _, t := range tabs {
				if tm, ok := t.(map[string]any); ok {
					collectPlainText(tm["children"], out)
				}
			}
		}
		// Tables project row-wise so a schedule/delivery table still reads.
		if headers, ok := n["headers"].([]any); ok {
			appendCellLine(headers, out)
		}
		if rows, ok := n["rows"].([]any); ok {
			for _, r := range rows {
				if cells, ok := r.([]any); ok {
					appendCellLine(cells, out)
				}
			}
		}
	}
}

func appendCellLine(cells []any, out *[]string) {
	var parts []string
	for _, c := range cells {
		if s, ok := c.(string); ok {
			if t := strings.TrimSpace(s); t != "" {
				parts = append(parts, t)
			}
		}
	}
	if len(parts) > 0 {
		*out = append(*out, strings.Join(parts, " · "))
	}
}

// ReplaceFences rewrites text with every deneb-ui fenced block replaced by
// repl(body) (the block is dropped when repl returns ""). A prose prefix
// glued to the opener line ("…했어요.```deneb-ui") and prose after a glued HTML
// close are preserved. Text without a fence returns unchanged.
func ReplaceFences(text string, repl func(body string) string) string {
	if !HasFence(text) {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		prefix, rest, open := denebUIFenceOpenParts(strings.TrimSpace(lines[i]))
		if !open {
			out = append(out, lines[i])
			continue
		}
		if prefix != "" {
			out = append(out, prefix)
		}
		var body []string
		suffix := ""
		isHTML := rest != ""
		htmlDecided := rest != ""
		closed := false
		if rest != "" {
			if pre, after, ok := splitGluedFenceCloseParts(rest); ok {
				body, suffix, closed = appendBodyLine(body, pre), after, true
			} else {
				body = append(body, rest)
			}
		}
		for !closed && i+1 < len(lines) {
			i++
			t := strings.TrimSpace(lines[i])
			if isFenceClose(t) {
				closed = true
				break
			}
			if !htmlDecided && t != "" {
				isHTML = strings.HasPrefix(t, "<")
				htmlDecided = true
			}
			if isHTML {
				if pre, after, ok := splitGluedFenceCloseParts(lines[i]); ok {
					body, suffix, closed = appendBodyLine(body, pre), after, true
					break
				}
			}
			body = append(body, lines[i])
		}
		if r := strings.TrimSpace(repl(strings.Join(body, "\n"))); r != "" {
			out = append(out, r)
		}
		if suffix != "" {
			out = append(out, suffix)
		}
	}
	return strings.Join(out, "\n")
}
