package schedule

import "testing"

func TestNormalizeCronAction(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "LIST", want: "list"},
		{in: "목록", want: "list"},
		{in: "추가", want: "add"},
		{in: "create", want: "add"},
		{in: "수정", want: "update"},
		{in: "삭제", want: "remove"},
		{in: "delete", want: "remove"},
		{in: "실행", want: "run"},
		{in: "조회", want: "get"},
		{in: "이력", want: "runs"},
		{in: "깨우기", want: "wake"},
		{in: "상태", want: "status"},
		{in: "  add  ", want: "add"},
	}
	for _, tt := range tests {
		if got := normalizeCronAction(tt.in); got != tt.want {
			t.Errorf("normalizeCronAction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeCalendarAction(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "", want: ""},
		{in: "LIST", want: "list"},
		{in: "목록", want: "list"},
		{in: "조회", want: "get"},
		{in: "생성", want: "create"},
		{in: "추가", want: "create"},
		{in: "수정", want: "update"},
		{in: "삭제", want: "delete"},
		{in: "free-slots", want: "free_slots"},
		{in: "빈시간", want: "free_slots"},
		{in: "브리프", want: "brief"},
		{in: "준비", want: "prep"},
		{in: "캡처", want: "capture"},
		{in: "점검", want: "audit"},
		{in: "타임라인", want: "timeline"},
	}
	for _, tt := range tests {
		if got := normalizeCalendarAction(tt.in); got != tt.want {
			t.Errorf("normalizeCalendarAction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
