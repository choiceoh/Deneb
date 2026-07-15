// sites.go — authoring the 현장 공통 포맷. A 현장 lives as a page under
// 프로젝트/<project>/현장/<name>.md with a standard set of frontmatter fields
// (address·status·용량·에너지원/특성 + the 공정 일정 milestone dates). The read path
// (ProjectSites, the 현장 지도) is in project_status.go; this file owns the write.
package wiki

import (
	"fmt"
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
