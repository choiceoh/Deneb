package gmailpoll

import "testing"

func TestParseImportance(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantTier string
		wantText string
	}{
		{"긴급 태그", "분석 본문.\n\nIMPORTANCE: 긴급", "urgent", "분석 본문."},
		{"확인 태그", "본문\nIMPORTANCE: 확인", "attention", "본문"},
		{"참고 태그 (영문 echo 포함)", "본문\nIMPORTANCE: 참고 (routine)", "routine", "본문"},
		{"영문만", "body\nIMPORTANCE: urgent", "urgent", "body"},
		{"태그 없음", "그냥 분석 본문입니다.", "", "그냥 분석 본문입니다."},
		{"미인식 값", "본문\nIMPORTANCE: 모르겠음", "", "본문"},
		{"중간 줄 태그도 제거", "앞\nIMPORTANCE: 긴급\n뒤", "urgent", "앞\n뒤"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotText, gotTier := parseImportance(tc.text)
			if gotTier != tc.wantTier {
				t.Fatalf("tier = %q, want %q", gotTier, tc.wantTier)
			}
			if gotText != tc.wantText {
				t.Fatalf("text = %q, want %q", gotText, tc.wantText)
			}
		})
	}
}
