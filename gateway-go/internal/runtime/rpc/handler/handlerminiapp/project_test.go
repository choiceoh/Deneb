package handlerminiapp

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestProjectMethods_NilStoreReturnsNil(t *testing.T) {
	if got := ProjectMethods(ProjectDeps{}); got != nil {
		t.Fatalf("ProjectMethods(no store) = %v, want nil", got)
	}
}

func TestProjectMethods_RegistersWithStore(t *testing.T) {
	m := ProjectMethods(ProjectDeps{Store: NewProjectDigestStore(t.TempDir())})
	if _, ok := m["miniapp.project.digests"]; !ok {
		t.Fatalf("miniapp.project.digests not registered with a store")
	}
}

func TestProjectDigests_RequiresAuth(t *testing.T) {
	h := projectDigests(ProjectDeps{Store: NewProjectDigestStore(t.TempDir())})
	resp := h(context.Background(), reqWith(t, "miniapp.project.digests", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized without client identity")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestProjectDigests_EmptyStoreReturnsNoRows(t *testing.T) {
	// A store whose dir was never written (no dream cycle yet) is not an error.
	resp := projectDigests(ProjectDeps{Store: NewProjectDigestStore(t.TempDir())})(authedCtx(), reqWith(t, "miniapp.project.digests", nil))
	if !resp.OK {
		t.Fatalf("expected OK on empty store, got code=%s", resp.Error.Code)
	}
	var got ProjectDigestsOut
	decode(t, resp, &got)
	if len(got.Digests) != 0 {
		t.Fatalf("digests = %d, want 0", len(got.Digests))
	}
}

func TestProjectDigests_ReturnsStoredNewestFirst(t *testing.T) {
	store := NewProjectDigestStore(t.TempDir())
	base := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	// Saved out of order; the handler must return newest UpdatedAt first.
	if err := store.SaveDigest(ProjectDigestInput{
		Project: "영산고", Headline: "모듈 발주 완료",
		Bullets: []string{"계약 체결", "납기 6월 말"}, Due: "2026-06-30",
		UpdatedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDigest(ProjectDigestInput{
		Project: "남도풍력", Headline: "환경영향평가 접수",
		UpdatedAt: base.Add(48 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	resp := projectDigests(ProjectDeps{Store: store})(authedCtx(), reqWith(t, "miniapp.project.digests", nil))
	if !resp.OK {
		t.Fatalf("expected OK, got code=%s", resp.Error.Code)
	}
	var got ProjectDigestsOut
	decode(t, resp, &got)
	if len(got.Digests) != 2 {
		t.Fatalf("digests = %d, want 2", len(got.Digests))
	}
	// Newest first: 남도풍력 (base+48h) before 영산고 (base).
	if got.Digests[0].Project != "남도풍력" {
		t.Errorf("first project = %q, want 남도풍력 (newest)", got.Digests[0].Project)
	}
	ys := got.Digests[1]
	if ys.Project != "영산고" || ys.Headline != "모듈 발주 완료" || ys.Due != "2026-06-30" {
		t.Errorf("영산고 row = %+v, unexpected", ys)
	}
	if len(ys.Bullets) != 2 {
		t.Errorf("영산고 bullets = %v, want 2", ys.Bullets)
	}
	if ys.UpdatedAtMs != base.UnixMilli() {
		t.Errorf("영산고 updatedAtMs = %d, want %d", ys.UpdatedAtMs, base.UnixMilli())
	}
}

func TestProjectDigestStore_SaveUpsertsByProject(t *testing.T) {
	store := NewProjectDigestStore(t.TempDir())
	base := time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC)
	if err := store.SaveDigest(ProjectDigestInput{Project: "영산고", Headline: "1차", UpdatedAt: base}); err != nil {
		t.Fatal(err)
	}
	// Re-saving the same project overwrites (newest cycle wins), not appends.
	if err := store.SaveDigest(ProjectDigestInput{Project: "영산고", Headline: "2차", UpdatedAt: base.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	recs, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1 (upsert, not append)", len(recs))
	}
	if recs[0].Headline != "2차" {
		t.Errorf("headline = %q, want 2차 (latest)", recs[0].Headline)
	}
}
