// person_stub.go — "is this person page a stub?" — the shared predicate for
// search demotion and related-link enrichment.
package wiki

import (
	"strings"
	"unicode/utf8"
)

// personStubProseRunes is how much prose lifts a page out of stub status. One
// written sentence about who the person is — "금호타이어 곡성 담당, 계약 창구" — clears
// it; the sync template alone does not.
const personStubProseRunes = 20

// personTemplateMarkers are the lines contacts sync / the dream seed write. A
// page made only of these says nothing a name index does not already know.
var personTemplateMarkers = []string{
	"_(미기재)_",
	"_주소록에서 동기화됨_",
	"주소록 기반 자동 생성",
	"드림 사이클 반복 언급으로 자동 생성",
	"## 소속",
	"## 직책",
	"## 담당",
	"## 관계",
	"## 연락처",
	"## 변경 이력",
	"- 이메일:",
	"- 전화:",
	"- 휴대폰:",
	// Contacts-sync value lines (contacts.go writePersonSkeleton): the org and
	// title the address book already knows. Counting these as prose made 267 of
	// 280 skeletons score ≥20 runes and dodge the stub predicate — the first
	// purge cycle (2026-08-25) removed only the 12 pages bare of even these.
	"- **소속**:",
	"- **직급 · 직책**:",
	"- 회사:",
}

// isPersonStubPage reports whether a 인물 page carries no prose of its own.
// A `pid` marks a person the operator's org chart tracks deliberately, so
// those are never stubs regardless of body length.
func isPersonStubPage(relPath string, page *Page) bool {
	if page == nil || categoryFromPath(relPath) != "인물" {
		return false
	}
	if strings.TrimSpace(page.Meta.PID) != "" {
		return false
	}
	// An operator decision IS content, and the most expensive kind: it cost a
	// human a judgment call. Purging it also un-answers the question — the next
	// mention re-seeds the page without the decision, so the homonym scan asks
	// again, forever. Two carriers, neither visible to the prose scan below:
	// the ack lives in frontmatter, and a 동명이인 주의 callout is a blockquote,
	// which personProse skips as scaffolding (2026-08-25: 김용범 was purged one
	// hour after being curated).
	if len(page.Meta.IdentityReviewed) > 0 || strings.Contains(page.Body, "동명이인 주의") {
		return false
	}
	return utf8.RuneCountInString(personProse(page.Body)) < personStubProseRunes
}

// personProse strips template scaffolding — headings, contact bullets, sync
// markers, wiki links, blank lines — and returns what a human actually wrote.
func personProse(body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ">") {
			continue
		}
		if isPersonTemplateLine(trimmed) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return strings.Join(kept, " ")
}

func isPersonTemplateLine(line string) bool {
	for _, marker := range personTemplateMarkers {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}
