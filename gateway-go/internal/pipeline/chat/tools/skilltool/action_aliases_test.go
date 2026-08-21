package skilltool

import "testing"

func TestNormalizeSkillManageAction(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "LIST", want: "list"},
		{in: "목록", want: "list"},
		{in: "생성", want: "create"},
		{in: "수정", want: "patch"},
		{in: "삭제", want: "delete"},
		{in: "읽기", want: "read"},
		{in: "list-files", want: "list_files"},
		{in: "파일목록", want: "list_files"},
		{in: "write-file", want: "write_file"},
		{in: "remove-file", want: "remove_file"},
		{in: "  patch  ", want: "patch"},
	}
	for _, tt := range tests {
		if got := normalizeSkillManageAction(tt.in); got != tt.want {
			t.Errorf("normalizeSkillManageAction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
