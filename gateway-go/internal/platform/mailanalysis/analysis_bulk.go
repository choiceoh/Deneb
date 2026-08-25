package mailanalysis

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// A correct analysis of a mail that is not business mail is still a page in the
// wiki, and the page costs more than it carries. The 2026-08-25 corpus audit
// found 59 of them: newsletters, webinar invites, plan-change notices, firmware
// announcements. One — a Cursor payment-failure notice whose own first line
// reads "업무 메일이 아닙니다" — was the single MOST-recalled page in the entire
// wiki (30 injections), because nothing about being uninteresting made it rank
// any lower than a contract.
//
// This gate is different in kind from AnalysisUsable, which judges whether the
// text is an analysis at all. Here the analysis is fine; its SUBJECT is not
// worth a page. That means trusting what the model concluded, which the repair
// tooling deliberately avoids elsewhere — a page's own claims are not evidence
// about that page. It is acceptable at this particular point for two reasons:
// the operator asked for it directly, and the cost of being wrong is bounded —
// the mail itself stays in the archive and the inbox, so a false positive loses
// a derived page, never a fact.

// ErrAnalysisBulkMail reports that the analysis classified its own subject as
// advertising, a newsletter, or another bulk send.
var ErrAnalysisBulkMail = errors.New("analysis classifies the mail as bulk, not business")

// bulkVerdictRe are the words an analysis uses when the mail is bulk. Taken
// verbatim from the live corpus rather than invented.
var bulkVerdictRe = regexp.MustCompile(
	`업무 *메일이 아|업무와 *무관|업무와는 *무관|업무 *무관|광고|뉴스레터|마케팅|프로모션|스팸|` +
		`자동 *발신|구독 *(안내|갱신|결제)|결제 *(실패|오류)|수신 *거부|홍보 *(메일|뉴스)`,
)

// bulkContrastRe marks a line where the verdict is being applied to OTHER mail.
// The corpus case: "오늘 메일 24건 중 광고·테스트성(…)을 제외하고, 이 건은 진코솔라 본사
// GM급이 직접 가격 협상 교착을 뚫으려는 에스컬레이션이라 …" — a GM-level price
// escalation that a bare keyword match would have thrown away.
//
// The second alternation covers the other direction — naming a bulk category
// only to disclaim it ("홍보 메일과는 무관한 정식 공문입니다"). Note it cannot be
// written as a bare "와는 무관", because "업무와는 무관" is itself a bulk verdict;
// what makes it a contrast is that the thing being disclaimed is the CATEGORY.
var bulkContrastRe = regexp.MustCompile(
	`제외|와 달리|과 달리|이 건은|이번 건은|이 메일은 다|반면|` +
		`(홍보|광고|마케팅|뉴스레터|스팸|프로모션)[^,.\n]{0,8}(와|과)는`,
)

// bulkVerdictWindow bounds how far into the opening line the verdict may sit.
// A real classification LEADS ("TLDR 테크 뉴스레터 — 업무와 무관한…", "광고 메일 —
// DeepL…"); a passing mention sits deeper in a sentence about something else.
// Counted in runes so Korean counts by character.
const bulkVerdictWindow = 60

// AnalysisNonBusiness reports why a finalized analysis should not become a wiki
// page because its subject is bulk mail, or nil when the mail is business.
//
// Only the opening line is consulted: that is where the analyst says what the
// mail IS, and confining the rule there is what keeps a later paragraph
// mentioning "광고비 정산" from condemning the page.
func AnalysisNonBusiness(text string) error {
	line := firstProseLine(analysisBody(text))
	if line == "" || bulkContrastRe.MatchString(line) {
		return nil
	}
	loc := bulkVerdictRe.FindStringIndex(line)
	if loc == nil || utf8.RuneCountInString(line[:loc[0]]) > bulkVerdictWindow {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrAnalysisBulkMail, truncateForLog(line))
}

// firstProseLine returns the first line of actual analysis — past any heading
// or provenance blockquote.
func firstProseLine(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "---") {
			continue
		}
		return t
	}
	return ""
}
