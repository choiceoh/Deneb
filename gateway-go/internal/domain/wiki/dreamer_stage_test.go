package wiki

import (
	"io"
	"log/slog"
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
	wd.mergeDreamUpdate(existing, wikiUpdate{Stage: "계약협의", Program: "광주-캐노피"}, "", "")
	if existing.Meta.Stage != "계약협의" {
		t.Fatalf("stage overwrite: got %q", existing.Meta.Stage)
	}
	if existing.Meta.Program != "광주-캐노피" {
		t.Fatalf("program fill: got %q", existing.Meta.Program)
	}
	wd.mergeDreamUpdate(existing, wikiUpdate{Stage: "시공", Program: "다른프로그램"}, "", "")
	if existing.Meta.Stage != "시공" {
		t.Fatalf("stage must overwrite again, got %q", existing.Meta.Stage)
	}
	if existing.Meta.Program != "광주-캐노피" {
		t.Fatalf("program is fill-only, got %q", existing.Meta.Program)
	}
}
