package recallops

import "testing"

func TestNormalizeKnowledgeOp(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "RECALL", want: "recall"},
		{in: "search", want: "recall"},
		{in: "검색", want: "recall"},
		{in: "query", want: "recall"},
		{in: "읽기", want: "read"},
		{in: "fetch", want: "read"},
		{in: "write", want: "record"},
		{in: "저장", want: "record"},
		{in: "  read  ", want: "read"},
	}
	for _, tt := range tests {
		if got := normalizeKnowledgeOp(tt.in); got != tt.want {
			t.Errorf("normalizeKnowledgeOp(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizePolarisAction(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "SEARCH", want: "search"},
		{in: "검색", want: "search"},
		{in: "설명", want: "describe"},
		{in: "요약", want: "describe"},
		{in: "펼치기", want: "expand"},
		{in: "원문", want: "expand"},
		{in: "  describe  ", want: "describe"},
	}
	for _, tt := range tests {
		if got := normalizePolarisAction(tt.in); got != tt.want {
			t.Errorf("normalizePolarisAction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
