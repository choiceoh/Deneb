package contacts

import "testing"

func TestNormalizePersonNamePreservesIdentityMatchContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "spaces", in: "   ", want: ""},
		{name: "plain", in: "홍길동", want: "홍길동"},
		{name: "internal spaces", in: "홍 길 동", want: "홍길동"},
		{name: "title separated", in: "홍길동 부장", want: "홍길동"},
		{name: "title attached", in: "김민준부장", want: "김민준"},
		{name: "compound title", in: "홍길동 대표이사", want: "홍길동"},
		{name: "stacked suffixes", in: "홍길동대표님", want: "홍길동"},
		{name: "parenthetical company", in: "홍길동(가나다에너지)", want: "홍길동"},
		{name: "full width parenthesis", in: "홍길동（가나다에너지）", want: "홍길동"},
		{name: "bracket affiliation", in: "홍길동[가나다에너지]", want: "홍길동"},
		{name: "angle affiliation", in: "홍길동<가나다에너지>", want: "홍길동"},
		{name: "slash affiliation", in: "홍길동/가나다에너지", want: "홍길동"},
		{name: "comma affiliation", in: "홍길동, 가나다에너지", want: "홍길동"},
		{name: "middle dot affiliation", in: "홍길동·가나다에너지", want: "홍길동"},
		{name: "two rune floor", in: "김대표님", want: "김대표"},
		{name: "one rune preserved", in: "민", want: "민"},
		{name: "ascii lower", in: "ALICE KIM", want: "alicekim"},
		{name: "ascii with title", in: "Alice Kim 부장", want: "alicekim"},
		{name: "no substring conflation", in: "이수민", want: "이수민"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizePersonName(tt.in); got != tt.want {
				t.Fatalf("NormalizePersonName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
