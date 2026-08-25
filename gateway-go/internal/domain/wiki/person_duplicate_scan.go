package wiki

// person_duplicate_scan.go — 인물 pages that look like the same person filed
// twice, e.g. `인물/이영민.md` beside `인물/이영민-차장.md`.
//
// The wiki mints a person page from several sources (address-book sync, mail
// analysis, the dreamer's seeding), and each source names people differently:
// one carries the rank, another does not, a third disambiguates with the
// employer in parentheses. The result is two nodes for one person, so recall
// splits — half the evidence hangs off each page and neither answers well.
//
// Detection only. Merging is never automatic: `이영민 차장` and `이영민 팀장`
// may be two people, exactly the case the 2026-07-28 over-merge incident
// created, and Korean names are short enough that edit distance is worthless
// (distance 1 is a different name). The finding therefore carries the evidence
// an operator needs — every page in the group and the company domains each one
// claims — and stops there.

import (
	"path/filepath"
	"sort"
	"strings"
)

// DuplicatePersonGroup is one set of 인물 pages sharing a base name.
type DuplicatePersonGroup struct {
	Name  string                `json:"name"`
	Pages []DuplicatePersonPage `json:"pages"`
}

// DuplicatePersonPage is one candidate page plus the evidence that tells the
// operator whether it is the same person: the addresses it claims.
type DuplicatePersonPage struct {
	PagePath string   `json:"pagePath"`
	Title    string   `json:"title"`
	Domains  []string `json:"domains"`
}

// personRankTokens are the Korean job titles a page name may or may not carry.
// Stripping them is what makes `이영민 차장` and `이영민` land in one group.
var personRankTokens = []string{
	"회장", "부회장", "사장", "부사장", "전무", "상무", "이사", "본부장", "실장",
	"소장", "팀장", "부장", "차장", "과장", "대리", "주임", "사원", "대표",
	"교수", "박사", "변호사", "세무사", "회계사", "기사", "선임", "책임", "수석",
}

// DuplicatePersonGroups returns 인물 pages that share a base name, most
// populous group first, capped at limit groups (0 = no cap). Advisory only —
// callers must not merge on this signal.
func (s *Store) DuplicatePersonGroups(limit int) []DuplicatePersonGroup {
	relPaths, err := s.ListPages("인물")
	if err != nil {
		return nil
	}
	byName := map[string][]DuplicatePersonPage{}
	reviewedMeta := map[string]Frontmatter{}
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp)
		page, rerr := s.ReadPage(rp)
		if rerr != nil || page == nil || page.Meta.Archived {
			continue
		}
		title := page.Meta.Title
		if title == "" {
			title = strings.TrimSuffix(filepath.Base(rp), ".md")
		}
		base := personBaseName(title)
		if base == "" {
			continue
		}
		domains := companyEmailDomains(append(append([]string(nil), page.Meta.Emails...),
			bodyEmailAddresses(page.Body)...))
		sort.Strings(domains)
		byName[base] = append(byName[base], DuplicatePersonPage{
			PagePath: rp, Title: title, Domains: domains,
		})
		reviewedMeta[rp] = page.Meta
	}

	var out []DuplicatePersonGroup
	for name, pages := range byName {
		if len(pages) < 2 {
			continue
		}
		sort.Slice(pages, func(i, j int) bool { return pages[i].PagePath < pages[j].PagePath })
		// A group is settled once EVERY page in it has the others recorded as
		// judged. One page acked out of three still leaves a live question, so
		// the group stays until the operator has closed all of them.
		settled := true
		for _, page := range pages {
			meta, ok := reviewedMeta[page.PagePath]
			if !ok || !identityEvidenceReviewed(meta, duplicatePeerEvidence(page.PagePath, pages)) {
				settled = false
				break
			}
		}
		if settled {
			continue
		}
		out = append(out, DuplicatePersonGroup{Name: name, Pages: pages})
	}
	// Biggest groups first, then by name so the list is stable across cycles.
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Pages) != len(out[j].Pages) {
			return len(out[i].Pages) > len(out[j].Pages)
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// personBaseName reduces a person page's title to the bare name: rank tokens,
// parenthesized qualifiers, and separators removed. Returns "" when nothing but
// a rank is left (a page titled just `부장` names no one).
func personBaseName(title string) string {
	name := title
	// Drop a parenthesized qualifier — `김익환 부장 (GS풍력발전)`.
	if i := strings.IndexAny(name, "(（"); i > 0 {
		name = name[:i]
	}
	name = strings.NewReplacer("-", " ", "_", " ", "·", " ").Replace(name)
	fields := strings.Fields(name)
	var kept []string
	for _, f := range fields {
		if isPersonRankToken(f) {
			continue
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, "")
}

func isPersonRankToken(token string) bool {
	for _, rank := range personRankTokens {
		if token == rank {
			return true
		}
	}
	return false
}
