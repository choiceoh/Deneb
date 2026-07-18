package denebui

// convertContentElem converts leaf-like elements that primarily present text,
// media, or a single status value.
func convertContentElem(el *openElem, inner string, node map[string]any) (any, bool) {
	a := el.attrs
	switch el.tag {
	case "text":
		if looksLikeMarkdownBlock(inner) {
			// Whole markdown blocks stuffed into <text> (tables, bullet runs)
			// upgrade to a markdown node so they render structured.
			node["type"] = "markdown"
			putNodeID(node, a)
			node["value"] = inner
			return node, true
		}
		node["type"] = "text"
		putNodeID(node, a)
		node["value"] = inner
		putStr(node, "style", canonTextStyle(a["style"]))
		putBool(node, "bold", a, "bold")
		putBool(node, "italic", a, "italic")
		putStr(node, "color", a["color"])
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "title", "label", "kv":
		// HTML fluency aliases: paragraphs and headings map onto text nodes.
		// title/label/kv are invented-but-frequent model habits promoted from
		// unknown-tag unwrap to proper typography (2026-07-18 reject telemetry).
		if el.tag == "kv" {
			// <kv label="발신">양도현</kv> → "발신 — 양도현" (the renderers'
			// key/value convention).
			if k := a["label"]; k != "" && inner != "" {
				inner = k + " — " + inner
			}
		}
		if inner == "" && len(el.children) == 0 {
			return nil, true
		}
		node["type"] = "text"
		putNodeID(node, a)
		node["value"] = inner
		switch el.tag {
		case "h1":
			node["style"] = "headline"
		case "h2", "h3", "title":
			node["style"] = "title"
		case "h4", "h5", "h6":
			node["bold"] = true
		case "label":
			node["style"] = "caption"
		}
		if len(el.children) > 0 {
			// Block children inside a paragraph (models nest freely): keep
			// both by wrapping in a column, text first.
			kids := []any{}
			if inner != "" {
				kids = append(kids, node)
			}
			kids = append(kids, el.children...)
			return map[string]any{"type": "column", "children": kids}, true
		}
	case "markdown":
		node["type"] = "markdown"
		putNodeID(node, a)
		node["value"] = inner
	case "img", "image":
		node["type"] = "image"
		putNodeID(node, a)
		node["url"] = firstNonEmpty(a["src"], a["url"])
		putStr(node, "alt", a["alt"])
		putNum(node, "height", a["height"], true)
		putNum(node, "aspectRatio", a["aspect-ratio"], false)
	case "icon":
		node["type"] = "icon"
		putNodeID(node, a)
		node["name"] = a["name"]
		putNum(node, "size", a["size"], true)
		putStr(node, "color", a["color"])
	case "code":
		node["type"] = "code"
		putNodeID(node, a)
		node["code"] = inner
		putStr(node, "language", firstNonEmpty(a["language"], a["lang"]))
	case "blockquote", "quote":
		node["type"] = "quote"
		putNodeID(node, a)
		node["text"] = inner
		putStr(node, "source", a["source"])
	case "badge":
		node["type"] = "badge"
		putNodeID(node, a)
		node["value"] = firstNonEmpty(a["value"], inner)
		putStr(node, "color", canonBadgeColor(a["color"]))
	case "stat":
		node["type"] = "stat"
		putNodeID(node, a)
		node["value"] = firstNonEmpty(a["value"], inner)
		node["label"] = a["label"]
		putStr(node, "description", a["description"])
	case "avatar":
		node["type"] = "avatar"
		putNodeID(node, a)
		putStr(node, "name", a["name"])
		putStr(node, "imageUrl", firstNonEmpty(a["src"], a["image-url"]))
		putNum(node, "size", a["size"], true)
	case "progress":
		node["type"] = "progress"
		putNodeID(node, a)
		if f, ok := lenientFloat(a["value"]); ok {
			// Percent tolerance: "68" / "68%" mean 68% — the 0..1 contract
			// only applies to values already in range.
			if f > 1 {
				f /= 100
			}
			node["value"] = min(max(f, 0), 1)
		}
		putStr(node, "label", a["label"])
	case "alert":
		node["type"] = "alert"
		putNodeID(node, a)
		node["message"] = firstNonEmpty(a["message"], inner)
		putStr(node, "title", a["title"])
		putStr(node, "severity", canonSeverity(a["severity"]))
	case "countdown":
		node["type"] = "countdown"
		putNodeID(node, a)
		putNum(node, "seconds", a["seconds"], true)
		putStr(node, "label", firstNonEmpty(a["label"], inner))
		if act := actionFromAttrs(a); act != nil {
			node["action"] = act
		}
	default:
		return nil, false
	}
	return node, true
}
