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
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Tag tables (mirror docs/research/deneb-ui-html.md)
// ---------------------------------------------------------------------------

// voidTags never take children; a matching close tag is tolerated and ignored.
var voidTags = map[string]bool{
	"hr": true, "img": true, "input": true, "icon": true, "slider": true,
	"progress": true, "avatar": true, "point": true, "br": true, "spacer": true,
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

// inlineTags are HTML formatting habits with no node of their own: they merge
// back into the parent text flow as markdown-marked runs ("**"/"*"), which the
// renderers' inline tokenizers already draw — content survives instead of the
// whole subtree dropping. Empty marker = keep bare text.
var inlineTags = map[string]string{
	"b": "**", "strong": "**", "i": "*", "em": "*",
	"u": "", "s": "", "del": "", "strike": "", "mark": "",
	"small": "", "span": "", "sub": "", "sup": "", "a": "",
}

// genericTags are structural HTML wrappers (div soup) models emit out of
// pre-trained habit. They produce no node: children hoist to the parent and
// bare text becomes implicit text nodes. Accepted fluency — no Issue.
var genericTags = map[string]bool{
	"div": true, "section": true, "article": true, "header": true,
	"footer": true, "main": true, "aside": true, "figure": true,
	"center": true, "nav": true, "thead": true, "tbody": true, "tfoot": true,
}

// knownTags is every tag convertElem maps to a node or structural. Tags in
// none of the tables (knownTags/genericTags/inlineTags/voidTags) unwrap like
// genericTags but keep the validator Issue, so typos stay visible in health
// telemetry while the content still renders.
var knownTags = map[string]bool{
	"column": true, "col": true, "row": true, "card": true, "box": true,
	"hr": true, "divider": true, "text": true, "markdown": true,
	"img": true, "image": true, "icon": true, "code": true,
	"blockquote": true, "quote": true, "badge": true, "stat": true,
	"avatar": true, "progress": true, "alert": true, "countdown": true,
	"chart": true, "point": true, "table": true, "tr": true, "td": true,
	"th": true, "ul": true, "ol": true, "list": true, "li": true,
	"tabs": true, "tab": true, "accordion": true, "button": true,
	"input": true, "textarea": true, "checkbox": true, "switch": true,
	"select": true, "radio-group": true, "radiogroup": true, "option": true,
	"slider": true, "chips": true, "chip-group": true, "chip": true,
	"br": true, "p": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "title": true, "label": true, "spacer": true,
	"kv": true,
}

// treatsTextAsChildren reports whether bare text inside a tag surfaces as
// implicit child nodes (containers, generic wrappers, unknown tags) rather
// than feeding the element's own value slot (text/badge/li/… and inline tags).
func treatsTextAsChildren(tag string) bool {
	if containerTags[tag] || genericTags[tag] {
		return true
	}
	if _, inline := inlineTags[tag]; inline {
		return false
	}
	return !knownTags[tag]
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
	pending  []string // buffered implicit-text runs, flushed as one merged node
	// pendingSpace records a whitespace-only run between two text runs; the
	// next run gets one leading space so inline merges don't glue
	// (<b>A</b> <b>B</b> → "**A** **B**", not "**A****B**").
	pendingSpace bool
}

type htmlParser struct {
	src              string
	pos              int
	stack            []*openElem
	roots            []any
	rootPending      []string // buffered root-level text runs
	rootPendingSpace bool     // root-level counterpart of openElem.pendingSpace
	issues           []Issue
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
	p.flushRootPending()
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
		var key, val string
		key, j = scanAttrName(s, j)
		val, j = scanAttrValue(s, j)
		attrs[key] = decodeEntities(val)
	}
}

// scanAttrName consumes the (ASCII-lowercased) attribute name at s[j:] and
// returns it with the index just past it.
func scanAttrName(s string, j int) (string, int) {
	k := j
	for k < len(s) && isNameChar(s[k]) {
		k++
	}
	return strings.ToLower(s[j:k]), k
}

// scanAttrValue consumes optional whitespace, '=' and the quoted or bare value
// at s[j:], returning the raw (undecoded) value and the index just past it.
// A valueless attribute is boolean: "true".
func scanAttrValue(s string, j int) (string, int) {
	for j < len(s) && isSpace(s[j]) {
		j++
	}
	if j >= len(s) || s[j] != '=' {
		return "true", j // boolean attribute
	}
	j++
	for j < len(s) && isSpace(s[j]) {
		j++
	}
	if j < len(s) && (s[j] == '"' || s[j] == '\'') {
		return scanQuotedAttrValue(s, j)
	}
	return scanBareAttrValue(s, j)
}

