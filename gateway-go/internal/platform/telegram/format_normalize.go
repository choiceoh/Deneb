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

// HTML→markdown rewrite patterns. Compiled once at package init.
var (
	rxFmtHTMLPreCode  = regexp.MustCompile(`(?is)<\s*pre[^>]*>\s*<\s*code[^>]*>(.*?)<\s*/\s*code\s*>\s*<\s*/\s*pre\s*>`)
	rxFmtHTMLPre      = regexp.MustCompile(`(?is)<\s*pre[^>]*>(.*?)<\s*/\s*pre\s*>`)
	rxFmtHTMLCode     = regexp.MustCompile(`(?is)<\s*code[^>]*>(.*?)<\s*/\s*code\s*>`)
	rxFmtHTMLBold     = regexp.MustCompile(`(?is)<\s*(?:b|strong)[^>]*>(.*?)<\s*/\s*(?:b|strong)\s*>`)
	rxFmtHTMLItalic   = regexp.MustCompile(`(?is)<\s*(?:i|em)[^>]*>(.*?)<\s*/\s*(?:i|em)\s*>`)
	rxFmtHTMLStrike   = regexp.MustCompile(`(?is)<\s*(?:s|del|strike)[^>]*>(.*?)<\s*/\s*(?:s|del|strike)\s*>`)
	rxFmtHTMLLinkDQ   = regexp.MustCompile(`(?is)<\s*a\s+[^>]*?href\s*=\s*"([^"]*)"[^>]*>(.*?)<\s*/\s*a\s*>`)
	rxFmtHTMLLinkSQ   = regexp.MustCompile(`(?is)<\s*a\s+[^>]*?href\s*=\s*'([^']*)'[^>]*>(.*?)<\s*/\s*a\s*>`)
	rxFmtHTMLBr       = regexp.MustCompile(`(?i)<\s*br\s*/?\s*>`)
	rxFmtHTMLPara     = regexp.MustCompile(`(?i)<\s*/?\s*p[^>]*>`)
	// Leftover stripper. Restricted to "alphabetic-start" tags so plain text
	// like "x < y & a > b" isn't misidentified as a tag and erased.
	rxFmtHTMLLeftover = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9-]*(?:\s[^>]*)?>`)
	// Markdown code regions to protect from HTML rewriting.
	rxFmtMDFence      = regexp.MustCompile("(?s)```[^\\n]*\\n.*?\\n```")
	rxFmtMDInlineCode = regexp.MustCompile("`[^`\\n]+`")
)

// htmlToTelegramMarkdown rewrites HTML tags and entities into markdown.
// Existing markdown code regions are stashed behind NUL-bracketed placeholders
// before rewriting and restored afterward, so code examples that show literal
// HTML are preserved.
//
// Order matters: entity decoding (&lt;b&gt; → <b>) must run BEFORE the tag
// regexes, otherwise an entity-escaped tag like &lt;b&gt;X&lt;/b&gt; passes
// through every tag rule (no '<' to match), gets decoded to raw <b> at the
// very end, and lands in the markdown parser which then re-escapes it →
// Telegram displays literal "<b>".
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

	// Decode HTML entities first so &lt;b&gt; becomes <b> for the tag rules.
	s = stdhtml.UnescapeString(s)

	s = rxFmtHTMLPreCode.ReplaceAllString(s, "```\n$1\n```")
	s = rxFmtHTMLPre.ReplaceAllString(s, "```\n$1\n```")
	s = rxFmtHTMLCode.ReplaceAllString(s, "`$1`")
	s = rxFmtHTMLBold.ReplaceAllString(s, "**$1**")
	s = rxFmtHTMLItalic.ReplaceAllString(s, "*$1*")
	s = rxFmtHTMLStrike.ReplaceAllString(s, "~~$1~~")
	s = rxFmtHTMLLinkDQ.ReplaceAllString(s, "[$2]($1)")
	s = rxFmtHTMLLinkSQ.ReplaceAllString(s, "[$2]($1)")
	s = rxFmtHTMLBr.ReplaceAllString(s, "\n")
	s = rxFmtHTMLPara.ReplaceAllString(s, "\n\n")
	s = rxFmtHTMLLeftover.ReplaceAllString(s, "")

	for i, body := range saved {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00C%d\x00", i), body)
	}
	return s
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
