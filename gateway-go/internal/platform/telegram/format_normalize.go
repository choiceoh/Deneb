package telegram

import (
	"fmt"
	stdhtml "html"
	"regexp"
	"strings"
)

// Telegram-friendly normalization applied before coremarkdown parses the text.
//
// Models occasionally emit raw HTML tags (<b>...</b>) and entities (&#x27;)
// instead of markdown. The downstream pipeline HTML-escapes those, so users
// see literal "<b>" text in Telegram. Markdown tables are also problematic:
// Telegram has no native table rendering, and coremarkdown's TableMode="code"
// fallback wraps them in a code block which is hard to read on mobile.
//
// normalizeForTelegram does two things:
//  1. Rewrite raw HTML tags / entities into markdown equivalents.
//  2. Flatten markdown tables into bullet lines like
//     "- **Header1**: cell1 / **Header2**: cell2".
//
// Both passes preserve fenced code blocks and inline code regions verbatim,
// so a code example showing literal HTML or entities is not corrupted.
//
// Pure function, idempotent for already-clean markdown.
func normalizeForTelegram(s string) string {
	s = htmlToTelegramMarkdown(s)
	s = flattenTablesToBullets(s)
	return s
}

// Markdown code regions to protect from HTML rewriting and table flattening.
var (
	rxFmtMDFence      = regexp.MustCompile("(?s)```[^\\n]*\\n.*?\\n```")
	rxFmtMDInlineCode = regexp.MustCompile("`[^`\\n]+`")
)

// htmlToTelegramMarkdown rewrites HTML tags and entities into markdown.
// Existing markdown code regions are stashed behind NUL-bracketed placeholders
// before rewriting and restored afterward, so code examples that show literal
// HTML are preserved.
//
// Order matters: entity decoding (&lt;b&gt; → <b>) must run BEFORE the tag
// rewriter, otherwise an entity-escaped tag like &lt;b&gt;X&lt;/b&gt; passes
// through unchanged, gets decoded to raw <b> at the very end, and lands in the
// markdown parser which then re-escapes it → Telegram displays literal "<b>".
//
// The tag rewriter is a single-pass stack-based parser (see
// rewriteHTMLToMarkdown) that handles cross-nested and unbalanced tags
// correctly. Earlier regex-based versions failed on inputs like
// "<b><code>X</b></code>" because non-greedy `.*?` matched across the wrong
// closing tag and produced malformed markdown that broke Telegram's HTML
// parser, falling back to plain text and exposing the original tags to users.
func htmlToTelegramMarkdown(s string) string {
	if s == "" || !strings.ContainsAny(s, "<&") {
		return s
	}

	var saved []string
	stash := func(m string) string {
		saved = append(saved, m)
		return fmt.Sprintf("\x00C%d\x00", len(saved)-1)
	}
	s = rxFmtMDFence.ReplaceAllStringFunc(s, stash)
	s = rxFmtMDInlineCode.ReplaceAllStringFunc(s, stash)

	// Decode HTML entities first so &lt;b&gt; becomes <b> for the rewriter.
	s = stdhtml.UnescapeString(s)

	s = rewriteHTMLToMarkdown(s)

	for i, body := range saved {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00C%d\x00", i), body)
	}
	return s
}

// --- Stack-based HTML→markdown rewriter ---
//
// The rewriter is a tiny tokenizer + a single-pass stack walker. We tokenize
// the input into text spans, opening tags, closing tags, and self-closing
// tags, then walk the token stream maintaining a stack of "open style frames"
// (one per active b/i/code/pre/a). When we see a close tag we look it up in
// the stack:
//
//   - matches the top → pop it, emit the close marker
//   - matches deeper in the stack → close intermediate frames in reverse so
//     the markdown stays well-formed (this is what fixes cross-nested input
//     like "<b><code>X</b></code>")
//   - matches nothing in the stack → drop the close tag silently
//
// At EOF we close any still-open frames in reverse order. Unknown tags are
// dropped entirely while their content is preserved.

