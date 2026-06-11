// message_body.go — body extraction and HTML→text flattening for Gmail
// messages: multipart body walk, the HTML cleanup regex pipeline, and the
// web-safe base64 decoders. Split from operations.go (pure move).
package gmail

import (
	"encoding/base64"
	"html"
	"regexp"
	"strings"
)

// extractBody extracts the text body from a message payload,
// preferring text/plain over text/html. HTML bodies are flattened to a
// plain-text approximation so the Mini App (which renders the body as
// text inside a <pre>) doesn't end up showing raw <table>/<div> markup
// on HTML-only newsletters.
func extractBody(p *apiPayload) string {
	return stripMailChrome(extractBodyRaw(p))
}

// extractBodyRaw returns the decoded body without chrome stripping. Kept
// separate so the chrome heuristics can be unit-tested in isolation from
// the multipart walk + base64 decode logic.
func extractBodyRaw(p *apiPayload) string {
	if p == nil {
		return ""
	}

	// Single-part message.
	if p.Body != nil && p.Body.Data != "" && len(p.Parts) == 0 {
		decoded := decodeBase64URL(p.Body.Data)
		if strings.EqualFold(p.MimeType, "text/html") {
			return htmlToText(decoded)
		}
		// text/plain (or other non-HTML): decode stray entities the sender left
		// literal in the plain part, so "주소 :&nbsp;경기" doesn't reach the reader raw.
		return decodeMailEntities(decoded)
	}

	// Multipart: search for text/plain first, then text/html.
	var plainText, htmlText string
	findBody(p, &plainText, &htmlText)

	if plainText != "" {
		return decodeMailEntities(plainText)
	}
	if htmlText != "" {
		return htmlToText(htmlText)
	}
	return ""
}

func findBody(p *apiPayload, plain, html *string) {
	if p.MimeType == "text/plain" && p.Body != nil && p.Body.Data != "" {
		*plain = decodeBase64URL(p.Body.Data)
	}
	if p.MimeType == "text/html" && p.Body != nil && p.Body.Data != "" && *html == "" {
		*html = decodeBase64URL(p.Body.Data)
	}
	for i := range p.Parts {
		findBody(&p.Parts[i], plain, html)
	}
}

