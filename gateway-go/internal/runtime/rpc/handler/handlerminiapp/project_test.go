package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeProjectStatusSource struct {
	statuses      []wiki.ProjectStatus
	sites         []wiki.ProjectSite
	err           error
	setStatusErr  error
	lastSetPath   string
	lastSetStatus string
}

func (f *fakeProjectStatusSource) ProjectStatuses() ([]wiki.ProjectStatus, error) {
	return f.statuses, f.err
}

func (f *fakeProjectStatusSource) ProjectSites() ([]wiki.ProjectSite, error) {
	return f.sites, f.err
}

func (f *fakeProjectStatusSource) SetSiteStatus(path, status string) error {
	f.lastSetPath = path
	f.lastSetStatus = status
	return f.setStatusErr
}

func projectDepsFor(src ProjectStatusSource, factoryErr error) ProjectDeps {
	return ProjectDeps{Wiki: func() (ProjectStatusSource, error) {
		if factoryErr != nil {
			return nil, factoryErr
		}
		return src, nil
	}}
}

func TestProjectMethods_NilWikiReturnsNil(t *testing.T) {
	if got := ProjectMethods(ProjectDeps{}); got != nil {
		t.Fatalf("ProjectMethods(no wiki) = %v, want nil", got)
	}
}

func TestProjectMethods_RegistersWithWiki(t *testing.T) {
	m := ProjectMethods(projectDepsFor(&fakeProjectStatusSource{}, nil))
	for _, name := range []string{
		"miniapp.project.digests",
		"miniapp.project.sites",
		"miniapp.project.site.setStatus",
	} {
		if _, ok := m[name]; !ok {
			t.Fatalf("%s not registered with a wiki factory", name)
		}
	}
}

