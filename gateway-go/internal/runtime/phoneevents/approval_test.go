package phoneevents

import (
	"log/slog"
	"testing"
)

func TestExtractGroupwareDocID(t *testing.T) {
	t.Parallel()
	body := "[그룹웨어 전자결재 · 미결문서]\n조회: 다과비\n\n제목: 영광 신하리\nid: 99178\n\n본문\nok"
	if got := extractGroupwareDocID(body); got != "99178" {
		t.Fatalf("id: line: got %q", got)
	}
	if got := extractGroupwareDocID("문서ID: 99179"); got != "99179" {
		t.Fatalf("structured id: got %q", got)
	}
	if got := extractGroupwareDocID("1. 제목 · id=99178 · 기안"); got != "99178" {
		t.Fatalf("id= inline: got %q", got)
	}
	if got := extractGroupwareDocID("no id here"); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

func TestIngestAsyncDefersElectronicApprovalWhenRadarOwns(t *testing.T) {
	h := New(Config{
		DeferElectronicApproval: true,
		Logger:                  slog.New(slog.DiscardHandler),
	})
	// Must return without panicking on nil chat/relay — defer short-circuits first.
	h.IngestAsync("notification", "groupware", "종류: 전자결재\n제목: 품의\n문서ID: 7")
}
