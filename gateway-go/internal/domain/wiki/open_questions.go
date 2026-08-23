// open_questions.go — the standing "미해결 질문" slot on project 대표페이지.
//
// The active-gap loop: answers and research keep discovering questions the wiki
// cannot answer yet ("JA Solar 단가가 트리나 대비 어느 정도인가"). Those used to
// evaporate with the conversation. Now the wiki-research task records them in a
// fixed `## 미해결 질문` section on the project's rep page (`- YYYY-MM-DD 질문`
// bullets), later cycles and new mail try to close them, and the morning letter
// escalates the ones that stayed open too long — the wiki chases its own gaps
// instead of waiting to be told.
//
// This file owns the deterministic side: parsing the section and collecting
// stale questions across rep pages. Writing/retiring bullets is done by the
// research turn itself (it rewrites the page body anyway).
package wiki

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// openQuestionsHeading is the H2 section holding a project's open questions.
	openQuestionsHeading = "미해결 질문"
	// vendorSilenceHeading is where dated questions move after sitting unanswered.
	vendorSilenceHeading = "벤더 침묵"
	// OpenQuestionExpireAfterDays is the scout/review expiry: dated bullets
	// this old leave 미해결 질문 and stop being scouted. Undated bullets stay.
	OpenQuestionExpireAfterDays = 14
	// maxOpenQuestionItems bounds how many bullets are read per page — the
	// research prompt keeps the section at ≤5, this is the parser's hard cap.
	maxOpenQuestionItems = 8
)

// OpenQuestionItem is one parsed bullet of a page's 미해결 질문 section.
type OpenQuestionItem struct {
	Question string
	Asked    string // YYYY-MM-DD from the bullet's leading date, "" when undated
}

// OpenQuestion is a stale open question found on a project rep page.
type OpenQuestion struct {
	Project  string `json:"project"`
	Question string `json:"question"`
	Asked    string `json:"asked,omitempty"` // YYYY-MM-DD, "" when undated
	AgeDays  int    `json:"age_days"`        // -1 when undated
	Path     string `json:"path,omitempty"`  // rep page path
}

// OpenQuestionsIn parses the `## 미해결 질문` bullets from a page body. Bullets
// may carry a leading ISO date ("- 2026-07-05 단가 확인"); undated bullets are
// kept with Asked="".
func OpenQuestionsIn(body string) []OpenQuestionItem {
	_, sections := (&Page{Body: body}).SplitByH2()
	for _, sec := range sections {
		if !strings.EqualFold(strings.TrimSpace(sec.Heading), openQuestionsHeading) {
			continue
		}
		out := parseOpenQuestionBullets(sec.Content)
		if len(out) > maxOpenQuestionItems {
			return out[:maxOpenQuestionItems]
		}
		return out
	}
	return nil
}

