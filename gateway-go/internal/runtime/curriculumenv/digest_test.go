package curriculumenv

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// TestMain pins the calendar source to empty by default so the feed/wiki tests
// stay deterministic (the real localcal.Default() would otherwise leak the
// host's calendar into the digest). Calendar-specific tests override it.
func TestMain(m *testing.M) {
	upcomingCalEvents = func(time.Time, time.Time) []calendar.Event { return nil }
	os.Exit(m.Run())
}

// withEvents overrides the calendar source for one test and restores it.
func withEvents(t *testing.T, fn func(from, to time.Time) []calendar.Event) {
	t.Helper()
	prev := upcomingCalEvents
	upcomingCalEvents = fn
	t.Cleanup(func() { upcomingCalEvents = prev })
}

// fakeFeed is an in-memory FeedLister so the digest test needs no real store.
type fakeFeed struct {
	items []workfeed.Item
	err   error
}

func (f fakeFeed) List(int, bool) ([]workfeed.Item, int, error) {
	return f.items, len(f.items), f.err
}

// fakeWiki is an in-memory WikiDomainSource.
type fakeWiki map[string]struct{}

func (f fakeWiki) ActiveCounterpartyDomains(string) map[string]struct{} { return f }

// fakeFailed is an in-memory AgentLogSource capturing the window it got.
type fakeFailed struct {
	reqs    []agentlog.FailedRequest
	grinds  []agentlog.HighEffortRun
	sinceMs *int64
}

func (f fakeFailed) FailedUserRequests(sinceMs int64, _ int) []agentlog.FailedRequest {
	if f.sinceMs != nil {
		*f.sinceMs = sinceMs
	}
	return f.reqs
}

func (f fakeFailed) HighEffortUserRuns(int64, int, int) []agentlog.HighEffortRun {
	return f.grinds
}

// truncRunes caps at n runes with an ellipsis (Korean runes count as 1 each).
func TestTruncRunesTruncatesToRuneLimit(t *testing.T) {
	if got := truncRunes("abc", 5); got != "abc" {
		t.Fatalf("short string should be unchanged: got %q", got)
	}
	got := truncRunes("abcdef", 4)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 4 {
		t.Fatalf("truncRunes(6,4) = %q, want 4 runes ending …", got)
	}
	got = truncRunes("한글테스트데이터", 5)
	if len([]rune(got)) != 5 {
		t.Fatalf("Korean truncRunes = %q, want 5 runes", got)
	}
}

// The digest surfaces recent feed-item titles, skipping blank ones.
func TestDigestFeedItemsIgnoresBlankTitles(t *testing.T) {
	feed := fakeFeed{items: []workfeed.Item{
		{Title: "계약 검토 — NDA 초안"}, {Title: "  "}, {Title: "주간 보고서 작성"},
	}}
	got := Digest(Sources{Feed: feed})
	if !strings.Contains(got, "계약 검토") || !strings.Contains(got, "주간 보고서") {
		t.Fatalf("digest missing a feed title:\n%s", got)
	}
	if !strings.Contains(got, "업무 피드") {
		t.Fatalf("digest missing section header:\n%s", got)
	}
	if n := strings.Count(got, "\n- "); n != 2 {
		t.Fatalf("expected 2 titled bullets (blank dropped), got %d:\n%s", n, got)
	}
}

// The digest surfaces active wiki domains, sorted, under the section header.
func TestDigestWikiDomainsFormatsSortedJoinedList(t *testing.T) {
	got := Digest(Sources{Wiki: fakeWiki{"acme.com": {}, "bohae.co.kr": {}}})
	if !strings.Contains(got, "위키 상대 도메인") {
		t.Fatalf("digest missing wiki section:\n%s", got)
	}
	// Sorted deterministically.
	if !strings.Contains(got, "acme.com · bohae.co.kr") {
		t.Fatalf("wiki domains not sorted/joined as expected:\n%s", got)
	}
}

