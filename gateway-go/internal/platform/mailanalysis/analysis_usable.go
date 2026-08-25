package mailanalysis

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// A finalized analysis is written to the wiki verbatim (buildMailAnalysisPage
// appends res.Text under a provenance blockquote). When the synthesis call
// degrades, what lands on the page is not a short analysis — it is not an
// analysis at all: the model's prefatory "지금부터 위키를 갱신할게요" announcement, a
// bare heading, or the delivery layer's own timeout string. Those pages then
// feed recall, so the failure is not contained to one page.
//
// sanitizeAnalysisLeak already removes leaked <think>/<tool_call> MARKUP. It
// cannot help here: these bodies carry no markup at all, so they pass every
// stripper and get persisted whole. The corpus audit (2026-08-25) found 15 such
// pages, 13 of them created after the leak sanitizer shipped — this is the
// live gap, not the markup one.
//
// AnalysisUsable is the write-time gate for that class. It is deliberately
// conservative: a real report must never be rejected, so every rule below
// requires a positive narration/error signal rather than inferring failure
// from brevity. A legitimate one-line dismissal ("TLDR 뉴스레터 — 업무와 무관")
// talks about the MAIL; a degraded body talks about the assistant's own next
// action. That distinction, not length, is what these rules key on.

var (
	// ErrAnalysisEmpty is returned when nothing survives footer/markup stripping.
	ErrAnalysisEmpty = errors.New("analysis is empty")
	// ErrAnalysisErrorText is returned when the body is a delivery-layer error
	// string rather than an analysis.
	ErrAnalysisErrorText = errors.New("analysis body is an error message")
	// ErrAnalysisNarration is returned when the body is only the model's
	// prefatory announcement of work it never went on to do.
	ErrAnalysisNarration = errors.New("analysis body is process narration, not a report")
)

// Pipeline-appended trailers. These are added AFTER synthesis (attachment note,
// auto-extracted wiki suggestions), so they must not count toward the analysis
// body — otherwise a two-line narration stub with a long attachment footer
// reads as substantial. Both markers start their own line.
var trailerRe = regexp.MustCompile(`(?m)^\s*(📎|📝)`)

// Delivery-layer failure strings that reached the wiki as analysis text. Exact
// substrings, not heuristics: each was found verbatim on a page in the corpus.
var analysisErrorStrings = []string{
	"응답 생성이 시간 초과로 중단",
	"응답 생성에 실패했",
	"The request was rejected because it was considered high risk",
}

// Prefatory narration: the model states it is ABOUT TO work on the wiki or the
// analysis, instead of delivering one. Requires both an artifact word (위키/로그/
// 맥락/분석/대표 페이지) and a forward-looking verb, so a report that merely
// mentions "위키에 기록해뒀다" in past tense at the end is not matched.
var narrationRe = regexp.MustCompile(
	`(위키|로그|대표\s*페이지|맥락|분석)[^.\n]{0,40}` +
		`(기록|갱신|반영|정리|보강|업데이트|작성|확인)[^.\n]{0,20}` +
		`(하겠|할게|해둘게|드릴게|하고 보고|한 뒤 보고|합니다\s*$)`,
)

// Completion-of-precondition announcements: "맥락 확인 완료", "맥락이 확보됐습니다".
// On their own these say only that the model finished reading — never a finding.
var preconditionRe = regexp.MustCompile(`(맥락|컨텍스트)(이|을)?\s*(확인|확보|파악)\s*(이)?\s*(완료|됐|되었|끝)`)

// Tool-failure chatter that leaked in place of the report ("edit 도구가 워크스페이스
// 밖 경로를 못 잡네요. 쉘로 직접 반영합니다.").
var toolChatterRe = regexp.MustCompile(`(edit|shell|쉘|도구|워크스페이스)[^.\n]{0,30}(못\s|실패|안\s?되|잡네)`)

// First-person intent to go inspect source material — "실제 견적 내용을 열어볼게요",
// "첨부를 확인해보겠습니다". Keyed on the volitional ending (…볼게요/…보겠습니다), which
// is what separates it from the imperative the attachment footer uses toward the
// reader ("원본을 열어 확인하세요").
var inspectIntentRe = regexp.MustCompile(`(열어|확인해|살펴|읽어|들여다)\s*(보|볼)(겠|게요|게$|까요)`)

// narrationBodyLimit bounds the narration rules to short bodies. A long report
// that happens to open with "맥락 확인 완료" still carries its analysis after it;
// only a body that never gets past the announcement stays under this.
const narrationBodyLimit = 220

// analysisBody strips pipeline trailers and markup so the rules see only what
// the synthesis model actually produced as the report.
func analysisBody(text string) string {
	s := sanitizeAnalysisLeak(text)
	if loc := trailerRe.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}
	return strings.TrimSpace(s)
}

// headingOnly reports whether the body carries no prose at all — every line is
// a markdown heading, rule, or blank ("### 분석 결과" and nothing under it).
func headingOnly(body string) bool {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "---") {
			continue
		}
		return false
	}
	return true
}

// AnalysisUsable reports why a finalized mail analysis must not be persisted to
// the wiki, or nil when the body is a usable report. Callers skip the page
// write and mark the message for re-analysis rather than storing the result.
func AnalysisUsable(text string) error {
	body := analysisBody(text)
	if body == "" || headingOnly(body) {
		return ErrAnalysisEmpty
	}
	for _, s := range analysisErrorStrings {
		if strings.Contains(body, s) {
			return fmt.Errorf("%w: %q", ErrAnalysisErrorText, s)
		}
	}
	if len([]rune(body)) <= narrationBodyLimit {
		switch {
		case narrationRe.MatchString(body):
			return fmt.Errorf("%w: %q", ErrAnalysisNarration, truncateForLog(body))
		case preconditionRe.MatchString(body):
			return fmt.Errorf("%w: %q", ErrAnalysisNarration, truncateForLog(body))
		case toolChatterRe.MatchString(body):
			return fmt.Errorf("%w: %q", ErrAnalysisNarration, truncateForLog(body))
		case inspectIntentRe.MatchString(body):
			return fmt.Errorf("%w: %q", ErrAnalysisNarration, truncateForLog(body))
		}
	}
	return nil
}

func truncateForLog(s string) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= 60 {
		return string(r)
	}
	return string(r[:60]) + "…"
}