type htmlTokKind int

const (
	htmlTokText htmlTokKind = iota
	htmlTokOpen
	htmlTokClose
	htmlTokSelfClose
)

type htmlTok struct {
	kind  htmlTokKind
	name  string // tag name, lowercased; empty for text
	attrs string // raw attribute string for opens (excluding name)
	text  string // raw text run for htmlTokText
}

func tokenizeHTMLLite(s string) []htmlTok {
	tokens := make([]htmlTok, 0, 16)
	n := len(s)
	i := 0
	for i < n {
		if s[i] != '<' {
			j := i
			for j < n && s[j] != '<' {
				j++
			}
			tokens = append(tokens, htmlTok{kind: htmlTokText, text: s[i:j]})
			i = j
			continue
		}
		// We're at '<'. Decide if this looks like a tag (we accept "<a..." or
		// "</a..."); anything else (e.g. "<3" or "x < y") gets emitted as
		// literal text so we don't munch through arithmetic / comparisons.
		k := i + 1
		if k < n && s[k] == '/' {
			k++
		}
		if k >= n || !isASCIIAlpha(s[k]) {
			tokens = append(tokens, htmlTok{kind: htmlTokText, text: s[i : i+1]})
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			// Unterminated tag — treat the rest as text so we don't lose data.
			tokens = append(tokens, htmlTok{kind: htmlTokText, text: s[i:]})
			break
		}
		tok, ok := parseHTMLTag(s[i+1 : i+end])
		if !ok {
			// Looked like "<X..." but the inside doesn't parse as a real tag
			// (e.g. "x<y && y>z" — the y's attrs aren't a valid attribute
			// list). Emit the leading '<' as text and re-scan from i+1 so the
			// rest is treated as plain text.
			tokens = append(tokens, htmlTok{kind: htmlTokText, text: "<"})
			i++
			continue
		}
		tokens = append(tokens, tok)
		i += end + 1
	}
	return tokens
}

// parseHTMLTag inspects the inside of a "<...>" pair (without the angle
// brackets). It returns a token + ok=true when the contents look like a
// real HTML tag. Returns ok=false for things that merely happened to
// match the loose "<alpha..." prefix but aren't tags — e.g. "y && y" from
// arithmetic "x<y && y>z" — so the caller can keep the literal '<' as
// text instead of munching through the operands.
func parseHTMLTag(inner string) (htmlTok, bool) {
	// Closing tag: "/name" possibly with trailing whitespace.
	if strings.HasPrefix(inner, "/") {
		name := strings.TrimSpace(inner[1:])
		if sp := indexSpace(name); sp >= 0 {
			// Closing tags don't take attributes; the trailing junk is
			// ignored but we keep the name anyway.
			name = name[:sp]
		}
		if !isValidTagName(name) {
			return htmlTok{}, false
		}
		return htmlTok{kind: htmlTokClose, name: strings.ToLower(name)}, true
	}
	body := strings.TrimSpace(inner)
	selfClose := false
	if strings.HasSuffix(body, "/") {
		selfClose = true
		body = strings.TrimSpace(strings.TrimSuffix(body, "/"))
	}
	name := body
	attrs := ""
	if sp := indexSpace(body); sp >= 0 {
		name = body[:sp]
		attrs = strings.TrimSpace(body[sp:])
	}
	if !isValidTagName(name) {
		return htmlTok{}, false
	}
	if !looksLikeAttrList(attrs) {
		return htmlTok{}, false
	}
	name = strings.ToLower(name)
	kind := htmlTokOpen
	if selfClose {
		kind = htmlTokSelfClose
	}
	return htmlTok{kind: kind, name: name, attrs: attrs}, true
}