// scanQuotedAttrValue consumes a double- or single-quoted attribute value at
// s[j:]; an unterminated quote swallows to EOF.
func scanQuotedAttrValue(s string, j int) (string, int) {
	q := s[j]
	j++
	k := j
	for k < len(s) && s[k] != q {
		k++
	}
	return s[j:k], min(k+1, len(s))
}

// scanBareAttrValue consumes an unquoted attribute value at s[j:], ending at
// whitespace, '>' or '/'.
func scanBareAttrValue(s string, j int) (string, int) {
	k := j
	for k < len(s) && !isSpace(s[k]) && s[k] != '>' && s[k] != '/' {
		k++
	}
	return s[j:k], k
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
		// Self-closed inline/generic tags carry no content — nothing to emit.
		if _, inline := inlineTags[name]; inline || genericTags[name] {
			return
		}
		if !knownTags[name] {
			p.issues = append(p.issues, Issue{"$", "unknown tag <" + name + ">"})
			return
		}
		p.attach(convertElem(el, p))
		return
	}
	p.stack = append(p.stack, el)
}

// captureRawText consumes verbatim content for markdown/code up to the literal
// close tag (case-insensitive); EOF closes implicitly.
func (p *htmlParser) captureRawText(name string, attrs map[string]string) {
	end := indexOfCloseTag(p.src, p.pos, name)
	var raw string
	if end < 0 {
		raw = p.src[p.pos:]
		p.pos = len(p.src)
	} else {
		raw = p.src[p.pos:end]
		gt := strings.IndexByte(p.src[end:], '>')
		if gt < 0 {
			p.pos = len(p.src)
		} else {
			p.pos = end + gt + 1
		}
	}
	decoded := decodeEntities(raw)
	// Inline habit: <code> inside a text flow ("명령 <code>make ci</code> 실행")
	// merges as a backtick run instead of breaking the sentence into a block.
	if name == "code" && p.inlineCodeContext() {
		if t := strings.TrimSpace(decoded); t != "" {
			p.emitRun("`" + t + "`")
		}
		return
	}
	el := &openElem{tag: name, attrs: attrs, text: []string{decoded}}
	p.attach(convertElem(el, p))
}

// inlineCodeContext reports whether raw <code> content should merge into the
// enclosing text flow (parent is a text node or an inline formatting tag).
func (p *htmlParser) inlineCodeContext() bool {
	if len(p.stack) == 0 {
		return false
	}
	tag := p.stack[len(p.stack)-1].tag
	if tag == "text" {
		return true
	}
	_, inline := inlineTags[tag]
	return inline
}

