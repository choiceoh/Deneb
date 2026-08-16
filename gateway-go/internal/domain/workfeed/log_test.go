package workfeed

import "testing"

func TestIsLogCard(t *testing.T) {
	cases := []struct {
		source, title string
		want          bool
	}{
		{"genesis-meta", "메타 개정 제안: evolve-system-prompt.md", true},
		{"genesis-ladder", "졸업 사다리", true},
		{"system_log", "무엇이든", true},
		{"genesis-other", "x", true},
		{"proactive", "📊 모델 튜너: 최근 24시간 분석", true},
		{"proactive", "self-correction 후보 8건 상태 분석", true},
		{"proactive", "자가개선 후보 1건 — 결정이 필요해요", true},
		{"proactive", "GLM-5.2 토큰·지연 급증 및 K3 오류 상승", true},
		{"proactive", "건창 인버터 결재 58시간 지연", false},
		{"mail_report", "JOCA 견적 회신", false},
		{"groupware-approval", "모듈대금 지출건", false},
		{"proactive", "임원 출근일 안내", false},
	}
	for _, tc := range cases {
		if got := IsLogCard(tc.source, tc.title); got != tc.want {
			t.Errorf("IsLogCard(%q, %q) = %v, want %v", tc.source, tc.title, got, tc.want)
		}
	}
}