// isValidTagName mirrors the original regex's "alpha-start, then
// alphanumeric/hyphen" restriction.
func isValidTagName(name string) bool {
	if name == "" {
		return false
	}
	if !isASCIIAlpha(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !(isASCIIAlpha(c) || (c >= '0' && c <= '9') || c == '-' || c == ':') {
			return false
		}
	}
	return true
}

// looksLikeAttrList is a lenient check that rejects obviously-non-HTML
// gunk inside angle brackets (boolean operators, math, etc.). It allows
// empty / whitespace-only and any string starting with an attribute-name
// character. Real HTML attribute syntax is more elaborate; we only care
// about distinguishing "real-ish tag" from "x<y && y>z" arithmetic.
func looksLikeAttrList(attrs string) bool {
	a := strings.TrimSpace(attrs)
	if a == "" {
		return true
	}
	c := a[0]
	return isASCIIAlpha(c) || c == '_' || c == ':'
}

func indexSpace(s string) int {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			return i
		}
	}
	return -1
}

func isASCIIAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// frame is one entry on the rewriter's open-tags stack.
type frame struct {
	name  string // canonical tag (e.g. "b", "i", "code", "pre", "a")
	close string // markdown sequence to emit when this frame is popped
	// blockingCode is true while we're inside a <pre> or <code> region. While
	// blocking, nested style tags are emitted as plain text (no markdown
	// markers) because Telegram's MarkdownV2 doesn't allow style nesting
	// inside code anyway.
	blocking bool
}

// rewriteHTMLToMarkdown performs the actual tag-to-markdown rewrite.
func rewriteHTMLToMarkdown(s string) string {
	tokens := tokenizeHTMLLite(s)
	var sb strings.Builder
	sb.Grow(len(s))
	stack := make([]frame, 0, 8)

	// pop emits close markers for frames above and including idx, then trims
	// the stack to remove them. Used both for explicit close tags and EOF.
	pop := func(idx int) {
		for k := len(stack) - 1; k >= idx; k-- {
			sb.WriteString(stack[k].close)
		}
		stack = stack[:idx]
	}

	insideBlocking := func() bool {
		for _, f := range stack {
			if f.blocking {
				return true
			}
		}
		return false
	}

	for _, tok := range tokens {
		switch tok.kind {
		case htmlTokText:
			sb.WriteString(tok.text)

		case htmlTokOpen:
			// While inside a <pre>/<code> block, ignore further style tags
			// (their content still survives as text, but no markdown markers).
			if insideBlocking() && tok.name != "pre" && tok.name != "code" {
				// Drop the tag itself; content from following text tokens
				// is still appended verbatim.
				continue
			}
			switch tok.name {
			case "b", "strong":
				sb.WriteString("**")
				stack = append(stack, frame{name: "b", close: "**"})
			case "i", "em":
				sb.WriteString("*")
				stack = append(stack, frame{name: "i", close: "*"})
			case "s", "del", "strike":
				sb.WriteString("~~")
				stack = append(stack, frame{name: "s", close: "~~"})
			case "code":
				// <code> directly inside <pre> is decorative; the <pre> already
				// opened a fenced block. Push a no-op frame so the matching
				// </code> pops cleanly without emitting anything.
				if len(stack) > 0 && stack[len(stack)-1].name == "pre" {
					stack = append(stack, frame{name: "code", close: "", blocking: true})
				} else {
					sb.WriteString("`")
					stack = append(stack, frame{name: "code", close: "`", blocking: true})
				}
			case "pre":
				sb.WriteString("```\n")
				stack = append(stack, frame{name: "pre", close: "\n```", blocking: true})
			case "a":
				href := extractHref(tok.attrs)
				sb.WriteString("[")
				stack = append(stack, frame{name: "a", close: "](" + href + ")"})
			case "br":
				sb.WriteString("\n")
			case "p":
				// Open paragraph — ensure paragraph break before content.
				if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n\n") {
					sb.WriteString("\n\n")
				}
			case "blockquote":
				if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
					sb.WriteString("\n")
				}
				stack = append(stack, frame{name: "blockquote", close: ""})
			default:
				// Unknown tag → drop the tag, content via subsequent tokens.
			}

		case htmlTokClose:
			// Find matching frame from the top down. Cross-nested input like
			// <b><code>X</b></code> matches "b" deeper than the top "code"
			// frame; we close the intermediate code frame first to keep the
			// markdown well-formed.
			idx := -1
			canonical := canonicalCloseName(tok.name)
			for k := len(stack) - 1; k >= 0; k-- {
				if stack[k].name == canonical {
					idx = k
					break
				}
			}
			if idx == -1 {
				// Stray close tag with no matching open — drop it silently.
				continue
			}
			pop(idx)
			if tok.name == "p" {
				if !strings.HasSuffix(sb.String(), "\n\n") {
					sb.WriteString("\n\n")
				}
			}

		case htmlTokSelfClose:
			switch tok.name {
			case "br":
				sb.WriteString("\n")
			case "p":
				if !strings.HasSuffix(sb.String(), "\n\n") {
					sb.WriteString("\n\n")
				}
			}
		}
	}
	// Close any still-open frames in reverse so the output is well-formed.
	pop(0)
	return sb.String()
}

