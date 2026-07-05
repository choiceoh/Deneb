// html.go implements the labeled-HTML wire format for deneb-ui blocks (v2).
//
// Rationale ("AST as HTML"): models carry deep pre-trained fluency in HTML —
// tags, attributes, nesting — while the previous custom JSON schema needed a
// client-side repair layer for LLM-mangled output (broken keys, truncated
// braces). The HTML body is parsed by a deliberately small XML-lite tokenizer
// (NOT an HTML5 parser: no foster-parenting, so real <table>/<tr>/<td> and
// custom tags coexist predictably) and converted into the same map[string]any
// node tree the legacy JSON path produces, so validation (nodeSpecs) and every
// downstream consumer are shared between both formats.
//
// Grammar single source of truth: docs/research/deneb-ui-html.md. The Kotlin
// (DenebUiHtml.kt) and TypeScript (denebUiHtml.ts) parsers port these exact
// rules; keep the shared test vectors in sync when changing behavior here.
package denebui

import (
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Tag tables (mirror docs/research/deneb-ui-html.md)
// ---------------------------------------------------------------------------

// voidTags never take children; a matching close tag is tolerated and ignored.
var voidTags = map[string]bool{
	"hr": true, "img": true, "input": true, "icon": true, "slider": true,
	"progress": true, "avatar": true, "point": true, "br": true,
}

// rawTextTags capture verbatim content up to their literal close tag; only
// entities are decoded afterwards (so &#96; escapes backtick fences).
var rawTextTags = map[string]bool{"markdown": true, "code": true}

// autoClose maps an opening tag to the set of open tags it implicitly closes
// when found as a sibling (HTML5 habit: models rarely close li/option/td).
var autoClose = map[string][]string{
	"li":     {"li"},
	"option": {"option"},
	"td":     {"td", "th"},
	"th":     {"td", "th"},
	"tr":     {"td", "th", "tr"},
	"tab":    {"tab"},
	"chip":   {"chip"},
	"point":  {"point"},
}

// containerTags accept implicit text runs as text nodes.
var containerTags = map[string]bool{
	"column": true, "col": true, "row": true, "card": true, "box": true,
	"accordion": true, "li": true, "tab": true,
}

// ---------------------------------------------------------------------------
// Public entry
// ---------------------------------------------------------------------------

// IsHTMLBody reports whether a fence body uses the HTML wire format.
func IsHTMLBody(body string) bool {
	return strings.HasPrefix(strings.TrimSpace(body), "<")
}

// ParseHTML converts a labeled-HTML fence body into the shared node-tree shape
// (map[string]any, same as the JSON path) plus any parse-level issues (unknown
// tags, empty body). The returned tree may be nil when nothing usable parsed.
func ParseHTML(body string) (any, []Issue) {
	p := &htmlParser{src: strings.TrimSpace(body)}
	nodes := p.parseNodes()
	switch len(nodes) {
	case 0:
		p.issues = append(p.issues, Issue{"$", "empty deneb-ui block"})
		return nil, p.issues
	case 1:
		return nodes[0], p.issues
	default:
		return map[string]any{"type": "column", "children": nodes}, p.issues
	}
}

// ---------------------------------------------------------------------------
// Tokenizer + tree builder
// ---------------------------------------------------------------------------

type openElem struct {
	tag      string
	attrs    map[string]string
	children []any    // converted DenebUiNode maps
	structs  []any    // structural intermediates (option/chip/tab/tr cell/point)
	text     []string // text runs (for label/value content)
}

type htmlParser struct {
	src    string
	pos    int
	stack  []*openElem
	roots  []any
	issues []Issue
}

func (p *htmlParser) parseNodes() []any {
	for p.pos < len(p.src) {
		lt := strings.IndexByte(p.src[p.pos:], '<')
		if lt < 0 {
			p.emitText(p.src[p.pos:])
			break
		}
		if lt > 0 {
			p.emitText(p.src[p.pos : p.pos+lt])
			p.pos += lt
		}
		if !p.parseTag() {
			// Not a real tag ("a < b"): keep the '<' as literal text.
			p.emitText("<")
			p.pos++
		}
	}
	// EOF auto-close (streaming truncation resilience).
	for len(p.stack) > 0 {
		p.closeTop()
	}
	return p.roots
}

// parseTag consumes one <...> construct at p.pos ('<'). Returns false when the
// '<' does not start a tag/comment/doctype.
func (p *htmlParser) parseTag() bool {
	s, i := p.src, p.pos
	if i+1 >= len(s) {
		return false
	}
	switch {
	case strings.HasPrefix(s[i:], "<!--"):
		end := strings.Index(s[i+4:], "-->")
		if end < 0 {
			p.pos = len(s)
		} else {
			p.pos = i + 4 + end + 3
		}
		return true
	case s[i+1] == '!': // <!DOCTYPE ...>
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			p.pos = len(s)
		} else {
			p.pos = i + end + 1
		}
		return true
	case s[i+1] == '/':
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			return false
		}
		name := strings.ToLower(strings.TrimSpace(s[i+2 : i+end]))
		p.pos = i + end + 1
		p.handleClose(name)
		return true
	}
	if !isNameStart(s[i+1]) {
		return false
	}
	// Opening tag: parse name + attributes up to '>'.
	j := i + 1
	for j < len(s) && isNameChar(s[j]) {
		j++
	}
	name := strings.ToLower(s[i+1 : j])
	attrs, end, selfClose, ok := parseAttrs(s, j)
	if !ok {
		return false
	}
	p.pos = end
	p.handleOpen(name, attrs, selfClose)
	return true
}

