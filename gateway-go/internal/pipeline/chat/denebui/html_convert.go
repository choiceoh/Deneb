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
	inner := strings.TrimSpace(strings.Join(el.text, ""))
	// Each family receives the same fresh node. Non-matching families leave it
	// untouched; matching families have no effects outside the returned value.
	node := map[string]any{}
	var value any
	var ok bool
	if value, ok = convertLayoutElem(el, node); !ok {
		if value, ok = convertContentElem(el, inner, node); !ok {
			if value, ok = convertStructuredElem(el, inner, node); !ok {
				if value, ok = convertFormElem(el, inner, node); !ok {
					p.issues = append(p.issues, Issue{"$", "unknown tag <" + el.tag + ">"})
					return nil
				}
			}
		}
	}
	// longpress="event" (+ data-*) attaches a long-press action to ANY node —
	// the renderers bind it as a press-hold callback. Universal here so a
	// deadline <row> (server-assembled morning card) carries it without each
	// family converter knowing about it. Tap actions stay via actionFromAttrs.
	if m, ok := value.(map[string]any); ok {
		if lp := longPressActionFromAttrs(el.attrs); lp != nil {
			m["longPressAction"] = lp
		}
	}
	return value
}

// longPressActionFromAttrs builds a callback UiAction from a longpress="event"
// attribute (+ data-* payload). Distinct from actionFromAttrs's event= (tap):
// a node may carry a tap action, a long-press action, or both.
func longPressActionFromAttrs(a map[string]string) map[string]any {
	ev := strings.TrimSpace(a["longpress"])
	if ev == "" {
		return nil
	}
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
	return act
}

func putNodeID(node map[string]any, attrs map[string]string) {
	if value := attrs["id"]; value != "" {
		node["id"] = value
	}
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