// canonicalCloseName maps closing tag aliases to the frame name we used at
// open time. Keeps "</strong>" matching a <b>-frame, etc.
func canonicalCloseName(name string) string {
	switch name {
	case "strong":
		return "b"
	case "em":
		return "i"
	case "del", "strike":
		return "s"
	default:
		return name
	}
}

// rxAttrHref extracts the href value from an anchor tag's raw attribute string.
// Handles double-quoted, single-quoted, and unquoted forms.
var rxAttrHref = regexp.MustCompile(`(?i)href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

func extractHref(attrs string) string {
	m := rxAttrHref.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	for i := 1; i < len(m); i++ {
		if m[i] != "" {
			return m[i]
		}
	}
	return ""
}

// flattenTablesToBullets rewrites markdown tables into bullet lines. A table
// is recognized by a header row (containing pipes) followed by a separator
// row (---|---|... possibly with colons for alignment) followed by zero or
// more data rows. Each data row becomes
// "- **Header1**: cell1 / **Header2**: cell2". Tables inside fenced code
// blocks are left alone.
func flattenTablesToBullets(s string) string {
	if !strings.Contains(s, "|") {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out = append(out, line)
			i++
			continue
		}
		if !inFence && isMDTableRow(line) &&
			i+1 < len(lines) && isMDTableSep(lines[i+1]) {
			headers := splitMDTableRow(line)
			j := i + 2
			for j < len(lines) && isMDTableRow(lines[j]) {
				cells := splitMDTableRow(lines[j])
				if row := mdTableRowToBullet(headers, cells); row != "" {
					out = append(out, row)
				}
				j++
			}
			i = j
			continue
		}
		out = append(out, line)
		i++
	}
	return strings.Join(out, "\n")
}

func isMDTableRow(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "|") {
		return false
	}
	return strings.Count(t, "|") >= 2 || strings.HasPrefix(t, "|") || strings.HasSuffix(t, "|")
}

func isMDTableSep(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.Contains(t, "-") || !strings.Contains(t, "|") {
		return false
	}
	for _, c := range t {
		if c != '|' && c != '-' && c != ':' && c != ' ' {
			return false
		}
	}
	return true
}

func splitMDTableRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}

func mdTableRowToBullet(headers, cells []string) string {
	var parts []string
	for i, cell := range cells {
		if cell == "" || cell == "-" {
			continue
		}
		var hdr string
		if i < len(headers) && headers[i] != "" {
			hdr = headers[i]
		} else {
			hdr = fmt.Sprintf("열%d", i+1)
		}
		parts = append(parts, fmt.Sprintf("**%s**: %s", hdr, cell))
	}
	if len(parts) == 0 {
		return ""
	}
	return "- " + strings.Join(parts, " / ")
}
