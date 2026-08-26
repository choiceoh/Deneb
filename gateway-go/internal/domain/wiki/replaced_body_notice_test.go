package wiki

import (
	"strings"
	"testing"
)

// Both page-writing tools (wiki write, knowledge op=record) replace a body
// outright. The notice is shared so the two cannot drift — the first version
// shipped in the wiki tool alone and the knowledge path stayed silent.
func TestReplacedBodyNoticeNamesLostSections(t *testing.T) {
	old := "## 요약\n\n영업팀 과장\n\n## 핵심 사실\n\n- 2026-03 입사\n\n## 변경 이력\n\n- 2026-03: 생성\n"
	notice := ReplacedBodyNotice(old, "## 요약\n\n영업팀 부장으로 승진\n")
	for _, heading := range []string{"핵심 사실", "변경 이력"} {
		if !strings.Contains(notice, heading) {
			t.Errorf("dropped section %q not named: %q", heading, notice)
		}
	}
	if strings.Contains(notice, "사라진 기존 섹션: 요약") {
		t.Errorf("a surviving section was reported as lost: %q", notice)
	}
}

func TestReplacedBodyNoticeReportsSectionlessReplacement(t *testing.T) {
	notice := ReplacedBodyNotice("해봄에너지 신규 담당자. 직급: 차장", "탑솔라 소속 부장.")
	if !strings.Contains(notice, "교체") {
		t.Errorf("a full replacement was not reported: %q", notice)
	}
}

// Quiet by construction — otherwise every ordinary edit warns and the notice
// stops meaning anything.
func TestReplacedBodyNoticeStaysQuietWhenNothingIsLost(t *testing.T) {
	cases := [][2]string{
		{"", "새 페이지 본문"},                                         // creation
		{"## 요약\n\n기획팀\n", "## 요약\n\n기획팀 팀장\n"},                  // same sections
		{"## 요약\n\n기획팀\n", "## 요약\n\n기획팀\n\n## 핵심 사실\n\n- 신규\n"}, // section added
		{"기존 문장", "기존 문장 그리고 덧붙인 내용"},                            // extension
		{"   ", "새 본문"}, // whitespace-only previous
	}
	for _, tc := range cases {
		if got := ReplacedBodyNotice(tc[0], tc[1]); got != "" {
			t.Errorf("ReplacedBodyNotice(%q, %q) = %q, want empty", tc[0], tc[1], got)
		}
	}
}