// indexOfCloseTag returns the absolute index of the first "</name" at or after
// from, comparing the (ASCII, whitelist-only) tag name case-insensitively via
// manual byte folding. Never lowercase the whole source for index math:
// Unicode case mapping can change byte length (e.g. 'İ' → "i̇"), skewing
// indexes into the original string — the fuzzer-found slice panic.
func indexOfCloseTag(s string, from int, name string) int {
	n := len(name)
	for i := from; i+2+n <= len(s); i++ {
		if s[i] != '<' || s[i+1] != '/' {
			continue
		}
		match := true
		for k := 0; k < n; k++ {
			c := s[i+2+k]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != name[k] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
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
	if marker, inline := inlineTags[el.tag]; inline {
		p.emitInline(el, marker)
		return
	}
	if genericTags[el.tag] || !knownTags[el.tag] {
		// Unwrap: the wrapper produces no node; its children (including the
		// flushed implicit text) hoist to the parent in source order.
		// Structural intermediates hoist too, so <thead>/<tbody> table rows
		// reach the enclosing <table> instead of vanishing with the wrapper.
		if !genericTags[el.tag] {
			p.issues = append(p.issues, Issue{"$", "unknown tag <" + el.tag + "> (children hoisted)"})
		}
		p.flushPending(el)
		for _, c := range el.children {
			p.attach(c)
		}
		for _, s := range el.structs {
			p.attach(s)
		}
		return
	}
	p.flushPending(el)
	p.attach(convertElem(el, p))
}

// emitInline merges an inline formatting element back into the parent text
// flow (<b>중요</b> → "**중요**"). Plain-value slots (badge, button labels)
// receive bare text — literal markers would render as noise there. Any real
// child nodes (rare: <b><icon/></b>) hoist to the parent afterwards.
func (p *htmlParser) emitInline(el *openElem, marker string) {
	inner := strings.TrimSpace(strings.Join(el.text, ""))
	if inner != "" {
		run := inner
		if p.inlineMarkupAllowed() {
			if el.tag == "a" {
				if href := el.attrs["href"]; href != "" {
					run = "[" + inner + "](" + href + ")"
				}
			} else if marker != "" {
				run = marker + inner + marker
			}
		}
		p.emitRun(run)
	}
	for _, c := range el.children {
		p.attach(c)
	}
}

// inlineMarkupAllowed reports whether the current attach target renders inline
// markdown (text nodes, containers, root) — those get **/*/[]() markers.
func (p *htmlParser) inlineMarkupAllowed() bool {
	if len(p.stack) == 0 {
		return true
	}
	tag := p.stack[len(p.stack)-1].tag
	return tag == "text" || treatsTextAsChildren(tag)
}

// attach adds a converted node (or structural intermediate) to the current
// parent, or to the roots when the stack is empty. nil results are dropped.
// Buffered implicit text flushes first so source order is preserved.
func (p *htmlParser) attach(v any) {
	if v == nil {
		return
	}
	if len(p.stack) == 0 {
		if _, isStruct := v.(structural); isStruct {
			return // option/chip/… floating at root: drop
		}
		p.flushRootPending()
		p.rootPendingSpace = false // whitespace before a block child is layout, not a separator
		p.roots = append(p.roots, v)
		return
	}
	top := p.stack[len(p.stack)-1]
	top.pendingSpace = false
	if _, isStruct := v.(structural); isStruct {
		top.structs = append(top.structs, v)
		return
	}
	p.flushPending(top)
	top.children = append(top.children, v)
}

func (p *htmlParser) emitText(t string) {
	if strings.TrimSpace(t) == "" {
		p.markPendingSpace()
		return
	}
	p.emitRun(decodeEntities(t))
}

// markPendingSpace remembers that a whitespace-only run arrived after existing
// text, so the next run in the same flow keeps a single separating space.
func (p *htmlParser) markPendingSpace() {
	if len(p.stack) == 0 {
		if len(p.rootPending) > 0 {
			p.rootPendingSpace = true
		}
		return
	}
	if top := p.stack[len(p.stack)-1]; len(top.text) > 0 {
		top.pendingSpace = true
	}
}

// emitRun adds an already-decoded text run (entity decoding must not repeat —
// inline merges re-emit runs that were decoded on first capture).
func (p *htmlParser) emitRun(t string) {
	if strings.TrimSpace(t) == "" {
		return
	}
	if len(p.stack) == 0 {
		if p.rootPendingSpace {
			t = " " + t
			p.rootPendingSpace = false
		}
		p.rootPending = append(p.rootPending, t)
		return
	}
	top := p.stack[len(p.stack)-1]
	if top.pendingSpace {
		t = " " + t
		top.pendingSpace = false
	}
	top.text = append(top.text, t)
	if treatsTextAsChildren(top.tag) {
		top.pending = append(top.pending, t)
	}
}

// flushPending materializes an element's buffered text runs as one merged
// implicit node. Merging (instead of one node per run) keeps sentences split
// by inline tags whole, and lets markdown block structure be recognized.
func (p *htmlParser) flushPending(el *openElem) {
	if len(el.pending) == 0 {
		return
	}
	node := textBlockNode(strings.Join(el.pending, ""))
	el.pending = nil
	el.children = append(el.children, node)
}

func (p *htmlParser) flushRootPending() {
	if len(p.rootPending) == 0 {
		return
	}
	node := textBlockNode(strings.Join(p.rootPending, ""))
	p.rootPending = nil
	p.roots = append(p.roots, node)
}

// textBlockNode wraps a merged text run as an implicit node — as markdown when
// the run carries markdown block structure (auto-correcting the "markdown
// table inside a card" habit: the markdown node routes through the full
// markdown renderer, tables included), else as plain text.
func textBlockNode(s string) map[string]any {
	s = strings.TrimSpace(s)
	if looksLikeMarkdownBlock(s) {
		return map[string]any{"type": "markdown", "value": s}
	}
	return map[string]any{"type": "text", "value": s}
}

// looksLikeMarkdownBlock reports whether text carries markdown block structure
// (table rows, headings, list runs, fences) that a plain text node would
// render broken. Conservative: single bullets or lone pipes stay text.
func looksLikeMarkdownBlock(s string) bool {
	if strings.Contains(s, "```") {
		return true
	}
	pipeRows, bullets := 0, 0
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "|") {
			if pipeRows++; pipeRows >= 2 {
				return true
			}
			continue
		}
		if isMarkdownHeading(t) {
			return true
		}
		if isMarkdownBullet(t) {
			if bullets++; bullets >= 2 {
				return true
			}
		}
	}
	return false
}

func isMarkdownHeading(t string) bool {
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && n < len(t) && t[n] == ' '
}

func isMarkdownBullet(t string) bool {
	if len(t) >= 2 && (t[0] == '-' || t[0] == '*') && t[1] == ' ' {
		return true
	}
	if strings.HasPrefix(t, "• ") {
		return true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i >= 1 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' '
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
				if err == nil && code > 0 && code <= utf8.MaxRune && !(code >= 0xd800 && code <= 0xdfff) {
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
