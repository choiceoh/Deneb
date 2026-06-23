package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeProjectStatusSource struct {
	statuses []wiki.ProjectStatus
	err      error
}

func (f fakeProjectStatusSource) ProjectStatuses() ([]wiki.ProjectStatus, error) {
	return f.statuses, f.err
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
	m := ProjectMethods(projectDepsFor(fakeProjectStatusSource{}, nil))
	if _, ok := m["miniapp.project.digests"]; !ok {
		t.Fatalf("miniapp.project.digests not registered with a wiki factory")
	}
}

func TestProjectDigests_RequiresAuth(t *testing.T) {
	h := projectDigests(projectDepsFor(fakeProjectStatusSource{}, nil))
	resp := h(context.Background(), reqWith(t, "miniapp.project.digests", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized without client identity")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestProjectDigests_WikiUnavailableDegrades(t *testing.T) {
	resp := projectDigests(projectDepsFor(nil, errors.New("wiki disabled")))(authedCtx(), reqWith(t, "miniapp.project.digests", nil))
	if resp.OK {
		t.Fatalf("expected UNAVAILABLE when the wiki factory errors")
	}
}

func TestProjectDigests_MapsStatusesToRows(t *testing.T) {
	// The wiki already sorts newest-first; the handler preserves that order and
	// maps each ProjectStatus to a wire row.
	src := fakeProjectStatusSource{statuses: []wiki.ProjectStatus{
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
