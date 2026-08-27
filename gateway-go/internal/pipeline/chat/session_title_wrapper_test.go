package chat

import "testing"

// The heuristic fallback (tiny model down or output unusable) named sessions
// after the machine wrapper a native-client message opens with. These strings
// are the session names shown in the app's list.
func TestSessionTitleFallbackSkipsMachineWrapper(t *testing.T) {
	batch := "📎 첨부 파일을 저장했습니다. 분석에 필요한 파일은 원문을 열어서 읽어라: 문서·이미지(OCR)·녹음(전사)은 `read <경로>`.\n\n1. 계약서_최종.pdf (document)\n   경로: memory/2026-08/계약서_최종.pdf\n"

	cases := map[string]struct{ msg, wantNot, wantHas string }{
		"share with the operator's own question": {
			msg:     "📲 공유 맥락:\n이 계약서 독소조항 봐줘\n\n" + batch,
			wantNot: "공유 맥락",
			wantHas: "독소조항",
		},
		"work-item open": {
			msg:     "이 업무 항목을 열었어. 아래 내용을 기준으로 핵심을 짧게 요약해줘.\n\n트리나솔라 3분기 계약 진행",
			wantNot: "업무 항목을 열었어",
			wantHas: "트리나솔라",
		},
		"attachment share with no caption": {
			msg:     batch,
			wantNot: "첨부 파일을 저장했습니다",
			wantHas: "계약서_최종.pdf",
		},
	}
	for name, tc := range cases {
		got := titleSourceLine(tc.msg)
		if contains(got, tc.wantNot) {
			t.Errorf("%s: title kept the wrapper: %q", name, got)
		}
		if !contains(got, tc.wantHas) {
			t.Errorf("%s: title lost the topic (%q): %q", name, tc.wantHas, got)
		}
	}
}

// A plain typed message is unchanged — this only strips wrappers.
func TestSessionTitleFallbackLeavesPlainMessagesAlone(t *testing.T) {
	for _, msg := range []string{
		"탑솔라 계약 진행 상황 알려줘",
		"안녕",
	} {
		if got := titleSourceLine(msg); got != msg {
			t.Errorf("titleSourceLine(%q) = %q, want unchanged", msg, got)
		}
	}
}

// A message that is nothing but wrapper still yields something rather than "" —
// an empty session name is worse than a boilerplate one.
func TestSessionTitleFallbackNeverReturnsEmpty(t *testing.T) {
	if got := titleSourceLine("📎 첨부"); got == "" {
		t.Error("a wrapper-only message produced an empty title source")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
