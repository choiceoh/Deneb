// title_lint.go — deterministic lint for the project rep-page title rule.
//
// The rule (wiki-layout.md, operator-confirmed 2026-07-21): a rep title is the
// project's NAME — events, stage words, and dates are banned because ① the
// alias goes stale the moment the event passes and ② stage vocabulary poisons
// search and gold anchors (measured: 13 unrelated mails mislabeled onto 세창스틸
// by a "재송부" title tail). Since the folder-code pivot the title is also the
// human display name everywhere (category browser, chat labels), so a dirty
// title is now a user-visible wart, not just a search hazard.
//
// Deliberately narrow: only unambiguous violations fire. Site/region tails
// ("— 비금 솔라 현장") are legitimate per the rule ("현장/지역 토큰은 보존") and
// must pass, so enforcement stays advisory — the verify cycle surfaces
// findings, a human confirms the rename (titles are search-ranking inputs).
package wiki

import (
	"regexp"
	"strings"
)

// titleTrailingStageWords are stage/event words that end a title only when the
// title is describing deal STATE, never when naming the deal. The stage: field
// is where this vocabulary belongs.
var titleTrailingStageWords = map[string]bool{
	"협의": true, "입찰": true, "검토": true, "재검토": true,
	"기회": true, "송부": true, "대기": true, "요청": true, "문의": true,
}

// titleStatusTailWords mark a "— …" dash tail as a status tail (violation)
// rather than a site tail (allowed). 검토/협상-class words in a tail describe
// where the deal stands, not what it is.
var titleStatusTailWords = []string{"현재", "현안", "재송부", "재검토", "대기", "협상", "진행", "완료"}

var (
	// Space-delimited M/D fragments ("6/29", "7.13") — dates ride into titles
	// from mail subjects. Slash-heavy technical tokens (80MW/480MWh) stay
	// word-joined so the space delimiters exclude them.
	titleDateFragment = regexp.MustCompile(`(?:^|\s)\d{1,2}[./]\d{1,2}(?:\s|$)`)
	// Four-digit years, with an optional 년 suffix captured: a 년-suffixed year
	// names a contract vintage ("2026년 모듈 공급 계약" — live corpus) and is part
	// of the name; a bare year is an event timestamp and violates.
	titleYearFragment = regexp.MustCompile(`20\d{2}년?`)
)

// LintProjectRepTitle returns human-readable violations of the rep-title rule,
// empty when the title is a clean name. Pure and deterministic.
func LintProjectRepTitle(title string) []string {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	var violations []string

	fields := strings.Fields(title)
	if last := fields[len(fields)-1]; titleTrailingStageWords[last] {
		violations = append(violations, "말단 단계어 '"+last+"' — 단계는 stage: 필드의 몫")
	}

	// Dash tails: split on the em/en-dash separators titles actually use.
	for _, sep := range []string{" — ", " – "} {
		if _, tail, ok := strings.Cut(title, sep); ok {
			for _, w := range titleStatusTailWords {
				if strings.Contains(tail, w) {
					violations = append(violations, "상태 꼬리 '"+strings.TrimSpace(tail)+"' — 상태는 summary·로그의 몫")
					break
				}
			}
		}
	}

	dated := titleDateFragment.MatchString(title)
	if !dated {
		for _, m := range titleYearFragment.FindAllString(title, -1) {
			if !strings.HasSuffix(m, "년") {
				dated = true
				break
			}
		}
	}
	if dated {
		violations = append(violations, "날짜 토큰 — 날짜는 로그 섹션의 몫")
	}
	return violations
}

// detectTitleRuleViolations lints every project rep page's title (pure
// computation over the index snapshot). Advisory only — a title edit changes
// search ranking, so the fix stays a human decision. Capped so a backlog can't
// flood one verify report.
func detectTitleRuleViolations(entries map[string]IndexEntry) []verifyFinding {
	const maxFindings = 5
	var findings []verifyFinding
	for path, e := range entries {
		if len(findings) >= maxFindings {
			break
		}
		if !IsProjectRepPage(path) {
			continue
		}
		if vs := LintProjectRepTitle(e.Title); len(vs) > 0 {
			findings = append(findings, verifyFinding{
				Type:   "title_rule",
				Detail: "대표 title 규약 위반 (" + e.Title + "): " + strings.Join(vs, "; "),
				PageA:  path,
			})
		}
	}
	return findings
}