// HTML cleanup regexes (compiled once).
//
//	htmlDropREs  — <script>/<style>/<head> blocks including their content.
//	               RE2 has no backreferences, so each tag gets its own pattern.
//	htmlImgRE    — <img ...> capture (used to keep alt text, drop pixels).
//	htmlAnchorRE — <a ...>inner</a> capture (used to keep href next to label).
//	htmlHrefRE   — extract href attribute value (any quote style).
//	htmlAltRE    — extract alt attribute value (any quote style).
//	htmlParaRE   — paragraph-level boundaries that become a blank line so
//	               paragraphs stay visually separated.
//	htmlLineRE   — line-level boundaries that become a single newline.
//	htmlAnyTagRE — any remaining tag (stripped without leaving artifacts).
//	htmlBlankRE  — collapse runs of 3+ newlines into a single blank line.
var (
	htmlDropREs = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`),
		regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`),
		regexp.MustCompile(`(?is)<head\b[^>]*>.*?</head\s*>`),
	}
	htmlImgRE    = regexp.MustCompile(`(?is)<img\b([^>]*?)/?\s*>`)
	htmlAnchorRE = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a\s*>`)
	htmlHrefRE   = regexp.MustCompile(`(?i)href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	htmlAltRE    = regexp.MustCompile(`(?i)alt\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	htmlParaRE   = regexp.MustCompile(`(?i)</(?:p|div|h[1-6]|blockquote)\s*>`)
	htmlLineRE   = regexp.MustCompile(`(?i)<(?:br\s*/?|hr\s*/?|/li|/tr)\s*[^>]*>`)
	htmlAnyTagRE = regexp.MustCompile(`(?s)<[^>]+>`)
	htmlBlankRE  = regexp.MustCompile(`\n{3,}`)

	// Structure markers that survive into the plain-text view:
	//   htmlCellRE     — </td> / </th> become a two-space gap so table cells
	//                    don't fuse ("품명단가수량") in invoice/order mails.
	//   htmlListItemRE — <li> openers become a bullet so lists keep structure.
	//   htmlQuoteRE    — <blockquote> openers mark the start of quoted text
	//                    with "> " (first line only — a plain-text view can't
	//                    carry the nesting, but the cue is enough to read it).
	htmlCellRE     = regexp.MustCompile(`(?i)</t[dh]\s*>`)
	htmlListItemRE = regexp.MustCompile(`(?i)<li\b[^>]*>`)
	htmlQuoteRE    = regexp.MustCompile(`(?i)<blockquote\b[^>]*>`)
)

// extractAttr returns the first non-empty subgroup matched by attrRE in
// attrs (used for href and alt which both have three quote-style branches).
// Returns "" if no match.
func extractAttr(attrRE *regexp.Regexp, attrs string) string {
	m := attrRE.FindStringSubmatch(attrs)
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

// replaceImages rewrites <img> tags. Alt text becomes a "[이미지: alt]"
// marker; tags without alt are dropped — those are almost always
// 1×1 tracking pixels or decorative spacers, and rendering them as
// "[이미지]" would just litter every newsletter with placeholders.
func replaceImages(s string) string {
	return htmlImgRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := htmlImgRE.FindStringSubmatch(m)
		if len(sub) < 2 {
			return ""
		}
		alt := strings.TrimSpace(extractAttr(htmlAltRE, sub[1]))
		if alt == "" {
			return ""
		}
		return "[이미지: " + alt + "]"
	})
}

// replaceAnchors rewrites <a href="...">text</a> into "text (href)" so
// the URL stays visible after tag stripping — critical for auth/CTA
// emails where the link IS the content. Schemes that aren't actionable
// from a <pre> view (javascript:, in-page #frag) drop the URL but keep
// the visible text. When text and href are the same, only one copy is
// emitted to avoid "https://x (https://x)" noise.
func replaceAnchors(s string) string {
	return htmlAnchorRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := htmlAnchorRE.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		attrs, inner := sub[1], sub[2]
		text := strings.TrimSpace(htmlAnyTagRE.ReplaceAllString(inner, ""))
		href := strings.TrimSpace(extractAttr(htmlHrefRE, attrs))
		lower := strings.ToLower(href)
		if href == "" || strings.HasPrefix(lower, "javascript:") || strings.HasPrefix(href, "#") {
			return text
		}
		if text == "" {
			return href
		}
		if strings.EqualFold(text, href) {
			return text
		}
		return text + " (" + href + ")"
	})
}

// htmlToText turns an HTML email body into a readable plain-text
// approximation for the Mini App's <pre>-based body view. It is regex-
// based on purpose: Gmail HTML is usually well-formed enough, and a
// perfect HTML→text render isn't the goal — we just need to keep
// raw <table>/<div style="..."> markup from leaking into the UI.
func htmlToText(s string) string {
	if s == "" {
		return s
	}
	for _, re := range htmlDropREs {
		s = re.ReplaceAllString(s, "")
	}
	// Images first, then anchors — so an <img> wrapped in an <a> is turned
	// into its alt text *before* the anchor pass treats it as the link
	// label. Without this order the alt would be stripped by the generic
	// tag pass and the anchor would lose its visible text.
	s = replaceImages(s)
	s = replaceAnchors(s)
	s = htmlCellRE.ReplaceAllString(s, "  ")
	s = htmlListItemRE.ReplaceAllString(s, "\u2022 ")
	s = htmlQuoteRE.ReplaceAllString(s, "\n> ")
	s = htmlParaRE.ReplaceAllString(s, "\n\n")
	s = htmlLineRE.ReplaceAllString(s, "\n")
	s = htmlAnyTagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// &nbsp; decodes to U+00A0 which renders as a space but trips up any
	// downstream splitter that expects ASCII whitespace. Normalize.
	s = strings.ReplaceAll(s, "\u00A0", " ")

	// Trim trailing whitespace per line, then collapse runs of blank
	// lines so newsletter templates don't render as a tall column of
	// empty lines.
	var b strings.Builder
	b.Grow(len(s))
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRight(line, " \t\r"))
	}
	out := htmlBlankRE.ReplaceAllString(b.String(), "\n\n")
	return strings.TrimSpace(out)
}

func decodeBase64URL(s string) string {
	if data, ok := decodeBase64URLBytes(s); ok {
		return string(data)
	}
	return s
}

// decodeBase64URLBytes decodes Gmail web-safe base64 into raw bytes. Gmail may
// send it with or without "=" padding, using the url-safe (-_) or standard
// (+/) alphabet, and MIME parts can wrap it across lines — the old strict
// decoder (URLEncoding + NoPadding) rejected padded or wrapped input and
// silently returned the raw base64. Normalize whitespace and padding, then try
// each alphabet. Returns ok=false only when the input is genuinely not base64.
func decodeBase64URLBytes(s string) ([]byte, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		}
		return r
	}, s)
	s = strings.TrimRight(s, "=")
	if data, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return data, true
	}
	if data, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return data, true
	}
	return nil, false
}
