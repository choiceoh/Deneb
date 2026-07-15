// plaud_project_entities.go — when a project is mentioned in the recording
// title/transcript, inject that project's people / places / orgs from the wiki
// 대표페이지 (client, sites, tags, cues, related 인물·거래처).
package meeting

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const (
	plaudMaxMentionedProjects = 3
	plaudEntityBlockMaxRunes  = 2_500
	plaudMatchTranscriptRunes = 8_000
)

// ProjectEntityFacts is the name-bearing surface of one project 대표페이지.
type ProjectEntityFacts struct {
	Path   string
	Title  string
	Client string
	Sites  []string
	Tags   []string
	Cues   []string
	People []string // related 인물 page titles
	Orgs   []string // related 거래처/업무 entity titles
	Extra  []string // other high-signal names (e.g. from summary)
}

// RankMentionedProjects returns candidates whose title/path tokens appear in
// the recording name or transcript, highest score first, capped.
func RankMentionedProjects(recordingName, transcript string, cands []mailanalysis.ProjectCandidate, max int) []mailanalysis.ProjectCandidate {
	if max <= 0 {
		max = plaudMaxMentionedProjects
	}
	hay := strings.ToLower(recordingName + "\n" + textutil.TruncateRunes(transcript, plaudMatchTranscriptRunes, ""))
	if strings.TrimSpace(hay) == "" || len(cands) == 0 {
		return nil
	}
	type scored struct {
		c mailanalysis.ProjectCandidate
		n int
	}
	var ranked []scored
	for _, c := range cands {
		n := projectMentionScore(hay, c)
		if n <= 0 {
			continue
		}
		ranked = append(ranked, scored{c: c, n: n})
	}
	if len(ranked) == 0 {
		return nil
	}
	// Stable insertion sort by score desc (small N).
	for i := 1; i < len(ranked); i++ {
		j := i
		for j > 0 && ranked[j].n > ranked[j-1].n {
			ranked[j], ranked[j-1] = ranked[j-1], ranked[j]
			j--
		}
	}
	if len(ranked) > max {
		ranked = ranked[:max]
	}
	out := make([]mailanalysis.ProjectCandidate, len(ranked))
	for i, s := range ranked {
		out[i] = s.c
	}
	return out
}

func projectMentionScore(hayLower string, c mailanalysis.ProjectCandidate) int {
	score := 0
	hayWords := strings.FieldsFunc(hayLower, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == '/' || r == '-' || r == '_' || r == ',' || r == '.'
	})
	for _, tok := range projectMatchTokens(c) {
		tl := strings.ToLower(tok)
		if strings.Contains(hayLower, tl) {
			score += utf8.RuneCountInString(tok) // longer tokens weigh more
			continue
		}
		// Partial prefix: "비금" → "비금도". Require the hay word to cover at
		// least half the token so "솔라" does not match "솔라빌리지".
		tokN := utf8.RuneCountInString(tl)
		for _, w := range hayWords {
			w = strings.Trim(w, ".,·")
			n := utf8.RuneCountInString(w)
			if n < 2 || !strings.HasPrefix(tl, w) {
				continue
			}
			if n*2 >= tokN {
				score += n
			}
		}
	}
	return score
}

func projectMatchTokens(c mailanalysis.ProjectCandidate) []string {
	var raw []string
	raw = append(raw, c.Title)
	raw = append(raw, filepath.Base(filepath.Dir(c.Path)))
	raw = append(raw, strings.TrimSuffix(filepath.Base(c.Path), filepath.Ext(c.Path)))
	seen := map[string]bool{}
	var out []string
	for _, r := range raw {
		for _, tok := range splitHintTokens(r) {
			key := strings.ToLower(tok)
			if seen[key] || utf8.RuneCountInString(tok) < 2 {
				continue
			}
			// Drop ultra-generic folder crumbs.
			if tok == "대표" || tok == "프로젝트" || strings.EqualFold(tok, "md") {
				continue
			}
			seen[key] = true
			out = append(out, tok)
		}
	}
	return out
}

// FormatProjectEntityBlock renders facts for the synthesis system prompt.
func FormatProjectEntityBlock(facts []ProjectEntityFacts) string {
	if len(facts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("녹음/전사에 등장한 프로젝트 위키에서 뽑은 고유명이다. 전사 오인식 교정 시 이 표기를 우선한다.\n")
	for _, f := range facts {
		title := strings.TrimSpace(f.Title)
		if title == "" {
			title = f.Path
		}
		fmt.Fprintf(&b, "\n### %s\n", title)
		if c := strings.TrimSpace(f.Client); c != "" {
			fmt.Fprintf(&b, "- 거래처/발주: %s\n", c)
		}
		if line := joinNonEmpty(f.Sites); line != "" {
			fmt.Fprintf(&b, "- 현장/지명: %s\n", line)
		}
		if line := joinNonEmpty(f.People); line != "" {
			fmt.Fprintf(&b, "- 인명: %s\n", line)
		}
		if line := joinNonEmpty(append(append([]string{}, f.Orgs...), f.Tags...)); line != "" {
			fmt.Fprintf(&b, "- 단체·태그: %s\n", line)
		}
		if line := joinNonEmpty(append(append([]string{}, f.Cues...), f.Extra...)); line != "" {
			fmt.Fprintf(&b, "- 기타 단서: %s\n", line)
		}
	}
	return trimRunes(strings.TrimSpace(b.String()), plaudEntityBlockMaxRunes)
}

// EntityHintTokens flattens facts into glossary-slice hint tokens.
func EntityHintTokens(facts []ProjectEntityFacts) []string {
	terms := textutil.NewLimitedTerms(80, 1500)
	add := func(ss ...string) {
		for _, s := range ss {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			_ = terms.Add(s)
			for _, tok := range splitHintTokens(s) {
				_ = terms.Add(tok)
			}
		}
	}
	for _, f := range facts {
		add(f.Title, f.Client)
		add(f.Sites...)
		add(f.People...)
		add(f.Orgs...)
		add(f.Tags...)
		add(f.Cues...)
		add(f.Extra...)
	}
	if s := terms.String(); s != "" {
		return strings.Split(s, ", ")
	}
	return nil
}

func joinNonEmpty(ss []string) string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		if skipEntityTag(s) {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= 24 {
			break
		}
	}
	return strings.Join(out, " · ")
}

func skipEntityTag(s string) bool {
	switch strings.ToLower(s) {
	case "mail-analysis", "topsolar.kr", "회의록", "plaud.ai", "사용자지식",
		"tags", "epc", "태양광", "모듈", "케이블", "인버터":
		return true
	default:
		return false
	}
}

// RelatedEntityKind classifies a wiki related path for entity injection.
func RelatedEntityKind(rel string) string {
	rel = strings.TrimSpace(rel)
	rel = strings.TrimSuffix(rel, ".md")
	switch {
	case strings.HasPrefix(rel, "인물/"):
		return "person"
	case strings.HasPrefix(rel, "프로젝트/거래/"), strings.HasPrefix(rel, "업무/"):
		return "org"
	default:
		return ""
	}
}

// TitleFromRelatedPath is a fallback when the related page cannot be read.
func TitleFromRelatedPath(rel string) string {
	rel = strings.TrimSpace(rel)
	base := filepath.Base(strings.TrimSuffix(rel, ".md"))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	return strings.TrimSpace(base)
}
