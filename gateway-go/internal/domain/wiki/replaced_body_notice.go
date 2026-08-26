package wiki

import (
	"fmt"
	"strings"
)

// ReplacedBodyNotice reports what a full-body page write destroyed, or "" when
// nothing was lost.
//
// Two tools replace a wiki page body: the wiki tool's write action and
// knowledge(op="record"). Both said only that the page was written, so
// overwriting a person page with a contradictory one read exactly like
// extending it. The notice lives here, in the domain, so the two paths cannot
// drift apart — the first version shipped in the wiki tool alone and the
// knowledge path kept the silent overwrite.
//
// Quiet by construction: it fires only when content is actually gone. A write
// that keeps every H2 section, or that extends the previous text, says nothing,
// so an ordinary edit does not train the reader to ignore the warning.
func ReplacedBodyNotice(oldBody, newBody string) string {
	if strings.TrimSpace(oldBody) == "" {
		return ""
	}
	if strings.Contains(newBody, strings.TrimSpace(oldBody)) {
		return "" // the write extended the previous text rather than replacing it
	}
	_, oldSections := (&Page{Body: oldBody}).SplitByH2()
	_, newSections := (&Page{Body: newBody}).SplitByH2()
	kept := make(map[string]struct{}, len(newSections))
	for _, section := range newSections {
		kept[section.Heading] = struct{}{}
	}
	var lost []string
	for _, section := range oldSections {
		if _, ok := kept[section.Heading]; !ok {
			lost = append(lost, section.Heading)
		}
	}
	if len(lost) > 0 {
		return fmt.Sprintf(
			"\n⚠️ 이 쓰기로 사라진 기존 섹션: %s — 본문을 통째로 교체한다(append 아님). "+
				"남겨야 할 내용이면 그 섹션까지 포함해 다시 써라.",
			strings.Join(lost, ", "),
		)
	}
	if len(oldSections) == 0 {
		return fmt.Sprintf(
			"\n⚠️ 기존 본문 %d자를 새 내용으로 교체했다 (append 아님). 이전 내용이 필요하면 그 부분까지 포함해 다시 써라.",
			len([]rune(strings.TrimSpace(oldBody))),
		)
	}
	return ""
}