// parseAttrs scans attributes from s[j:] until '>'. Returns the attr map, the
// index just past '>', whether the tag self-closed, and ok=false when no '>'
// exists before EOF-with-no-tag-shape (treated as literal text by the caller
// only when the remainder clearly isn't a tag; a truncated streaming tag is
// consumed to EOF instead).
func parseAttrs(s string, j int) (map[string]string, int, bool, bool) {
	attrs := map[string]string{}
	selfClose := false
	for {
		for j < len(s) && isSpace(s[j]) {
			j++
		}
		if j >= len(s) {
			return attrs, j, selfClose, true // truncated tag: swallow to EOF
		}
		if s[j] == '>' {
			return attrs, j + 1, selfClose, true
		}
		if s[j] == '/' {
			selfClose = true
			j++
			continue
		}
		if !isNameStart(s[j]) { // stray char inside tag — skip it
			j++
			continue
		}
		k := j
		for k < len(s) && isNameChar(s[k]) {
			k++
		}
		key := strings.ToLower(s[j:k])
		j = k
		for j < len(s) && isSpace(s[j]) {
			j++
		}
		val := ""
		hasVal := false
		if j < len(s) && s[j] == '=' {
			j++
			for j < len(s) && isSpace(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == '"' || s[j] == '\'') {
				q := s[j]
				j++
				k = j
				for k < len(s) && s[k] != q {
					k++
				}
				val = s[j:k]
				j = min(k+1, len(s))
			} else {
				k = j
				for k < len(s) && !isSpace(s[k]) && s[k] != '>' && s[k] != '/' {
					k++
				}
				val = s[j:k]
				j = k
			}
			hasVal = true
		}
		if !hasVal {
			val = "true" // boolean attribute
		}
		attrs[key] = decodeEntities(val)
	}
}

func (p *htmlParser) handleOpen(name string, attrs map[string]string, selfClose bool) {
	// Sibling auto-close (li→li, option→option, tr→td/th/tr, …).
	if closers, ok := autoClose[name]; ok {
		for len(p.stack) > 0 {
			top := p.stack[len(p.stack)-1].tag
			if !containsStr(closers, top) {
				break
			}
			p.closeTop()
		}
	}
	if rawTextTags[name] && !selfClose {
		p.captureRawText(name, attrs)
		return
	}
	el := &openElem{tag: name, attrs: attrs}
	if voidTags[name] || selfClose {
		p.attach(convertElem(el, p))
		return
	}
	p.stack = append(p.stack, el)
}

// captureRawText consumes verbatim content for markdown/code up to the literal
// close tag (case-insensitive); EOF closes implicitly.
func (p *htmlParser) captureRawText(name string, attrs map[string]string) {
	lowerSrc := strings.ToLower(p.src)
	end := strings.Index(lowerSrc[p.pos:], "</"+name)
	var raw string
	if end < 0 {
		raw = p.src[p.pos:]
		p.pos = len(p.src)
	} else {
		raw = p.src[p.pos : p.pos+end]
		gt := strings.IndexByte(p.src[p.pos+end:], '>')
		if gt < 0 {
			p.pos = len(p.src)
		} else {
			p.pos = p.pos + end + gt + 1
		}
	}
	el := &openElem{tag: name, attrs: attrs, text: []string{decodeEntities(raw)}}
	p.attach(convertElem(el, p))
}

