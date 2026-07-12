package contacts

import "strings"

// personTitleSuffixes are Korean honorific and role tokens stripped from the
// tail of a display name. Longest titles lead so compounds are removed whole.
var personTitleSuffixes = []string{
	"대표이사",
	"부사장", "본부장", "부회장",
	"부장", "차장", "과장", "대리", "사원", "주임", "팀장", "실장",
	"이사", "상무", "전무", "사장", "회장", "선임", "책임", "수석", "대표",
	"님", "씨", "군", "양",
}

// NormalizePersonName reduces a display name to the shared address-book match
// key. It removes affiliation suffixes and whitespace, peels trailing Korean
// titles without shrinking below two runes, and lowercases ASCII. Exact key
// matching keeps short names such as "이수" distinct from "이수민".
func NormalizePersonName(name string) string {
	normalized := strings.TrimSpace(name)
	if normalized == "" {
		return ""
	}
	for _, separator := range []string{"(", "（", "[", "<", "/", ",", "·"} {
		if index := strings.Index(normalized, separator); index >= 0 {
			normalized = normalized[:index]
		}
	}
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.TrimSpace(normalized)
	for {
		stripped := false
		for _, suffix := range personTitleSuffixes {
			if !strings.HasSuffix(normalized, suffix) {
				continue
			}
			candidate := strings.TrimSuffix(normalized, suffix)
			if len([]rune(candidate)) < 2 {
				continue
			}
			normalized = candidate
			stripped = true
			break
		}
		if !stripped {
			break
		}
	}
	return strings.ToLower(normalized)
}
