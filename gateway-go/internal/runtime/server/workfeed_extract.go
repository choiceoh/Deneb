package server

import (
	"regexp"
	"strings"
	"unicode"
)

// Work-feed card title/summary extraction from proactive bodies.
//
// Proactive output (mail analysis, calendar briefings, wiki consolidation,
// morning letters) is well-structured markdown that almost always opens with a
// heading. Pulling the first meaningful heading as the title and the body that
// follows as the summary reads far better than the old "업무 리포트" + first-line
// slice, which leaked markdown markers ("### 📧 …", "---") into the card.
//
// These are pure functions (no LLM): proactive delivery is a background path and
// adding an LLM call here would add a failure mode and latency for little gain
// while the bodies are already structured. If inputs that start with a table or
// code block become common and the heuristic looks thin, this is the spot to
// graft a claude-haiku 1-shot summary onto.

var (
	wfHeaderRe  = regexp.MustCompile(`^#{1,6}\s+`)  // "## ", "### "
	wfBulletRe  = regexp.MustCompile(`^[-*+]\s+`)   // "- ", "* ", "+ "
	wfOrderedRe = regexp.MustCompile(`^\d+[.)]\s+`) // "1. ", "2) "
	wfQuoteRe   = regexp.MustCompile(`^>\s?`)       // "> "
	wfBoldRe    = regexp.MustCompile(`\*\*(.+?)\*\*`)
	wfCodeRe    = regexp.MustCompile("`([^`]+)`")
	wfSpaceRe   = regexp.MustCompile(`[ \t]+`)
)

// extractCardTitle returns the first meaningful line of content as a card title,
// with markdown markers (#, **) stripped and emoji preserved, clipped to
// workFeedTitleMaxRunes. Horizontal rules and blank lines are skipped, so a body
// that opens with "---" yields the heading after it. Returns ("", "") when
// nothing usable is found (the store then falls back to defaultTitle → "업무 리포트").
//
// sourceLine is the *raw* line the title came from, handed to extractCardSummary
// so the summary starts strictly after the title line.
func extractCardTitle(content string) (title, sourceLine string) {
	s := strings.TrimSpace(content)
	if s == "" {
		return "", ""
	}
	lines := strings.Split(s, "\n")
	idx, raw := firstMeaningfulLine(lines, 0)
	if idx < 0 {
		return "", ""
	}
	t := stripMarkdownLine(raw)
	if t == "" {
		return "", "" // the line was only markers
	}
	// A short, generic heading ("분석", "보고") carries little on its own. When
	// the next meaningful line is a sub-heading ("### 왜 지금 왔는가"), fold it in
	// as "분석 — 왜 지금 왔는가" and let the summary start after it, so the card
	// title is specific instead of a bare section word.
	if len([]rune(t)) < genericTitleMaxRunes {
		if idx2, raw2 := firstMeaningfulLine(lines, idx+1); idx2 >= 0 && wfHeaderRe.MatchString(strings.TrimSpace(raw2)) {
			if sub := stripMarkdownLine(raw2); sub != "" {
				return clipRunes(t+" — "+sub, workFeedTitleMaxRunes), raw2
			}
		}
	}
	return clipRunes(t, workFeedTitleMaxRunes), raw
}

// extractCardSummary joins the meaningful lines after sourceLine into a single
// cleaned summary clipped to workFeedSummaryMaxRunes. Horizontal rules, table
// separators, code fences, and blank lines are skipped; headings, bullets,
// ordered numbers, bold, and inline code are unwrapped (a sub-heading like
// "### 왜 지금 왔는가" enriches the summary). When no body follows the title, it
// falls back to the cleaned first meaningful line (the title itself).
func extractCardSummary(content, sourceLine string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	start := 0
	if target := strings.TrimSpace(sourceLine); target != "" {
		for i, l := range lines {
			if strings.TrimSpace(l) == target {
				start = i + 1
				break
			}
		}
	}
	var parts []string
	runeCount := 0
	cursor := start
	for cursor < len(lines) {
		idx, raw := firstMeaningfulLine(lines, cursor)
		if idx < 0 {
			break
		}
		if seg := stripMarkdownLine(raw); seg != "" {
			parts = append(parts, seg)
			runeCount += len([]rune(seg)) + 1
		}
		cursor = idx + 1
		if runeCount >= workFeedSummaryMaxRunes {
			break
		}
	}
	if len(parts) == 0 {
		// No body after the title — fall back to the cleaned first line.
		if idx, raw := firstMeaningfulLine(lines, 0); idx >= 0 {
			return clipRunes(stripMarkdownLine(raw), workFeedSummaryMaxRunes)
		}
		return ""
	}
	return clipRunes(strings.Join(parts, " "), workFeedSummaryMaxRunes)
}