func (p *htmlParser) handleClose(name string) {
	if voidTags[name] {
		return // tolerated stray close of a void tag
	}
	// Find the matching open element; close everything above it.
	for idx := len(p.stack) - 1; idx >= 0; idx-- {
		if p.stack[idx].tag == name {
			for len(p.stack) > idx {
				p.closeTop()
			}
			return
		}
	}
	// No matching open tag: ignore the stray close.
}

func (p *htmlParser) closeTop() {
	el := p.stack[len(p.stack)-1]
	p.stack = p.stack[:len(p.stack)-1]
	p.attach(convertElem(el, p))
}

// attach adds a converted node (or structural intermediate) to the current
// parent, or to the roots when the stack is empty. nil results are dropped.
func (p *htmlParser) attach(v any) {
	if v == nil {
		return
	}
	if len(p.stack) == 0 {
		if _, isStruct := v.(structural); isStruct {
			return // option/chip/… floating at root: drop
		}
		p.roots = append(p.roots, v)
		return
	}
	top := p.stack[len(p.stack)-1]
	if _, isStruct := v.(structural); isStruct {
		top.structs = append(top.structs, v)
		return
	}
	top.children = append(top.children, v)
}

func (p *htmlParser) emitText(t string) {
	if strings.TrimSpace(t) == "" {
		return
	}
	if len(p.stack) == 0 {
		p.roots = append(p.roots, map[string]any{"type": "text", "value": strings.TrimSpace(decodeEntities(t))})
		return
	}
	top := p.stack[len(p.stack)-1]
	top.text = append(top.text, decodeEntities(t))
	if containerTags[top.tag] {
		// Containers surface text runs as implicit text nodes, in order.
		top.children = append(top.children, map[string]any{"type": "text", "value": strings.TrimSpace(decodeEntities(t))})
	}
}

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
		node["type"] = "text"
		putID()
		node["value"] = inner
		putStr(node, "style", a["style"])
		putBool(node, "bold", a, "bold")
		putBool(node, "italic", a, "italic")
		putStr(node, "color", a["color"])
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
		putStr(node, "color", a["color"])
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
		putNum(node, "value", a["value"], false)
		putStr(node, "label", a["label"])
	case "alert":
		node["type"] = "alert"
		putID()
		node["message"] = firstNonEmpty(a["message"], inner)
		putStr(node, "title", a["title"])
		putStr(node, "severity", a["severity"])
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
		putStr(node, "chartType", a["type"])
		putStr(node, "label", a["label"])
		labels, values := []any{}, []any{}
		for _, s := range el.structs {
			pt, ok := s.(structural)
			if !ok || pt.kind != "point" {
				continue
			}
			labels = append(labels, pt.attrs["label"])
			f, _ := strconv.ParseFloat(strings.TrimSpace(pt.attrs["value"]), 64)
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
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return
	}
	if integer {
		m[k] = float64(int64(f)) // JSON numbers arrive as float64; keep parity
	} else {
		m[k] = f
	}
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

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isNameStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isNameChar(c byte) bool {
	return isNameStart(c) || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// decodeEntities decodes the small entity set the grammar guarantees:
// &lt; &gt; &amp; &quot; &#39;/&apos; &nbsp; and numeric &#NN; / &#xHH;.
func decodeEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		semi := strings.IndexByte(s[i:], ';')
		if semi < 0 || semi > 10 {
			b.WriteByte(s[i])
			i++
			continue
		}
		ent := s[i+1 : i+semi]
		switch strings.ToLower(ent) {
		case "lt":
			b.WriteByte('<')
		case "gt":
			b.WriteByte('>')
		case "amp":
			b.WriteByte('&')
		case "quot":
			b.WriteByte('"')
		case "apos":
			b.WriteByte('\'')
		case "nbsp":
			b.WriteByte(' ')
		default:
			if strings.HasPrefix(ent, "#") {
				var code int64
				var err error
				if len(ent) > 1 && (ent[1] == 'x' || ent[1] == 'X') {
					code, err = strconv.ParseInt(ent[2:], 16, 32)
				} else {
					code, err = strconv.ParseInt(ent[1:], 10, 32)
				}
				if err == nil && code > 0 {
					b.WriteRune(rune(code))
				} else {
					b.WriteByte(s[i])
					i++
					continue
				}
			} else {
				b.WriteByte(s[i])
				i++
				continue
			}
		}
		i += semi + 1
	}
	return b.String()
}
