// person_homonym_scan.go — 인물 pages that hold two identities.
//
// The frontmatter identity backfill refuses to write ambiguous addresses and
// contacts sync no longer merges them into the body, but pages merged before
// those guards still carry both employers — and a merged node answers "그 사람
// 연락처" with the wrong company's number. Detection only: splitting is the
// operator's call (the 2026-07-28 over-merge is why nothing here auto-fixes).
package wiki

import (
	"path/filepath"
	"sort"
	"strings"
)

// HomonymPerson is one 인물 page whose own contact details span two employers.
type HomonymPerson struct {
	PagePath string   `json:"pagePath"`
	Title    string   `json:"title"`
	Domains  []string `json:"domains"`
}

// HomonymPersonFor evaluates ONE page: the operator-facing card lane needs to
// re-check the exact finding a posted card names, not diff against a capped
// list (a finding outside the cap would read as resolved and retire a live
// card). Reports false when the page is gone, archived, single-identity, or
// already judged.
func (s *Store) HomonymPersonFor(relPath string) (HomonymPerson, bool) {
	if s == nil {
		return HomonymPerson{}, false
	}
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	page, err := s.ReadPage(relPath)
	if err != nil || page == nil || page.Meta.Archived {
		return HomonymPerson{}, false
	}
	domains := companyEmailDomains(append(append([]string(nil), page.Meta.Emails...),
		bodyEmailAddresses(page.Body)...))
	if len(domains) < 2 {
		return HomonymPerson{}, false
	}
	sort.Strings(domains)
	if identityEvidenceReviewed(page.Meta, domains) {
		return HomonymPerson{}, false
	}
	title := page.Meta.Title
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(relPath), ".md")
	}
	return HomonymPerson{PagePath: relPath, Title: title, Domains: domains}, true
}

// HomonymPersonPages returns up to limit such pages, sorted by path so the
// dream report and the operator card agree on what "the first five" means.
func (s *Store) HomonymPersonPages(limit int) []HomonymPerson {
	if s == nil || limit <= 0 {
		return nil
	}
	relPaths, err := s.ListPages("인물")
	if err != nil {
		return nil
	}
	sort.Strings(relPaths)
	var out []HomonymPerson
	for _, rp := range relPaths {
		if len(out) >= limit {
			break
		}
		rp = filepath.ToSlash(rp)
		page, rerr := s.ReadPage(rp)
		if rerr != nil || page == nil || page.Meta.Archived {
			continue
		}
		domains := companyEmailDomains(append(append([]string(nil), page.Meta.Emails...),
			bodyEmailAddresses(page.Body)...))
		if len(domains) < 2 {
			continue
		}
		sort.Strings(domains)
		// Already judged: the operator looked at exactly these domains and
		// decided. Re-asking is what made this scan feel like nagging.
		if identityEvidenceReviewed(page.Meta, domains) {
			continue
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(rp), ".md")
		}
		out = append(out, HomonymPerson{PagePath: rp, Title: title, Domains: domains})
	}
	return out
}