// firstMeaningfulLine returns the index and raw text of the first meaningful
// line at or after start: non-blank, not a horizontal rule, not a table
// separator, not inside a code fence. Headings count as meaningful — their text
// is useful in both title and summary; stripMarkdownLine removes the "#".
func firstMeaningfulLine(lines []string, start int) (lineIdx int, line string) {
	inFence := false
	for i := start; i < len(lines); i++ {
		raw := lines[i]
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inFence || t == "" {
			continue
		}
		if isHorizontalRule(t) || isTableSeparator(t) {
			continue
		}
		return i, raw
	}
	return -1, ""
}

// stripMarkdownLine removes leading block markers (heading, bullet, ordered
// number, blockquote) and inline emphasis (**bold**, `code`) from a single
// line, collapses whitespace, and trims. Emoji are preserved — they help a card
// scan ("📧 메일 분석 보고").
func stripMarkdownLine(line string) string {
	s := strings.TrimSpace(line)
	// Peel leading block markers repeatedly to handle nesting like "> - **x**".
	for {
		before := s
		s = wfHeaderRe.ReplaceAllString(s, "")
		s = wfQuoteRe.ReplaceAllString(s, "")
		s = wfBulletRe.ReplaceAllString(s, "")
		s = wfOrderedRe.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
		if s == before {
			break
		}
	}
	s = wfBoldRe.ReplaceAllString(s, "$1")
	s = wfCodeRe.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "**", "") // drop unmatched bold leftovers
	s = strings.ReplaceAll(s, "|", " ") // collapse table-cell pipes to spaces
	s = wfSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// isHorizontalRule reports whether a trimmed line is a markdown horizontal rule
// (---, ***, ___, ===) — three or more of a single ruling character.
func isHorizontalRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' && c != '=' {
		return false
	}
	for i := range len(s) {
		if s[i] != c {
			return false
		}
	}
	return true
}

// isTableSeparator reports whether a trimmed line is a markdown table separator
// row like "|---|:--:|" — pipes, dashes, colons, whitespace only.
func isTableSeparator(s string) bool {
	if !strings.Contains(s, "|") || !strings.Contains(s, "-") {
		return false
	}
	for _, r := range s {
		switch r {
		case '|', '-', ':', ' ', '\t':
		default:
			return false
		}
	}
	return true
}

// substantiveText strips markdown markers, emoji, and all whitespace from a
// body, leaving only its "meat" (Hangul/Han/alphanumeric content). The
// contentless filter judges a multi-line proactive body by how much real
// content it carries, not by its line count.
func substantiveText(s string) string {
	var b strings.Builder
	inFence := false
	for _, raw := range strings.Split(s, "\n") {
		t := strings.TrimSpace(raw)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
			continue
		}
		if inFence || t == "" || isHorizontalRule(t) || isTableSeparator(t) {
			continue
		}
		for _, r := range stripMarkdownLine(t) {
			if unicode.IsSpace(r) || isEmojiRune(r) {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isEmojiRune reports whether r is an emoji/symbol or emoji modifier that should
// not count as substantive content. Conservative: Unicode symbol categories
// (So/Sk) plus ZWJ and variation-selector-16, leaving Hangul/Han intact.
func isEmojiRune(r rune) bool {
	if r == 0x200D || r == 0xFE0F {
		return true
	}
	return unicode.In(r, unicode.So, unicode.Sk)
}

// clipRunes truncates s to maxRunes runes, appending "..." on overflow. A
// maxRunes <= 0 returns s unchanged.
func clipRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
}
