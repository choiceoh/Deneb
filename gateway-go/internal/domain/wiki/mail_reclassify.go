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
)

// ReclassifyResult is one re-filed mail page.
type ReclassifyResult struct {
	From    string
	To      string
	Project string
}

// ReclassifyUnlinkedMailAnalyses re-files pages from the category-level
// 메일분석 bucket into per-project slots, at most maxMoves per call.
//
// Retention-archived pages stay put: they are retired, and re-filing them
// would resurrect them into a project's active 메일분석 slot. The re-file is a
// metadata repair, so it never stamps Updated (link_prune's no-stamp doctrine)
// — stamping extended the mail's 90d retention window and reset the target
// project's dormancy clock.
func (s *Store) ReclassifyUnlinkedMailAnalyses(maxMoves int) []ReclassifyResult {
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
		if page.Meta.Archived {
			continue // retention-archived — retired mail must not be re-filed
		}
		project := reclassifyTarget(page, projects)
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
		// The project-corner ownership edge: mail → 대표페이지 via Related. No
		// Updated stamp — see the method comment (metadata repair, no re-stamp).
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
			return cur, nil
		})
		moved = append(moved, ReclassifyResult{From: rp, To: dst, Project: project})
	}
	return moved
}

// FindMailAnalysisPage returns the path of the existing mail-analysis page
// carrying msgID — searching every 메일분석 bucket (per-project, category-level
// unlinked, legacy mail-analyses) — or "" when none exists. Mail pages are
// keyed by message ID as the filename, so this is a cheap index scan. It backs
// the "메일 1통 = 1페이지" contract's global lookup: when the analysis cache is
// bypassed/lost and the analyzer resolves projects differently on a re-run,
// the same msgID must update its existing page in place, not mint a second
// page in another folder.
func (s *Store) FindMailAnalysisPage(msgID string) string {
	msgID = strings.TrimSpace(msgID)
	if s == nil || msgID == "" {
		return ""
	}
	want := msgID + ".md"
	for p := range s.SnapshotEntries() {
		if IsMailAnalysisPath(p) && path.Base(p) == want {
			return p
		}
	}
	return ""
}

// reclassifyTarget picks the owning project for an unlinked mail page, or ""
// when no unambiguous signal exists (모호하면 잔류 — a wrong filing is worse than
// an unfiled mail). Matching runs against the caller's already-fetched project
// list; re-listing per candidate was a silent N× knownProjects scan.
func reclassifyTarget(page *Page, projects []ProjectRef) string {
	// Signal 1: Related entries resolving into projects — but only when they
	// all agree. Two DISTINCT related projects are exactly the ambiguity the
	// doctrine says to leave alone (returning the first was arbitrary).
	related := ""
	for _, r := range page.Meta.Related {
		name, ok := ProjectNameOf(strings.ReplaceAll(strings.TrimSpace(r), "\\", "/"))
		if !ok {
			continue
		}
		if related != "" && related != name {
			return "" // ≥2 distinct projects → ambiguous, stay put
		}
		related = name
	}
	if related != "" {
		return related
	}
	// Signal 2: the title names exactly one known project (normalized
	// containment on the passed project set).
	hay := normalizeTitleKey(strings.TrimSpace(page.Meta.Title))
	if hay == "" {
		return ""
	}
	matched := ""
	for _, ref := range projects {
		if bestProjectKeyIn(hay, ref) == "" {
			continue
		}
		name, ok := ProjectNameOf(ref.Path)
		if !ok {
			continue
		}
		if matched != "" && matched != name {
			return "" // names two projects → ambiguous, stay put
		}
		matched = name
	}
	return matched
}
