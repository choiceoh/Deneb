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
		{"genesis-evolve-verdict", "저신뢰 스킬 개선 확인", true},
		{"proactive", "📊 모델 튜너: 최근 24시간 분석", false},
		{"proactive", "self-correction 후보 8건 상태 분석", false},
		{"proactive", "자가개선 후보 1건 — 결정이 필요해요", false},
		{"proactive", "GLM-5.2 토큰·지연 급증 및 K3 오류 상승", false},
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

func TestRewriteLegacyLogSource(t *testing.T) {
	cases := []struct {
		source, title, want string
	}{
		{"proactive", "📊 모델 튜너: 최근 24시간 분석", SourceSystemLog},
		{"proactive", "self-correction 후보 8건 상태 분석", SourceSystemLog},
		{"proactive", "자가개선 후보 1건 — 결정이 필요해요", SourceSystemLog},
		{"proactive", "GLM-5.2 토큰·지연 급증 및 K3 오류 상승", SourceSystemLog},
		{"proactive", "건창 인버터 결재 58시간 지연", SourceProactive},
		{"proactive", "임원 출근일 안내", SourceProactive},
		{"mail_report", "모델 튜너처럼 보이는 메일", SourceMailReport},
		{SourceSystemLog, "무엇이든", SourceSystemLog},
		{SourceGenesisMeta, "메타 개정 제안: x", SourceGenesisMeta},
	}
	for _, tc := range cases {
		got := rewriteLegacyLogSource(Item{Source: tc.source, Title: tc.title})
		if got.Source != tc.want {
			t.Errorf("rewrite(%q, %q) source = %q, want %q", tc.source, tc.title, got.Source, tc.want)
		}
	}
}

func TestNormalizeExistingRewritesLegacyProactiveLog(t *testing.T) {
	got := normalizeExisting(Item{
		ID:     "wf_legacy",
		Source: SourceProactive,
		Title:  "📊 모델 튜너: 최근 24시간 분석",
		Body:   "조치 항목",
	})
	if got.Source != SourceSystemLog {
		t.Fatalf("source = %q, want %q", got.Source, SourceSystemLog)
	}
}
