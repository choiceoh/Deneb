// person_stub.go — "is this person page a stub?" — the shared predicate for
// search demotion and related-link enrichment.
package wiki

import (
	"path/filepath"
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
	// A learned summary is knowledge, and it is the ONLY place some of it
	// lives: the dreamer writes what it worked out about a person there while
	// the body stays a contact template. Scanning only the body deleted 신정훈
	// (파인드그린 대표 · 감포 풍력 창구) and 제용범 (동하메디칼 딜 창구) as
	// "contentless" on 2026-08-25.
	if isLearnedPersonSummary(page.Meta.Summary) {
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

// PersonStubPaths lists every 인물 page that currently qualifies as a
// contentless stub, in scan order. Exported for the operator-run bulk purge:
// the dream cycle drains these 50 at a time behind a grace window, and the
// operator asked for the backlog cleared in one pass (2026-08-25). Same
// predicate as the cycle's, so the two can never disagree about what "쓸데없는"
// means — including its protection for pages carrying an operator decision.
func (s *Store) PersonStubPaths() []string {
	if s == nil {
		return nil
	}
	relPaths, err := s.ListPages("인물")
	if err != nil {
		return nil
	}
	var out []string
	for _, rp := range relPaths {
		rp = filepath.ToSlash(rp)
		page, rerr := s.ReadPage(rp)
		if rerr != nil || page == nil {
			continue
		}
		if isPersonStubPage(rp, page) {
			out = append(out, rp)
		}
	}
	return out
}

// isLearnedPersonSummary reports whether a 인물 summary says something a person
// or the dreamer worked out, as opposed to the two seeded forms
// (person_seed.go): "<org> — 주소록 기반 자동 생성" and the contact-sync
// "<org> 소속". Anything else was written because someone learned it.
func isLearnedPersonSummary(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" || strings.Contains(summary, "주소록 기반 자동 생성") {
		return false
	}
	return !strings.HasSuffix(summary, "소속")
}
