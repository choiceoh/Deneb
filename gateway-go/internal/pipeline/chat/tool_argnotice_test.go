package chat

import (
	"strings"
	"testing"
)

// A dropped argument that changes what the tool did must not read as plain
// success. Live case 2026-08-26: a wiki write with an invented "path" key was
// re-slugged to another page and reported "위키 페이지 생성" with no hint.
func TestUnknownArgNoticeNamesTheKeysAndKeepsTheOutput(t *testing.T) {
	out := appendUnknownArgNotice("위키 페이지 생성: 사용자/사용자-현행-사실.md", "wiki", []string{"path"})

	if !strings.Contains(out, "위키 페이지 생성") {
		t.Fatalf("tool output must survive: %q", out)
	}
	if !strings.Contains(out, "path") || !strings.Contains(out, "wiki") {
		t.Fatalf("notice must name the key and the tool: %q", out)
	}
}

func TestUnknownArgNoticeIsAbsentForCleanCalls(t *testing.T) {
	const output = "8/26(수) 일정 없음"
	if got := appendUnknownArgNotice(output, "calendar", nil); got != output {
		t.Fatalf("a clean call must be byte-identical: %q", got)
	}
}

func TestUnknownArgNoticeStandsAloneForEmptyOutput(t *testing.T) {
	got := appendUnknownArgNotice("", "calendar", []string{"range"})
	if !strings.HasPrefix(got, "[무시된 인자") {
		t.Fatalf("empty output still carries the notice: %q", got)
	}
}
