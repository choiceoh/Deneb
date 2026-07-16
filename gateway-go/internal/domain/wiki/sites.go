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

// ValidSiteStatuses are the lifecycle values a 현장 page may carry. Empty string is
// also valid and means 미분류 (the status frontmatter key is omitted on render).
var ValidSiteStatuses = []string{"후보", "계약", "개설", "준공"}

// NormalizeSiteStatus trims status and accepts 후보/계약/개설/준공 or "" (미분류).
// Any other value is rejected so free-form labels can't sneak onto the map filter.
func NormalizeSiteStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return "", nil
	}
	for _, v := range ValidSiteStatuses {
		if status == v {
			return status, nil
		}
	}
	return "", fmt.Errorf("invalid status %q (want 후보/계약/개설/준공 or empty)", status)
}

// SetSiteStatus sets (or clears) the lifecycle status on an existing 현장 page.
// Unlike UpsertSitePage, empty status clears Meta.Status (미분류) rather than
// leaving it unchanged. Rejects non-현장 paths and unknown status labels.
func (s *Store) SetSiteStatus(path, status string) error {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if !IsProjectSitePage(path) {
		return fmt.Errorf("not a site page: %s", path)
	}
	normalized, err := NormalizeSiteStatus(status)
	if err != nil {
		return err
	}
	return s.UpdatePage(path, func(cur *Page) (*Page, error) {
		if cur == nil {
			return nil, fmt.Errorf("site page not found: %s", path)
		}
		cur.Meta.Status = normalized
		return cur, nil
	})
}

// EnsureSitePage finds or creates a 현장 page for address under the project that
// owns projectPath (대표페이지 or any path under 프로젝트/<folder>/…). Idempotent:
// an existing page whose address matches returns that path with created=false.
// New stubs copy 거래처·kinds from the project 대표페이지 when available.
func (s *Store) EnsureSitePage(projectPath, address string) (path string, created bool, err error) {
	projectPath = filepath.ToSlash(strings.TrimSpace(projectPath))
	address = normalizeSiteName(address)
	if projectPath == "" {
		return "", false, fmt.Errorf("path is required")
	}
	if address == "" {
		return "", false, fmt.Errorf("address is required")
	}
	folder, ok := ProjectNameOf(projectPath)
	if !ok {
		return "", false, fmt.Errorf("not a project path: %s", projectPath)
	}
	paths, err := s.ListPages(projectCategoryPrefix)
	if err != nil {
		return "", false, fmt.Errorf("list project pages: %w", err)
	}
	usedNames := make(map[string]bool)
	for _, p := range paths {
		if !IsProjectSitePage(p) {
			continue
		}
		pageFolder, ok := ProjectNameOf(p)
		if !ok || pageFolder != folder {
			continue
		}
		usedNames[strings.TrimSuffix(filepath.Base(p), ".md")] = true
		page, readErr := s.ReadPage(p)
		if readErr != nil || page == nil {
			continue
		}
		if normalizeSiteName(page.Meta.Address) == address {
			return p, false, nil
		}
	}
	var client string
	var kinds []string
	for _, ref := range s.knownProjects() {
		refFolder, ok := ProjectNameOf(ref.Path)
		if ok && refFolder == folder {
			client, kinds = ref.Client, ref.Kinds
			break
		}
	}
	name := deriveSiteName(address, usedNames)
	path, err = s.UpsertSitePage(folder, name, SiteFields{
		Address: address, Client: client, Kinds: kinds,
	})
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}

// UpdateSitePage applies non-empty SiteFields to an existing 현장 page (partial
// edit by path). Unlike UpsertSitePage it refuses to create missing pages, and
// unlike SetSiteStatus an empty Status field leaves Meta.Status unchanged.
func (s *Store) UpdateSitePage(path string, f SiteFields) error {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if !IsProjectSitePage(path) {
		return fmt.Errorf("not a site page: %s", path)
	}
	return s.UpdatePage(path, func(cur *Page) (*Page, error) {
		if cur == nil {
			return nil, fmt.Errorf("site page not found: %s", path)
		}
		applySiteFields(cur, f, false)
		return cur, nil
	})
}

// applySiteFields copies non-empty SiteFields onto page. created controls whether
// an empty body gets the standard scaffold (Upsert create path only).
func applySiteFields(page *Page, f SiteFields, created bool) {
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
		page.Body = siteBodyScaffold(page.Meta.Title)
	}
	if note := strings.TrimSpace(f.Note); note != "" {
		page.Body = strings.TrimRight(page.Body, "\n") + "\n\n" + note + "\n"
	}
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
		applySiteFields(page, f, created)
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
	// Abort on a walk failure rather than proceed — this is a WRITE path, and an
	// empty listing would make the seeder treat existing 현장 pages as missing and
	// re-create/clobber stubs.
	paths, err := s.ListPages(projectCategoryPrefix)
	if err != nil {
		return nil, fmt.Errorf("list project pages: %w", err)
	}
	sitePagesByFolder := make(map[string][]string)
	for _, p := range paths {
		if !IsProjectSitePage(p) {
			continue
		}
		if folder, ok := ProjectNameOf(p); ok {
			sitePagesByFolder[folder] = append(sitePagesByFolder[folder], p)
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
