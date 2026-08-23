package wiki

import (
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestNewPageFromUpdate_PersistsStageAndProgram(t *testing.T) {
	page := newPageFromUpdate(wikiUpdate{
		Title:    "기아 광주",
		Category: "프로젝트",
		Stage:    "견적",
		Program:  "광주-캐노피",
	}, "pl2-kia-epc-002", "")
	if page.Meta.Stage != "견적" {
		t.Fatalf("stage=%q, want 견적", page.Meta.Stage)
	}
	if page.Meta.Program != "광주-캐노피" {
		t.Fatalf("program=%q, want 광주-캐노피", page.Meta.Program)
	}
	junk := newPageFromUpdate(wikiUpdate{Title: "x", Category: "프로젝트", Stage: "영업중"}, "", "")
	if junk.Meta.Stage != "" {
		t.Fatalf("unknown stage must drop, got %q", junk.Meta.Stage)
	}
}

func TestMergeDreamUpdate_StageOverwriteProgramFillOnly(t *testing.T) {
	wd := &WikiDreamer{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	existing := NewPage("기아 광주", "프로젝트", nil)
	existing.Meta.Stage = "견적"
	rep := "프로젝트/pl2-kia-epc-002/대표.md"
	wd.mergeDreamUpdate(existing, wikiUpdate{Path: rep, Stage: "계약협의", Program: "광주-캐노피"}, "", "")
	if existing.Meta.Stage != "계약협의" {
		t.Fatalf("stage overwrite: got %q", existing.Meta.Stage)
	}
	if existing.Meta.Program != "광주-캐노피" {
		t.Fatalf("program fill: got %q", existing.Meta.Program)
	}
	wd.mergeDreamUpdate(existing, wikiUpdate{Path: rep, Stage: "시공", Program: "다른프로그램"}, "", "")
	if existing.Meta.Stage != "시공" {
		t.Fatalf("stage must overwrite again, got %q", existing.Meta.Stage)
	}
	if existing.Meta.Program != "광주-캐노피" {
		t.Fatalf("program is fill-only, got %q", existing.Meta.Program)
	}
}

// TestMergeDreamUpdate_RepOnlyFieldsSkipNonRepPages: client/sites/kinds/stage/
// program describe a PROJECT, so they belong on its 대표페이지. A retargeted
// update once stamped them onto a child page in the same folder, which then
// reads as a second rep page to every consumer that groups by client.
func TestMergeDreamUpdate_RepOnlyFieldsSkipNonRepPages(t *testing.T) {
	wd := &WikiDreamer{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	child := NewPage("기아 AL광주 2공장 태양광 모듈 입찰", "프로젝트", nil)
	wd.mergeDreamUpdate(child, wikiUpdate{
		Path:   "프로젝트/pl2-kia-epc-002/기아-al광주-2공장-태양광-모듈-입찰.md",
		Client: "기아", Sites: flexStringList{"광주 광산구"},
		Kinds: flexStringList{"태양광/루프탑"}, Stage: "계약협의", Program: "기아-광주",
		Content: "본문 보강",
	}, "", "")

	if child.Meta.Client != "" || len(child.Meta.Sites) != 0 || len(child.Meta.Kinds) != 0 ||
		child.Meta.Stage != "" || child.Meta.Program != "" {
		t.Errorf("rep-only fields leaked onto a child page: %+v", child.Meta)
	}
	if !strings.Contains(child.Body, "본문 보강") {
		t.Error("content append must still happen on a non-rep page")
	}
}