func parseOpenQuestionBullets(content string) []OpenQuestionItem {
	var out []OpenQuestionItem
	for _, ln := range strings.Split(content, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "- "))
		if t == "" {
			continue
		}
		item := OpenQuestionItem{Question: t}
		if len(t) >= 10 {
			if _, err := time.Parse("2006-01-02", t[:10]); err == nil {
				item.Asked = t[:10]
				item.Question = strings.TrimSpace(t[10:])
				if item.Question == "" {
					continue
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func formatOpenQuestionBullet(item OpenQuestionItem) string {
	if item.Asked != "" {
		return "- " + item.Asked + " " + item.Question
	}
	return "- " + item.Question
}

func isResolvedQuestion(q string) bool {
	return strings.HasPrefix(strings.TrimSpace(q), "~~")
}

func joinH2(preamble string, sections []H2Section) string {
	var b strings.Builder
	if p := strings.TrimSpace(preamble); p != "" {
		b.WriteString(p)
	}
	for _, sec := range sections {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(sec.Heading)
		if c := strings.TrimSpace(sec.Content); c != "" {
			b.WriteString("\n\n")
			b.WriteString(c)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "\n"
}

// expireOpenQuestionsInBody moves dated 미해결 질문 bullets older than
// minAgeDays into ## 벤더 침묵 and drops struck-through (resolved) bullets.
// Undated bullets are never expired.
func expireOpenQuestionsInBody(body string, now time.Time, minAgeDays int) (string, int, int, bool) {
	pre, sections := (&Page{Body: body}).SplitByH2()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	openIdx, silenceIdx := -1, -1
	var keep, expire []string
	dropped := 0
	for i, sec := range sections {
		h := strings.TrimSpace(sec.Heading)
		if h == openQuestionsHeading {
			openIdx = i
			for _, item := range parseOpenQuestionBullets(sec.Content) {
				if isResolvedQuestion(item.Question) {
					dropped++
					continue
				}
				if item.Asked != "" {
					asked, err := time.ParseInLocation("2006-01-02", item.Asked, now.Location())
					if err == nil {
						age := int(today.Sub(asked).Hours() / 24)
						if age >= minAgeDays {
							expire = append(expire, formatOpenQuestionBullet(item))
							continue
						}
					}
				}
				keep = append(keep, formatOpenQuestionBullet(item))
			}
		}
		if h == vendorSilenceHeading {
			silenceIdx = i
		}
	}
	if openIdx < 0 || (dropped == 0 && len(expire) == 0) {
		return body, 0, 0, false
	}

	silenceContent := ""
	if silenceIdx >= 0 {
		silenceContent = strings.TrimSpace(sections[silenceIdx].Content)
	}
	if len(expire) > 0 {
		extra := strings.Join(expire, "\n")
		if silenceContent != "" {
			silenceContent = silenceContent + "\n" + extra
		} else {
			silenceContent = extra
		}
	}

	next := make([]H2Section, 0, len(sections)+1)
	silenceWritten := false
	for i, sec := range sections {
		switch i {
		case openIdx:
			if len(keep) > 0 {
				next = append(next, H2Section{
					Heading: openQuestionsHeading,
					Content: strings.Join(keep, "\n"),
				})
			}
			if silenceIdx < 0 && silenceContent != "" {
				next = append(next, H2Section{Heading: vendorSilenceHeading, Content: silenceContent})
				silenceWritten = true
			}
		case silenceIdx:
			if silenceContent != "" {
				next = append(next, H2Section{Heading: vendorSilenceHeading, Content: silenceContent})
				silenceWritten = true
			}
		default:
			next = append(next, sec)
		}
	}
	if !silenceWritten && silenceContent != "" {
		next = append(next, H2Section{Heading: vendorSilenceHeading, Content: silenceContent})
	}
	return joinH2(pre, next), len(expire), dropped, true
}

// ExpireStaleOpenQuestions rewrites project 대표페이지: dated 미해결 질문
// bullets older than minAgeDays move to ## 벤더 침묵; struck-through bullets
// are dropped. Undated bullets stay. Returns pages changed.
func (s *Store) ExpireStaleOpenQuestions(now time.Time, minAgeDays int) int {
	if s == nil {
		return 0
	}
	if minAgeDays <= 0 {
		minAgeDays = OpenQuestionExpireAfterDays
	}
	pages, err := s.ListPages(projectCategoryPrefix)
	if err != nil {
		return 0
	}
	today := now.Format("2006-01-02")
	changed := 0
	for _, rp := range pages {
		rp = filepath.ToSlash(rp)
		if !IsProjectRepPage(rp) {
			continue
		}
		did := false
		_ = s.UpdatePage(rp, func(cur *Page) (*Page, error) {
			if cur == nil || cur.Meta.Archived || IsEffectivelySuperseded(rp, cur.Meta) {
				return nil, nil
			}
			next, _, _, ok := expireOpenQuestionsInBody(cur.Body, now, minAgeDays)
			if !ok {
				return nil, nil
			}
			cur.Body = next
			cur.Meta.Updated = today
			did = true
			return cur, nil
		})
		if did {
			changed++
		}
	}
	return changed
}

// CollectStaleOpenQuestions walks project 대표페이지 under wikiDir and returns
// the open questions asked at least minAgeDays ago (undated bullets count as
// stale — they have been sitting since some past write), oldest first. Archived
// projects are skipped. Read-only, no Store needed — mirrors the morning
// letter's deadline scan.
func CollectStaleOpenQuestions(wikiDir string, minAgeDays int, now time.Time) []OpenQuestion {
	if wikiDir == "" {
		return nil
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var out []OpenQuestion
	_ = filepath.Walk(wikiDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible entries in walk
		}
		if info.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, rerr := filepath.Rel(wikiDir, path)
		if rerr != nil {
			return nil //nolint:nilerr // outside root — skip
		}
		rel = filepath.ToSlash(rel)
		if !IsProjectRepPage(rel) {
			return nil
		}
		page, perr := ParsePageFile(path)
		if perr != nil || page == nil || page.Meta.Archived ||
			IsEffectivelySuperseded(rel, page.Meta) {
			return nil //nolint:nilerr // unreadable/archived/superseded — skip
		}
		project, _ := ProjectNameOf(rel)
		if t := strings.TrimSpace(page.Meta.Title); t != "" {
			project = t
		}
		for _, item := range OpenQuestionsIn(page.Body) {
			age := -1
			if item.Asked != "" {
				asked, aerr := time.ParseInLocation("2006-01-02", item.Asked, now.Location())
				if aerr != nil {
					continue
				}
				age = int(today.Sub(asked).Hours() / 24)
				if age < minAgeDays {
					continue
				}
			}
			out = append(out, OpenQuestion{
				Project:  project,
				Question: item.Question,
				Asked:    item.Asked,
				AgeDays:  age,
				Path:     rel,
			})
		}
		return nil
	})

	// Oldest first; undated ("" sorts first) are treated as oldest.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Asked != out[j].Asked {
			return out[i].Asked < out[j].Asked
		}
		return out[i].Project < out[j].Project
	})
	return out
}
