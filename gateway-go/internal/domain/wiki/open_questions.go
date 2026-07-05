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
	// OpenQuestionsHeading is the H2 section holding a project's open questions.
	OpenQuestionsHeading = "미해결 질문"
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
	var out []OpenQuestionItem
	_, sections := (&Page{Body: body}).SplitByH2()
	for _, sec := range sections {
		if !strings.EqualFold(strings.TrimSpace(sec.Heading), OpenQuestionsHeading) {
			continue
		}
		for _, ln := range strings.Split(sec.Content, "\n") {
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
			if len(out) >= maxOpenQuestionItems {
				return out
			}
		}
	}
	return out
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
		if perr != nil || page == nil || page.Meta.Archived {
			return nil //nolint:nilerr // unreadable/archived — skip
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