// A feed read error drops the feed section without failing the whole digest.
func TestDigest_FeedErrorDropsSection(t *testing.T) {
	got := Digest(Sources{Feed: fakeFeed{err: errBoom}, Wiki: fakeWiki{"x.com": {}}})
	if strings.Contains(got, "업무 피드") {
		t.Fatalf("feed error should drop the feed section:\n%s", got)
	}
	if !strings.Contains(got, "위키 상대 도메인") {
		t.Fatalf("a feed error must not suppress the wiki section:\n%s", got)
	}
}

// No sources wired (dev/test) → empty digest, quiet.
func TestDigest_Empty(t *testing.T) {
	if got := Digest(Sources{}); got != "" {
		t.Fatalf("empty sources should yield empty digest, got %q", got)
	}
}

// The digest surfaces recent failed user requests — quoted so the curriculum
// grounding gate can bind a proposal to the actual ask — and honors the
// 14d window; an empty source stays silent.
func TestDigest_FailedRequests(t *testing.T) {
	var gotSince int64
	src := Sources{
		AgentLog: fakeFailed{
			reqs: []agentlog.FailedRequest{
				{Message: "위키에서 발주서 초안 뽑아줘", Error: "stream stall", Ts: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC).UnixMilli()},
				{Message: "지난달 매출 정리해줘", Ts: time.Date(2026, 7, 9, 9, 0, 0, 0, time.UTC).UnixMilli()},
			},
			sinceMs: &gotSince,
		},
		Now: func() time.Time { return time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) },
	}
	got := Digest(src)
	if !strings.Contains(got, "실패한 요청") {
		t.Fatalf("digest missing failed-request section:\n%s", got)
	}
	if !strings.Contains(got, `"위키에서 발주서 초안 뽑아줘" — 오류: stream stall`) {
		t.Fatalf("failed request must be quoted with its error:\n%s", got)
	}
	if !strings.Contains(got, `"지난달 매출 정리해줘"`) || strings.Contains(got, "정리해줘\" — 오류") {
		t.Fatalf("error-less failure must render without an error suffix:\n%s", got)
	}
	want := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC).UnixMilli() // Now - 14d
	if gotSince != want {
		t.Fatalf("failed-request window = %d, want %d (Now - 14d)", gotSince, want)
	}

	if got := Digest(Sources{AgentLog: fakeFailed{}}); got != "" {
		t.Fatalf("no failures should keep the digest silent, got %q", got)
	}
}

// The digest surfaces completed-but-grinding runs (implicit demand): quoted
// request, effort stats, top-tool histogram, and the skill-consult marker.
// An empty source stays silent.
func TestDigest_HighEffortRuns(t *testing.T) {
	src := Sources{
		AgentLog: fakeFailed{grinds: []agentlog.HighEffortRun{
			{Message: "경쟁사 케이블 단가 비교해줘", ToolCalls: 14, Turns: 5, TopTools: "wiki×6 · exec×4", Ts: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC).UnixMilli()},
			{Message: "주간 발주 현황 정리", ToolCalls: 9, Turns: 3, UsedSkill: true, Ts: time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC).UnixMilli()},
		}},
		Now: func() time.Time { return time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) },
	}
	got := Digest(src)
	if !strings.Contains(got, "고비용 완주 런") {
		t.Fatalf("digest missing high-effort section:\n%s", got)
	}
	if !strings.Contains(got, `"경쟁사 케이블 단가 비교해줘" — 도구 14회/5턴, 주도구 wiki×6 · exec×4`) {
		t.Fatalf("high-effort run must carry quote, effort stats, and top tools:\n%s", got)
	}
	if !strings.Contains(got, `"주간 발주 현황 정리" — 도구 9회/3턴 (스킬 참조함)`) {
		t.Fatalf("skill-consulted run must carry the marker without a top-tools suffix:\n%s", got)
	}

	if got := Digest(Sources{AgentLog: fakeFailed{}}); got != "" {
		t.Fatalf("no grinding runs should keep the digest silent, got %q", got)
	}
}

