// web_focus.go — Query-targeted excerpting for fetched pages.
//
// Fetching a reference page to answer one question spends the whole page's
// tokens on it: a 50KB article costs ~15k tokens whether the model needed all of
// it or one section. Truncation does not help — it keeps the FRONT of the page,
// which is exactly where the answer usually is not.
//
// So when the caller says what it is looking for, keep the sections that match
// and drop the rest. The ancestor headings of a kept section come along, because
// a heading is what tells the model where the text sits in the document;
// sections are emitted in document order, not score order, so the excerpt still
// reads as a document. When nothing scores, the caller falls back to plain
// truncation — a focus that matched nothing must never silently return an empty
// page.
package web

import (
	"sort"
	"strings"
	"unicode"
)

// focusResult is what focusExcerpt produced, for the metadata line.
type focusResult struct {
	Text          string
	KeptSections  int
	TotalSections int
	KeptChars     int
	TotalChars    int
}

// minFocusScore is the overlap a section needs to be worth keeping. One shared
// token is noise ("the", "이"); two is a signal.
const minFocusScore = 2

// focusExcerpt keeps the parts of markdown content that match focus, within
// budget characters. ok is false when the focus matched nothing, when there is
// no focus, or when the content already fits — all cases where the caller should
// use the content as-is.
func focusExcerpt(content, focus string, budget int) (focusResult, bool) {
	focus = strings.TrimSpace(focus)
	if focus == "" || budget <= 0 || len(content) <= budget {
		return focusResult{}, false
	}
	wanted := focusTokenSet(focus)
	if len(wanted) == 0 {
		return focusResult{}, false
	}

	sections := splitMarkdownSections(content)
	if len(sections) == 0 {
		return focusResult{}, false
	}

	type scored struct {
		idx   int
		score int
	}
	ranked := make([]scored, 0, len(sections))
	for i, sec := range sections {
		if s := scoreSection(sec, wanted); s >= minFocusScore {
			ranked = append(ranked, scored{idx: i, score: s})
		}
	}
	if len(ranked) == 0 {
		return focusResult{}, false
	}
	// Best first; for equal scores the earlier section — a document's own order
	// is a reasonable tiebreak and it keeps the output stable.
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].score != ranked[b].score {
			return ranked[a].score > ranked[b].score
		}
		return ranked[a].idx < ranked[b].idx
	})

	keep := map[int]bool{}
	used := 0
	for _, r := range ranked {
		cost := sections[r.idx].size()
		if used > 0 && used+cost > budget {
			continue // a later, smaller section may still fit
		}
		keep[r.idx] = true
		used += cost
		if used >= budget {
			break
		}
	}
	if len(keep) == 0 {
		return focusResult{}, false
	}

	var b strings.Builder
	kept := 0
	elided := false
	for i, sec := range sections {
		if !keep[i] {
			elided = true
			continue
		}
		if elided && b.Len() > 0 {
			b.WriteString("\n[...]\n\n")
		}
		elided = false
		// Breadcrumb: a section body without its ancestor headings can read as a
		// statement about the wrong subject.
		if trail := parentTrail(sections, i); trail != "" {
			b.WriteString(trail)
			b.WriteString("\n")
		}
		rendered := sec.render()
		b.WriteString(rendered)
		if !strings.HasSuffix(rendered, "\n") {
			b.WriteString("\n")
		}
		kept++
	}
	if elided {
		b.WriteString("\n[...]")
	}

	return focusResult{
		Text:          strings.TrimSpace(b.String()),
		KeptSections:  kept,
		TotalSections: len(sections),
		KeptChars:     used,
		TotalChars:    len(content),
	}, true
}

// markdownSection is one heading and the body under it. A document's preamble
// (text before any heading) is a section with level 0 and no title.
type markdownSection struct {
	level int
	title string
	body  string
}

func (s markdownSection) size() int { return len(s.title) + len(s.body) + s.level + 2 }

func (s markdownSection) render() string {
	if s.level == 0 {
		return s.body
	}
	return strings.Repeat("#", s.level) + " " + s.title + "\n" + s.body
}

// parentTrail is the ancestor headings of section i, as one breadcrumb line.
// Empty when the section is top-level.
func parentTrail(all []markdownSection, i int) string {
	self := all[i]
	if self.level <= 1 {
		return ""
	}
	var parts []string
	want := self.level - 1
	for j := i - 1; j >= 0 && want >= 1; j-- {
		if all[j].level == want {
			parts = append([]string{all[j].title}, parts...)
			want--
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "> " + strings.Join(parts, " › ")
}

// splitMarkdownSections cuts markdown at ATX headings, leaving fenced code
// blocks intact — a "#" inside a fence is a comment, not a heading.
func splitMarkdownSections(content string) []markdownSection {
	lines := strings.Split(content, "\n")
	sections := make([]markdownSection, 0, 8)
	cur := markdownSection{}
	var body strings.Builder
	inFence := false

	flush := func() {
		cur.body = strings.TrimRight(body.String(), "\n")
		if cur.level > 0 || strings.TrimSpace(cur.body) != "" {
			sections = append(sections, cur)
		}
		body.Reset()
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
		}
		if !inFence {
			if level, title, ok := parseATXHeading(line); ok {
				flush()
				cur = markdownSection{level: level, title: title}
				continue
			}
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	flush()
	return sections
}

// parseATXHeading reads "## Title" into (2, "Title"). Markdown allows at most
// six levels and requires a space after the hashes.
func parseATXHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	title := strings.TrimSpace(strings.TrimRight(trimmed[level+1:], " #"))
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

// scoreSection weighs a heading match far above a body match: a section titled
// "Ownership and references" IS about ownership, while a body that mentions it
// once may only be pointing elsewhere.
func scoreSection(sec markdownSection, wanted map[string]bool) int {
	return 3*countMatches(sec.title, wanted) + countMatches(sec.body, wanted)
}

// countMatches counts how many DISTINCT wanted tokens appear in text. Distinct,
// not total: a page repeating one word a hundred times is not a better answer
// than one covering several of the asked-about terms.
func countMatches(text string, wanted map[string]bool) int {
	if text == "" {
		return 0
	}
	have := focusTokenSet(text)
	n := 0
	for token := range wanted {
		if have[token] {
			n++
		}
	}
	return n
}

// focusTokenSet tokenizes for matching. Two shapes are produced because the
// corpus is bilingual: whitespace/punctuation-delimited words cover English and
// spaced Korean, and Hangul character bigrams cover the compounds Korean writes
// without spaces ("메모리안전성" contains "메모리"). Single characters are
// dropped as noise.
func focusTokenSet(text string) map[string]bool {
	out := map[string]bool{}
	word := make([]rune, 0, 24)
	emit := func() {
		if len(word) >= 2 {
			out[strings.ToLower(string(word))] = true
			if isHangulWord(word) {
				for i := 0; i+1 < len(word); i++ {
					out[string(word[i:i+2])] = true
				}
			}
		}
		word = word[:0]
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
			continue
		}
		emit()
	}
	emit()
	return out
}

func isHangulWord(word []rune) bool {
	for _, r := range word {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}
