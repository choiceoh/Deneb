// Element-to-node conversion for the labeled deneb-ui HTML parser.
package denebui

import (
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Element → node conversion
// ---------------------------------------------------------------------------

// structural marks intermediates consumed by their parent (not UI nodes).
type structural struct {
	kind     string // "option" | "chip" | "tab" | "tr" | "cell" | "point"
	text     string
	attrs    map[string]string
	children []any
	structs  []any
}

func convertElem(el *openElem, p *htmlParser) any {
	a := el.attrs
	inner := strings.TrimSpace(strings.Join(el.text, ""))
	node := map[string]any{}
	putID := func() {
		if v := a["id"]; v != "" {
			node["id"] = v
		}
	}
	switch el.tag {
	case "column", "col":
		node["type"] = "column"
		putID()
		node["children"] = el.children
	case "row":
		node["type"] = "row"
		putID()
		node["children"] = el.children
	case "card":
		node["type"] = "card"
		putID()
		node["children"] = el.children
	case "box":
		node["type"] = "box"
		putID()
		node["children"] = el.children
		putStr(node, "contentAlignment", a["align"])
	case "hr", "divider":
		node["type"] = "divider"
		putID()
	case "text":
		if looksLikeMarkdownBlock(inner) {
			// Whole markdown blocks stuffed into <text> (tables, bullet runs)
			// upgrade to a markdown node so they render structured.
			node["type"] = "markdown"
			putID()
			node["value"] = inner
			return node
		}
		node["type"] = "text"
		putID()
		node["value"] = inner
		putStr(node, "style", canonTextStyle(a["style"]))
		putBool(node, "bold", a, "bold")
		putBool(node, "italic", a, "italic")
		putStr(node, "color", a["color"])
	case "p", "h1", "h2", "h3", "h4", "h5", "h6":
		// HTML fluency aliases: paragraphs and headings map onto text nodes.
		if inner == "" && len(el.children) == 0 {
			return nil
		}
		node["type"] = "text"
		putID()
		node["value"] = inner
		switch el.tag {
		case "h1":
			node["style"] = "headline"
		case "h2", "h3":
			node["style"] = "title"
		case "h4", "h5", "h6":
			node["bold"] = true
		}
		if len(el.children) > 0 {
			// Block children inside a paragraph (models nest freely): keep
			// both by wrapping in a column, text first.
			kids := []any{}
			if inner != "" {
				kids = append(kids, node)
			}
			kids = append(kids, el.children...)
			return map[string]any{"type": "column", "children": kids}
		}
	case "markdown":
		node["type"] = "markdown"
		putID()
		node["value"] = strings.TrimSpace(strings.Join(el.text, ""))
	case "img", "image":
		node["type"] = "image"
		putID()
		node["url"] = firstNonEmpty(a["src"], a["url"])
		putStr(node, "alt", a["alt"])
		putNum(node, "height", a["height"], true)
		putNum(node, "aspectRatio", a["aspect-ratio"], false)
	case "icon":
		node["type"] = "icon"
		putID()
		node["name"] = a["name"]
		putNum(node, "size", a["size"], true)
		putStr(node, "color", a["color"])
	case "code":
		node["type"] = "code"
		putID()
		node["code"] = strings.TrimSpace(strings.Join(el.text, ""))
		putStr(node, "language", firstNonEmpty(a["language"], a["lang"]))
	case "blockquote", "quote":
		node["type"] = "quote"
		putID()
		node["text"] = inner
		putStr(node, "source", a["source"])
	case "badge":
		node["type"] = "badge"
		putID()
		node["value"] = firstNonEmpty(a["value"], inner)
		putStr(node, "color", canonBadgeColor(a["color"]))
	case "stat":
		node["type"] = "stat"
		putID()
		node["value"] = firstNonEmpty(a["value"], inner)
		node["label"] = a["label"]
		putStr(node, "description", a["description"])
	case "avatar":
		node["type"] = "avatar"
		putID()
		putStr(node, "name", a["name"])
		putStr(node, "imageUrl", firstNonEmpty(a["src"], a["image-url"]))
		putNum(node, "size", a["size"], true)
	case "progress":
		node["type"] = "progress"
		putID()
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
		putID()
		node["message"] = firstNonEmpty(a["message"], inner)
		putStr(node, "title", a["title"])
		putStr(node, "severity", canonSeverity(a["severity"]))
	case "countdown":
		node["type"] = "countdown"
		putID()
		putNum(node, "seconds", a["seconds"], true)
		putStr(node, "label", firstNonEmpty(a["label"], inner))
		if act := actionFromAttrs(a); act != nil {
			node["action"] = act
		}
	case "chart":
		node["type"] = "chart"
		putID()
		putStr(node, "chartType", canonChartType(a["type"]))
		putStr(node, "label", a["label"])
		labels, values := []any{}, []any{}
		for _, s := range el.structs {
			pt, ok := s.(structural)
			if !ok || pt.kind != "point" {
				continue
			}
			labels = append(labels, pt.attrs["label"])
			f, _ := lenientFloat(pt.attrs["value"])
			values = append(values, f)
		}
		node["labels"], node["values"] = labels, values
	case "point":
		return structural{kind: "point", attrs: a}
	case "table":
		node["type"] = "table"
		putID()
		headers, rows := []any{}, []any{}
		for _, s := range el.structs {
			tr, ok := s.(structural)
			if !ok || tr.kind != "tr" {
				continue
			}
			var cells []any
			hasTH := false
			for _, cs := range tr.structs {
				c, cok := cs.(structural)
				if !cok || c.kind != "cell" {
					continue
				}
				if c.attrs["__th"] == "true" {
					hasTH = true
				}
				cells = append(cells, c.text)
			}
			if hasTH && len(headers) == 0 {
				headers = cells
			} else {
				rows = append(rows, cells)
			}
		}
		node["headers"], node["rows"] = headers, rows
	case "tr":
		return structural{kind: "tr", structs: el.structs}
	case "td", "th":
		th := "false"
		if el.tag == "th" {
			th = "true"
		}
		return structural{kind: "cell", text: inner, attrs: map[string]string{"__th": th}}
	case "ul", "ol", "list":
		node["type"] = "list"
		putID()
		ordered := el.tag == "ol" || truthy(a["ordered"])
		if ordered {
			node["ordered"] = true
		}
		items := []any{}
		for _, s := range el.structs {
			li, ok := s.(structural)
			if !ok || li.kind != "li" {
				continue
			}
			switch len(li.children) {
			case 0:
				items = append(items, map[string]any{"type": "text", "value": li.text})
			case 1:
				items = append(items, li.children[0])
			default:
				items = append(items, map[string]any{"type": "column", "children": li.children})
			}
		}
		node["items"] = items
	case "li":
		return structural{kind: "li", text: inner, children: el.children}
	case "tabs":
		node["type"] = "tabs"
		putID()
		putNum(node, "selectedIndex", a["selected-index"], true)
		tabs := []any{}
		for _, s := range el.structs {
			tb, ok := s.(structural)
			if !ok || tb.kind != "tab" {
				continue
			}
			tabs = append(tabs, map[string]any{"label": tb.attrs["label"], "children": tb.children})
		}
		node["tabs"] = tabs
	case "tab":
		return structural{kind: "tab", attrs: a, children: el.children}
	case "accordion":
		node["type"] = "accordion"
		putID()
		node["title"] = a["title"]
		node["children"] = el.children
		putBool(node, "expanded", a, "expanded")
	case "button":
		node["type"] = "button"
		putID()
		node["label"] = firstNonEmpty(a["label"], inner)
		putStr(node, "variant", a["variant"])
		putBool(node, "enabled", a, "enabled")
		if act := actionFromAttrs(a); act != nil {
			node["action"] = act
		}
	case "input":
		switch strings.ToLower(a["type"]) {
		case "date":
			node["type"] = "date_input"
			fillInput(node, a, false)
		case "time":
			node["type"] = "time_input"
			fillInput(node, a, false)
		case "checkbox":
			node["type"] = "checkbox"
			node["id"] = a["id"]
			node["label"] = a["label"]
			putBool(node, "checked", a, "checked")
		default:
			node["type"] = "text_input"
			fillInput(node, a, true)
		}
	case "textarea":
		node["type"] = "text_input"
		fillInput(node, a, true)
		node["multiline"] = true
		if inner != "" {
			node["value"] = inner
		}
	case "checkbox":
		node["type"] = "checkbox"
		node["id"] = a["id"]
		node["label"] = firstNonEmpty(a["label"], inner)
		putBool(node, "checked", a, "checked")
	case "switch":
		node["type"] = "switch"
		node["id"] = a["id"]
		node["label"] = firstNonEmpty(a["label"], inner)
		putBool(node, "checked", a, "checked")
	case "select", "radio-group", "radiogroup":
		if el.tag == "select" {
			node["type"] = "select"
			putStr(node, "placeholder", a["placeholder"])
		} else {
			node["type"] = "radio_group"
		}
		node["id"] = a["id"]
		putStr(node, "label", a["label"])
		putBool(node, "required", a, "required")
		opts := []any{}
		for _, s := range el.structs {
			op, ok := s.(structural)
			if !ok || op.kind != "option" {
				continue
			}
			opts = append(opts, op.text)
			if v, present := op.attrs["selected"]; present && truthy(v) {
				node["selected"] = op.text
			}
		}
		node["options"] = opts
		if v := a["selected"]; v != "" {
			node["selected"] = v
		}
	case "option":
		return structural{kind: "option", text: inner, attrs: a}
	case "slider":
		node["type"] = "slider"
		node["id"] = a["id"]
		putStr(node, "label", a["label"])
		putNum(node, "value", a["value"], false)
		putNum(node, "min", a["min"], false)
		putNum(node, "max", a["max"], false)
		putNum(node, "step", a["step"], false)
	case "chips", "chip-group":
		node["type"] = "chip_group"
		node["id"] = a["id"]
		if v := a["selection"]; v != "" {
			node["selection"] = v
		}
		putBool(node, "required", a, "required")
		chips := []any{}
		for _, s := range el.structs {
			ch, ok := s.(structural)
			if !ok || ch.kind != "chip" {
				continue
			}
			val := firstNonEmpty(ch.attrs["value"], ch.text)
			chips = append(chips, map[string]any{"label": ch.text, "value": val})
		}
		node["chips"] = chips
	case "chip":
		return structural{kind: "chip", text: inner, attrs: a}
	case "br":
		return nil
	default:
		p.issues = append(p.issues, Issue{"$", "unknown tag <" + el.tag + ">"})
		return nil
	}
	return node
}

func fillInput(node map[string]any, a map[string]string, textKind bool) {
	node["id"] = a["id"]
	putStr(node, "label", a["label"])
	if v, ok := a["value"]; ok && v != "" {
		node["value"] = v
	}
	putBool(node, "required", a, "required")
	if textKind {
		putStr(node, "placeholder", a["placeholder"])
		putStr(node, "keyboard", a["keyboard"])
		putBool(node, "multiline", a, "multiline")
	}
}

// actionFromAttrs maps action attributes to a UiAction map.
// Precedence: event > href > toggle > copy.
func actionFromAttrs(a map[string]string) map[string]any {
	if ev := a["event"]; ev != "" {
		act := map[string]any{"type": "callback", "event": ev}
		data := map[string]any{}
		for k, v := range a {
			if strings.HasPrefix(k, "data-") && len(k) > 5 {
				data[k[5:]] = v
			}
		}
		if len(data) > 0 {
			act["data"] = data
		}
		if c := strings.TrimSpace(a["collect"]); c != "" {
			var ids []any
			for _, id := range strings.Split(c, ",") {
				if id = strings.TrimSpace(id); id != "" {
					ids = append(ids, id)
				}
			}
			act["collectFrom"] = ids
		}
		return act
	}
	if u := a["href"]; u != "" {
		return map[string]any{"type": "open_url", "url": u}
	}
	if t := a["toggle"]; t != "" {
		return map[string]any{"type": "toggle", "targetId": t}
	}
	if c := a["copy"]; c != "" {
		return map[string]any{"type": "copy_to_clipboard", "text": c}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func putStr(m map[string]any, k, v string) {
	if v != "" {
		m[k] = v
	}
}

func putBool(m map[string]any, k string, a map[string]string, attr string) {
	if v, ok := a[attr]; ok {
		m[k] = truthy(v)
	}
}

func putNum(m map[string]any, k, v string, integer bool) {
	f, ok := lenientFloat(v)
	if !ok {
		return
	}
	if integer {
		m[k] = float64(int64(f)) // JSON numbers arrive as float64; keep parity
	} else {
		m[k] = f
	}
}

// lenientFloat extracts a number from a lenient attribute value: exact floats
// parse as-is; otherwise units, thousands commas, and stray symbols are
// tolerated ("1,200톤" → 1200, "68%" → 68, "16px" → 16). ok=false when the
// value carries no digits at all.
func lenientFloat(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f, true
	}
	start := -1
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, false
	}
	var b strings.Builder
	if start > 0 && v[start-1] == '-' {
		b.WriteByte('-')
	}
	dot := false
scan:
	for i := start; i < len(v); i++ {
		switch c := v[i]; {
		case c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == ',': // thousands separator: skip
		case c == '.' && !dot:
			dot = true
			b.WriteByte(c)
		default:
			break scan
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(b.String(), "."), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// canonBadgeColor folds common CSS color words onto the badge tint enum.
func canonBadgeColor(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "red":
		return "error"
	case "green":
		return "success"
	case "yellow", "amber", "orange":
		return "warning"
	case "blue":
		return "primary"
	case "gray", "grey", "neutral":
		return "secondary"
	}
	return v
}

// canonSeverity folds severity synonyms onto the alert enum.
func canonSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "warn", "caution":
		return "warning"
	case "danger", "critical", "fatal":
		return "error"
	case "ok", "done":
		return "success"
	case "note", "notice", "information":
		return "info"
	}
	return v
}

// canonChartType folds chart-type synonyms onto bar/line.
func canonChartType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "bars", "column", "columns":
		return "bar"
	case "lines", "area", "trend":
		return "line"
	}
	return v
}

// canonTextStyle folds text-style synonyms onto the style enum.
func canonTextStyle(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "heading", "header":
		return "headline"
	case "subtitle", "subheading":
		return "title"
	}
	return v
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "no", "off":
		return false
	default:
		return true // presence-only booleans ("required") count as true
	}
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
