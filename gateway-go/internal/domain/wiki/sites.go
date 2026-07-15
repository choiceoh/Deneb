// sites.go — authoring the 현장 공통 포맷. A 현장 lives as a page under
// 프로젝트/<project>/현장/<name>.md with a standard set of frontmatter fields
// (address·status·용량·에너지원/특성 + the 공정 일정 milestone dates). The read path
// (ProjectSites, the 현장 지도) is in project_status.go; this file owns the write.
package wiki

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SiteFields are the authorable fields of a 현장 page — the 현장 공통 포맷. Empty
// fields are LEFT UNCHANGED on an update (partial edit), so the agent can set 계약일
// today and 준공검사일 months later without clobbering the rest of the page.
type SiteFields struct {
	Client               string
	Address              string   // canonical "광역약칭 시/군 읍/면" (normalized on write)
	Status               string   // 후보/계약/개설/준공
	Capacity             float64  // 용량 MW
	Kinds                []string // 에너지원/특성 (normalized to the fixed vocabulary)
	ContractDate         string   // 계약일 YYYY-MM-DD
	ConstructionStart    string   // 공사개시일
	ModuleDelivery       string   // 모듈입고(기간 가능)
	PreUseInspection     string   // 사용전검사일
	CompletionInspection string   // 준공검사일
	Summary              string   // one-line 개요
	Note                 string   // freeform note appended under the standard body
}

// UpsertSitePage creates or edits a 현장 page in the 현장 공통 포맷. Non-empty fields
// overwrite; empty fields are preserved (partial edit). A newly created page gets a
// standard body scaffold. Returns the page path so the caller can link/report it.
func (s *Store) UpsertSitePage(project, name string, f SiteFields) (string, error) {
	project = strings.TrimSpace(project)
	name = strings.TrimSpace(name)
	if project == "" || name == "" {
		return "", fmt.Errorf("project and site name are required")
	}
	path := SitePagePath(project, name)
	err := s.UpdatePage(path, func(cur *Page) (*Page, error) {
		page := cur
		created := page == nil
		if created {
			page = NewPage(name, projectCategoryPrefix, nil)
			page.Meta.Type = "site"
		}
		if f.Client != "" {
			page.Meta.Client = f.Client
		}
		if f.Address != "" {
			page.Meta.Address = normalizeSiteName(f.Address)
		}
		if f.Status != "" {
			page.Meta.Status = strings.TrimSpace(f.Status)
		}
		if f.Capacity > 0 {
			page.Meta.Capacity = f.Capacity
		}
		if len(f.Kinds) > 0 {
			page.Meta.Kinds = normalizeKinds(f.Kinds)
		}
		if f.ContractDate != "" {
			page.Meta.ContractDate = strings.TrimSpace(f.ContractDate)
		}
		if f.ConstructionStart != "" {
			page.Meta.ConstructionStart = strings.TrimSpace(f.ConstructionStart)
		}
		if f.ModuleDelivery != "" {
			page.Meta.ModuleDelivery = strings.TrimSpace(f.ModuleDelivery)
		}
		if f.PreUseInspection != "" {
			page.Meta.PreUseInspection = strings.TrimSpace(f.PreUseInspection)
		}
		if f.CompletionInspection != "" {
			page.Meta.CompletionInspection = strings.TrimSpace(f.CompletionInspection)
		}
		if f.Summary != "" {
			page.Meta.Summary = strings.TrimSpace(f.Summary)
		}
		if created && strings.TrimSpace(page.Body) == "" {
			page.Body = siteBodyScaffold(name)
		}
		if note := strings.TrimSpace(f.Note); note != "" {
			page.Body = strings.TrimRight(page.Body, "\n") + "\n\n" + note + "\n"
		}
		return page, nil
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// siteBodyScaffold is the standard 현장 body — the structured 일정 lives in
// frontmatter, so the body just holds narrative sections.
func siteBodyScaffold(name string) string {
	return "# " + name + " 현장\n\n## 개요\n\n## 공정 현황\n\n## 이슈\n"
}

// SeedSitePages bootstraps 현장 page stubs from a project's 대표페이지 Meta.Sites —
// one page per site address not yet backed by a 현장 page — so existing projects
// enter the 현장 공통 포맷 without hand-creating each page. The stub carries the
// address plus the project's 거래처·특성 as defaults; status·용량·공정 일정 stay
// blank for the operator to fill. project=="" seeds every active project.
// Idempotent (an address already covered by a 현장 page is skipped). Returns the
// created page paths, sorted.
func (s *Store) SeedSitePages(project string) ([]string, error) {
	refs := s.knownProjects()

	// One corpus pass: bucket existing 현장 pages under their owning project folder.
	sitePagesByFolder := make(map[string][]string)
	if paths, err := s.ListPages(projectCategoryPrefix); err == nil {
		for _, p := range paths {
			if !IsProjectSitePage(p) {
				continue
			}
			if folder, ok := ProjectNameOf(p); ok {
				sitePagesByFolder[folder] = append(sitePagesByFolder[folder], p)
			}
		}
	}

	var created []string
	for i := range refs {
		ref := &refs[i]
		folder, ok := ProjectNameOf(ref.Path)
		if !ok || len(ref.Sites) == 0 {
			continue
		}
		if project != "" && folder != project && ref.Name != project {
			continue
		}
		// Addresses already covered + page names already taken in this project.
		covered := make(map[string]bool)
		usedNames := make(map[string]bool)
		for _, sp := range sitePagesByFolder[folder] {
			usedNames[strings.TrimSuffix(filepath.Base(sp), ".md")] = true
			if page, err := s.ReadPage(sp); err == nil && page != nil {
				if a := normalizeSiteName(page.Meta.Address); a != "" {
					covered[a] = true
				}
			}
		}
		for _, addr := range ref.Sites {
			na := normalizeSiteName(addr)
			if na == "" || covered[na] {
				continue
			}
			covered[na] = true
			name := deriveSiteName(na, usedNames)
			usedNames[name] = true
			path, err := s.UpsertSitePage(folder, name, SiteFields{
				Address: na, Client: ref.Client, Kinds: ref.Kinds,
			})
			if err != nil {
				return created, err
			}
			created = append(created, path)
		}
	}
	sort.Strings(created)
	return created, nil
}

// deriveSiteName picks a page filename for a site address: the finest token
// (읍/면/동/리) by default, disambiguated with the parent token (then a numeric
// suffix) when a name is already taken in the project.
func deriveSiteName(address string, used map[string]bool) string {
	toks := strings.Fields(address)
	if len(toks) == 0 {
		return "현장"
	}
	name := toks[len(toks)-1]
	if !used[name] {
		return name
	}
	if len(toks) >= 2 {
		if combined := toks[len(toks)-2] + name; !used[combined] {
			return combined
		}
	}
	for i := 2; ; i++ {
		if cand := fmt.Sprintf("%s-%d", name, i); !used[cand] {
			return cand
		}
	}
}
