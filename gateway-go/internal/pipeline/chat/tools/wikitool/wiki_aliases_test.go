package wikitool

import "testing"

func TestNormalizeWikiAction(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "SEARCH", want: "search"},
		{in: "검색", want: "search"},
		{in: "찾기", want: "search"},
		{in: "읽기", want: "read"},
		{in: "목록", want: "index"},
		{in: "목차", want: "index"},
		{in: "쓰기", want: "write"},
		{in: "write_site", want: "write-site"},
		{in: "현장쓰기", want: "write-site"},
		{in: "seed_sites", want: "seed-sites"},
		{in: "로그", want: "log"},
		{in: "일기", want: "daily"},
		{in: "상태", want: "status"},
		{in: "닫기", want: "close"},
		{in: "재개", want: "reopen"},
		{in: "수집", want: "ingest"},
		{in: "기록", want: "기록"},
		{in: "  read  ", want: "read"},
	}
	for _, tt := range tests {
		if got := normalizeWikiAction(tt.in); got != tt.want {
			t.Errorf("normalizeWikiAction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
