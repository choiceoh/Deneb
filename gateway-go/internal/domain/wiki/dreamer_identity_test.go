package wiki

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestMergeDreamUpdate_KeepsIdentityOnRetarget: when synthesis proposed page X
// but the update was rerouted onto existing page Y, the update's identity
// fields describe X — appending its content to Y is the intended repair, but
// overwriting Y's id/summary/type/resource is how a smart-glasses page ended up
// with a scopolamine summary and a scopolamine id (2026-07-30).
func TestMergeDreamUpdate_KeepsIdentityOnRetarget(t *testing.T) {
	wd := &WikiDreamer{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	existing := NewPage("Even Realities G2 스마트 안경", "기타", nil)
	existing.Meta.ID = "even-realities-g2"
	existing.Meta.Summary = "G2 스마트 안경 — 번역·AI 탑재"
	existing.Meta.Type = "concept"
	existing.Meta.Resource = "https://evenrealities.example/g2"
	existing.Body = "# G2\n\n기존 본문"

	wd.mergeDreamUpdate(existing, wikiUpdate{
		Path: "기타/even-realities-g2-smart-glasses.md", retargetedFrom: "기타/데이팅앱-스코폴라민.md",
		ID: "dating-app-scopolamine-threat", Summary: "스코폴라민 범죄 경고",
		Type: "entity", Resource: "https://embassy.example/warning",
		Content: "## 데이팅 앱 기반 범죄",
	}, "", "")

	if existing.Meta.ID != "even-realities-g2" || existing.Meta.Summary != "G2 스마트 안경 — 번역·AI 탑재" {
		t.Errorf("retargeted update rewrote page identity: %+v", existing.Meta)
	}
	if existing.Meta.Type != "concept" || existing.Meta.Resource != "https://evenrealities.example/g2" {
		t.Errorf("retargeted update rewrote type/resource: %+v", existing.Meta)
	}
	if !strings.Contains(existing.Body, "데이팅 앱 기반 범죄") {
		t.Error("retargeted update must still append its content")
	}
}

// TestMergeDreamUpdate_IDIsFillOnly: id is the key similar/verify/link_prune/
// graph resolve by. A direct (non-retargeted) update may fill an empty id but
// never rewrite one — 24 rewrites in 30 days made page identity drift per
// update and fed verify's exact-id auto-merge.
func TestMergeDreamUpdate_IDIsFillOnly(t *testing.T) {
	wd := &WikiDreamer{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	stamped := NewPage("Vaultwarden", "시스템", nil)
	stamped.Meta.ID = "vaultwarden"
	wd.mergeDreamUpdate(stamped, wikiUpdate{Path: "시스템/vaultwarden.md", ID: "isw-family"}, "", "")
	if stamped.Meta.ID != "vaultwarden" {
		t.Errorf("existing id overwritten: %q", stamped.Meta.ID)
	}

	fresh := NewPage("새 페이지", "업무", nil)
	wd.mergeDreamUpdate(fresh, wikiUpdate{Path: "업무/새-페이지.md", ID: "new-page"}, "", "")
	if fresh.Meta.ID != "new-page" {
		t.Errorf("empty id not filled: %q", fresh.Meta.ID)
	}

	// A direct update still refreshes the summary — only retargets are frozen.
	direct := NewPage("금호타이어 곡성", "프로젝트", nil)
	direct.Meta.Summary = "이전 요약"
	wd.mergeDreamUpdate(direct, wikiUpdate{Path: "프로젝트/pl2-kum-epc-001/대표.md", Summary: "새 요약"}, "", "")
	if direct.Meta.Summary != "새 요약" {
		t.Errorf("direct update must refresh summary, got %q", direct.Meta.Summary)
	}
}

// TestUpdateDreamPage_EmptyContentOnMissingTargetCreatesNothing: update-on-
// missing writes the update's content verbatim, so an empty update minted
// frontmatter-only pages (three 0-byte pages live). They still enter FTS and
// the embedding set and compete for recall slots.
func TestUpdateDreamPage_EmptyContentOnMissingTargetCreatesNothing(t *testing.T) {
	s, wd := newVerifyStore(t)

	created, err := wd.updateDreamPage(wikiUpdate{
		Action: "update", Path: "기타/모닝레터-2026-08-19.md",
		Title: "모닝레터 2026-08-19", Content: "   \n",
	}, "", "")
	if err != nil {
		t.Fatalf("updateDreamPage: %v", err)
	}
	if created {
		t.Error("empty-content update reported a creation")
	}
	if p, _ := s.ReadPage("기타/모닝레터-2026-08-19.md"); p != nil {
		t.Errorf("empty page created: %+v", p.Meta)
	}

	// With content, the same path still creates.
	if _, err := wd.updateDreamPage(wikiUpdate{
		Action: "update", Path: "기타/모닝레터-2026-08-19.md",
		Title: "모닝레터 2026-08-19", Content: "## 환율\n1,414원",
	}, "", ""); err != nil {
		t.Fatalf("updateDreamPage with content: %v", err)
	}
	if p, _ := s.ReadPage("기타/모닝레터-2026-08-19.md"); p == nil || !strings.Contains(p.Body, "1,414원") {
		t.Error("update-on-missing with content did not create the page")
	}
}
