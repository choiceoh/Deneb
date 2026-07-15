package phoneevents

import "testing"

func TestExtractGroupwareDocID(t *testing.T) {
	t.Parallel()
	body := "[그룹웨어 전자결재 · 미결문서]\n조회: 다과비\n\n제목: 영광 신하리\nid: 99178\n\n본문\nok"
	if got := extractGroupwareDocID(body); got != "99178" {
		t.Fatalf("id: line: got %q", got)
	}
	if got := extractGroupwareDocID("1. 제목 · id=99178 · 기안"); got != "99178" {
		t.Fatalf("id= inline: got %q", got)
	}
	if got := extractGroupwareDocID("no id here"); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}
