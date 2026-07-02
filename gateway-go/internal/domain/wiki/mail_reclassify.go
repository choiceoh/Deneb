// mail_reclassify.go — retroactive filing of unlinked mail analyses.
//
// A mail the analyzer couldn't link at arrival lands in the category-level
// 프로젝트/메일분석/ bucket. Projects appear later (a folder minted the day after
// the first mails about it), so the bucket accumulates pages that NOW have an
// obvious home. This pass re-files them deterministically — no LLM:
//
//	signal 1: a Related entry resolving to a project (the link pruner repairs
//	          related paths, so a repaired edge is a strong signal);
//	signal 2: the mail title names exactly ONE known project (normalized
//	          containment; ambiguity = stay put — a wrong filing is worse
//	          than an unfiled mail).
//
// Runs from the wiki-review task, capped per cycle. MovePage keeps every
// inbound reference intact; the project's 대표페이지 is appended to the moved
// mail's Related so the project corner picks it up (projectOwnedRefs resolves
// ownership through that edge).
package wiki

import (
	"path"
	"strings"
	"time"
)

// ReclassifyResult is one re-filed mail page.
type ReclassifyResult struct {
	From    string
	To      string
	Project string
}

// ReclassifyUnlinkedMailAnalyses re-files pages from the category-level
// 메일분석 bucket into per-project slots, at most maxMoves per call.
func (s *Store) ReclassifyUnlinkedMailAnalyses(now time.Time, maxMoves int) []ReclassifyResult {
	if s == nil || maxMoves <= 0 {
		return nil
	}
	bucket := projectCategoryPrefix + "/" + MailAnalysisDir
	pages, err := s.ListPages(bucket)
	if err != nil {
		return nil
	}

	projects := s.knownProjects()
	repByName := make(map[string]string, len(projects))
	for _, ref := range projects {
		if name, ok := ProjectNameOf(ref.Path); ok {
			repByName[name] = ref.Path
		}
	}

	var moved []ReclassifyResult
	for _, rp := range pages {
		if len(moved) >= maxMoves {
			break
		}
		rp = strings.ReplaceAll(rp, "\\", "/")
		if path.Dir(rp) != bucket { // only the flat unlinked bucket
			continue
		}
		page, perr := s.ReadPage(rp)
		if perr != nil || page == nil {
			continue
		}
		project := reclassifyTarget(s, page, projects)
		if project == "" {
			continue
		}
		repPath, ok := repByName[project]
		if !ok {
			continue
		}
		dst := MailAnalysisPagePath(project, strings.TrimSuffix(path.Base(rp), ".md"))
		if err := s.MovePage(rp, dst); err != nil {
			continue // e.g. same msgID already filed there — leave for the operator
		}
		// The project-corner ownership edge: mail → 대표페이지 via Related.
		_ = s.UpdatePage(dst, func(cur *Page) (*Page, error) {
			if cur == nil {
				return nil, nil
			}
			for _, r := range cur.Meta.Related {
				if r == repPath {
					return nil, nil // already linked — skip the write
				}
			}
			cur.Meta.Related = append(cur.Meta.Related, repPath)
			cur.Meta.Updated = now.Format("2006-01-02")
			return cur, nil
		})
		moved = append(moved, ReclassifyResult{From: rp, To: dst, Project: project})
	}
	return moved
}

// reclassifyTarget picks the owning project for an unlinked mail page, or ""
// when no unambiguous signal exists.
func reclassifyTarget(s *Store, page *Page, projects []ProjectRef) string {
	// Signal 1: a Related entry already resolving into a project.
	for _, r := range page.Meta.Related {
		if name, ok := ProjectNameOf(strings.TrimSpace(r)); ok {
			return name
		}
	}
	// Signal 2: the title names exactly one known project.
	title := strings.TrimSpace(page.Meta.Title)
	if title == "" {
		return ""
	}
	matches := s.MatchProjectsInText(title, 2)
	if len(matches) != 1 {
		return "" // zero or ambiguous — stay put
	}
	if name, ok := ProjectNameOf(matches[0].Path); ok {
		return name
	}
	return ""
}