// M6: the failed-request section — the strongest demand evidence — must render
// BEFORE feed/domain/calendar so a downstream rune-budget truncation cannot
// silently starve it.
func TestDigest_FailedRequestsRenderFirst(t *testing.T) {
	src := Sources{
		AgentLog: fakeFailed{
			reqs: []agentlog.FailedRequest{
				{Message: "실패한 능력 요청", Error: "no capability", Ts: time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC).UnixMilli()},
			},
			grinds: []agentlog.HighEffortRun{
				{Message: "갈아서 해결한 요청", ToolCalls: 11, Turns: 4, Ts: time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC).UnixMilli()},
			},
		},
		Feed: fakeFeed{items: []workfeed.Item{{Title: "피드 항목 하나"}}},
		Wiki: fakeWiki{"acme.com": {}},
		Now:  func() time.Time { return time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) },
	}
	got := Digest(src)
	failedAt := strings.Index(got, "실패한 요청")
	grindAt := strings.Index(got, "고비용 완주 런")
	feedAt := strings.Index(got, "업무 피드")
	domainAt := strings.Index(got, "위키 상대 도메인")
	if failedAt < 0 || grindAt < 0 || feedAt < 0 || domainAt < 0 {
		t.Fatalf("expected all four sections present:\n%s", got)
	}
	if !(failedAt < grindAt && grindAt < feedAt && failedAt < domainAt) {
		t.Fatalf("demand sections must lead (failed=%d grind=%d feed=%d domain=%d):\n%s", failedAt, grindAt, feedAt, domainAt, got)
	}
}

// The injected clock feeds the wiki cutoff (a fixed Now keeps the test
// deterministic and proves Now is honored).
func TestDigestWikiCutoffWithInjectedClock(t *testing.T) {
	var gotCutoff string
	src := Sources{
		Wiki: cutoffCapture{&gotCutoff},
		Now:  func() time.Time { return time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) },
	}
	Digest(src)
	if gotCutoff != "2026-07-06" { // 2026-07-13 minus the 7d window
		t.Fatalf("cutoff = %q, want 2026-07-06 (Now - 7d)", gotCutoff)
	}
}

type cutoffCapture struct{ got *string }

func (c cutoffCapture) ActiveCounterpartyDomains(cutoff string) map[string]struct{} {
	*c.got = cutoff
	return nil
}

var errBoom = errors.New("boom")

// The digest surfaces upcoming business-calendar commitments (P5-1 forward
// demand), skipping untitled holds.
func TestDigestUpcomingCalendarIgnoresUntitledHolds(t *testing.T) {
	withEvents(t, func(from, to time.Time) []calendar.Event {
		return []calendar.Event{
			{Summary: "ACME 계약 협상 미팅", Start: from.Add(24 * time.Hour)},
			{Summary: "", Start: from.Add(48 * time.Hour)}, // untitled — dropped
			{Summary: "분기 실적 발표 준비", Start: from.Add(72 * time.Hour)},
		}
	})
	got := Digest(Sources{})
	if !strings.Contains(got, "다가오는 일정") {
		t.Fatalf("digest missing upcoming-calendar section:\n%s", got)
	}
	if !strings.Contains(got, "ACME 계약 협상 미팅") || !strings.Contains(got, "분기 실적 발표 준비") {
		t.Fatalf("digest missing a titled commitment:\n%s", got)
	}
	if n := strings.Count(got, "\n- "); n != 2 {
		t.Fatalf("expected 2 event bullets (untitled dropped), got %d:\n%s", n, got)
	}
}

// A calendar window with only untitled holds carries no demand → no section.
func TestDigestOnlyUntitledCalendarYieldsEmptyDigest(t *testing.T) {
	withEvents(t, func(from, to time.Time) []calendar.Event {
		return []calendar.Event{{Summary: "   ", Start: from.Add(time.Hour)}}
	})
	if got := Digest(Sources{}); got != "" {
		t.Fatalf("untitled-only calendar should yield empty digest, got %q", got)
	}
}