func TestProjectDigestsReturnsUnauthorizedWithoutIdentity(t *testing.T) {
	h := projectDigests(projectDepsFor(&fakeProjectStatusSource{}, nil))
	resp := h(context.Background(), reqWith(t, "miniapp.project.digests", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized without client identity")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestProjectDigestsReturnsUnavailableWhenWikiFactoryErrors(t *testing.T) {
	resp := projectDigests(projectDepsFor(nil, errors.New("wiki disabled")))(authedCtx(), reqWith(t, "miniapp.project.digests", nil))
	if resp.OK {
		t.Fatalf("expected UNAVAILABLE when the wiki factory errors")
	}
}

func TestProjectDigestsPreservesOrderAndMapsStatusesToRows(t *testing.T) {
	// The wiki already sorts newest-first; the handler preserves that order and
	// maps each ProjectStatus to a wire row.
	src := &fakeProjectStatusSource{statuses: []wiki.ProjectStatus{
		{Name: "남도풍력", Path: "프로젝트/남도풍력.md", Summary: "해상풍력 단지", Bullets: []string{"환경영향평가 접수"}, UpdatedMs: 200},
		{Name: "영산고", Path: "프로젝트/영산고.md", Summary: "옥상 태양광", Due: "2026-06-30", Bullets: []string{"모듈 발주 완료", "납기 6월 말"}, UpdatedMs: 100},
	}}
	resp := projectDigests(projectDepsFor(src, nil))(authedCtx(), reqWith(t, "miniapp.project.digests", nil))
	if !resp.OK {
		t.Fatalf("expected OK, got code=%s", resp.Error.Code)
	}
	var got ProjectDigestsOut
	decode(t, resp, &got)
	if len(got.Digests) != 2 {
		t.Fatalf("digests = %d, want 2", len(got.Digests))
	}
	if got.Digests[0].Project != "남도풍력" {
		t.Errorf("first project = %q, want 남도풍력", got.Digests[0].Project)
	}
	ys := got.Digests[1]
	if ys.Project != "영산고" || ys.Headline != "옥상 태양광" || ys.Due != "2026-06-30" || ys.Path != "프로젝트/영산고.md" {
		t.Errorf("영산고 row = %+v, unexpected", ys)
	}
	if len(ys.Bullets) != 2 || ys.UpdatedAtMs != 100 {
		t.Errorf("영산고 row = %+v, want 2 bullets and updatedAtMs=100", ys)
	}
}

func TestProjectSitesReturnsUnavailableWhenWikiFactoryErrors(t *testing.T) {
	resp := projectSites(projectDepsFor(nil, errors.New("wiki disabled")))(authedCtx(), reqWith(t, "miniapp.project.sites", nil))
	if resp.OK {
		t.Fatalf("expected UNAVAILABLE when the wiki factory errors")
	}
}

func TestProjectSitesMapsActiveProjectsWithSitesToRows(t *testing.T) {
	// Every active project carrying Sites is emitted (whether or not it has a
	// 현재 상태 digest), mapped ProjectSite → wire row.
	src := &fakeProjectStatusSource{sites: []wiki.ProjectSite{
		{Name: "영산고", Client: "영산", Path: "프로젝트/영산고/대표.md", Due: "2026-06-30", Sites: []string{"전남 해남군 산이면"}, Kinds: []string{"태양광/토지"}, Capacity: 24, Status: "계약"},
		{Name: "군산수산리", Path: "프로젝트/군산수산리.md", Sites: []string{"전북 군산시 옥구읍 수산리", "전북 군산시 옥서면"}},
	}}
	resp := projectSites(projectDepsFor(src, nil))(authedCtx(), reqWith(t, "miniapp.project.sites", nil))
	if !resp.OK {
		t.Fatalf("expected OK, got code=%s", resp.Error.Code)
	}
	var got ProjectSitesOut
	decode(t, resp, &got)
	if len(got.Sites) != 2 {
		t.Fatalf("sites = %d, want 2", len(got.Sites))
	}
	first := got.Sites[0]
	if first.Project != "영산고" || first.Client != "영산" || first.Due != "2026-06-30" || first.Path != "프로젝트/영산고/대표.md" {
		t.Errorf("영산고 row = %+v, unexpected", first)
	}
	if len(first.Sites) != 1 || first.Sites[0] != "전남 해남군 산이면" {
		t.Errorf("영산고 sites = %v, want [전남 해남군 산이면]", first.Sites)
	}
	if len(first.Kinds) != 1 || first.Kinds[0] != "태양광/토지" || first.Capacity != 24 || first.Status != "계약" {
		t.Errorf("영산고 kinds/capacity/status = %v/%v/%q, want [태양광/토지]/24/계약", first.Kinds, first.Capacity, first.Status)
	}
	if len(got.Sites[1].Sites) != 2 {
		t.Errorf("군산수산리 sites = %v, want 2", got.Sites[1].Sites)
	}
}

func TestProjectSiteSetStatusWritesNormalizedStatus(t *testing.T) {
	src := &fakeProjectStatusSource{}
	path := "프로젝트/군산수산리/현장/수산리.md"
	resp := projectSiteSetStatus(projectDepsFor(src, nil))(authedCtx(), reqWith(t, "miniapp.project.site.setStatus", map[string]any{
		"path":   path,
		"status": " 개설 ",
	}))
	if !resp.OK {
		t.Fatalf("expected OK, got code=%s msg=%s", resp.Error.Code, resp.Error.Message)
	}
	if src.lastSetPath != path || src.lastSetStatus != " 개설 " {
		t.Errorf("SetSiteStatus args = (%q, %q), want (%q, %q)", src.lastSetPath, src.lastSetStatus, path, " 개설 ")
	}
	var got ProjectSiteSetStatusOut
	decode(t, resp, &got)
	if got.Path != path || got.Status != "개설" {
		t.Errorf("out = %+v, want path=%q status=개설", got, path)
	}
}

func TestProjectSiteSetStatusRejectsMissingPath(t *testing.T) {
	resp := projectSiteSetStatus(projectDepsFor(&fakeProjectStatusSource{}, nil))(authedCtx(), reqWith(t, "miniapp.project.site.setStatus", map[string]any{
		"status": "개설",
	}))
	if resp.OK {
		t.Fatal("expected error for missing path")
	}
}

func TestProjectSiteSetStatusMapsInvalidStatus(t *testing.T) {
	src := &fakeProjectStatusSource{setStatusErr: errors.New(`invalid status "공사중" (want 후보/계약/개설/준공 or empty)`)}
	resp := projectSiteSetStatus(projectDepsFor(src, nil))(authedCtx(), reqWith(t, "miniapp.project.site.setStatus", map[string]any{
		"path":   "프로젝트/군산수산리/현장/수산리.md",
		"status": "공사중",
	}))
	if resp.OK {
		t.Fatal("expected INVALID_REQUEST")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrInvalidRequest)
	}
}
