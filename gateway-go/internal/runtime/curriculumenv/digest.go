// Package curriculumenv assembles the curriculum lane's environment demand
// digest (RSI P5-1). Extracted from runtime/server so the composition root
// keeps only the wiring: this package owns the digest FORMAT and the source
// orchestration (fetch, nil-tolerance, cap, sort), and server passes its stores
// in through the narrow interfaces below. Future demand sources (e.g. upcoming
// calendar commitments) land here, not in the server package.
//
// The curriculum producer reads this digest to widen demand mining beyond
// tracker-local evidence: it sees what the operator is ACTUALLY working on
// (recent feed items), the wiki domains in play, upcoming commitments, and
// requests that FAILED outright, so proposed capabilities target real
// environment gaps, not catalog-internal rearrangement. Genesis stays a leaf
// — the closure server injects owns all workfeed/wiki/agentlog knowledge.
package curriculumenv

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

// domainWindow bounds how far back the wiki-domain lookback reaches;
// forwardWindow bounds how far ahead upcoming commitments are read.
// failedWindow bounds the failed-request lookback — failures are scarce at
// single-operator cadence (a handful per month), so the window is wider than
// the 7d domain lookback, same rationale as genesis' organic-label window.
const (
	domainWindow  = 7 * 24 * time.Hour
	forwardWindow = 14 * 24 * time.Hour
	failedWindow  = 14 * 24 * time.Hour
	// grindWindow bounds the high-effort-run lookback — like failures, heavy
	// completed runs are scarce at single-operator cadence.
	grindWindow = 14 * 24 * time.Hour
)

// feedCap / domainCap / eventCap / failedCap / grindCap bound each section so
// the digest stays compact.
const (
	feedCap   = 20
	domainCap = 15
	eventCap  = 15
	failedCap = 8
	grindCap  = 6
	// grindMinToolCalls is the effort floor for the high-effort section: below
	// it a run is ordinary tool use, not grinding worth a purpose-built skill.
	grindMinToolCalls = 8
)

// upcomingCalEvents returns business-calendar events overlapping [from, to).
// It reads the process-wide local calendar directly (localcal is a singleton,
// unlike the server-held feed/wiki stores) — kept here so the calendar demand
// source needs no composition-root change. A package var so tests inject a
// fixture; nil-tolerant.
var upcomingCalEvents = func(from, to time.Time) []calendar.Event {
	store, err := localcal.Default()
	if err != nil || store == nil {
		return nil
	}
	return store.ListRange(from, to)
}

// FeedLister is the narrow slice of the workfeed store the digest needs.
// *workfeed.Store satisfies it structurally.
type FeedLister interface {
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
}

// WikiDomainSource is the narrow slice of the wiki store the digest needs.
// *wiki.Store satisfies it structurally.
type WikiDomainSource interface {
	ActiveCounterpartyDomains(cutoff string) map[string]struct{}
}

// AgentLogSource is the narrow slice of the agent behavioral log the digest
// needs: explicit capability gaps (failed requests) and implicit ones
// (completed-but-grinding runs — the SearchOS host-miner half of the demand
// signal). *agentlog.Writer satisfies it structurally.
type AgentLogSource interface {
	FailedUserRequests(sinceMs int64, limit int) []agentlog.FailedRequest
	HighEffortUserRuns(sinceMs int64, minToolCalls, limit int) []agentlog.HighEffortRun
}

// Sources are the injected environment stores the digest reads (feed + wiki +
// agent behavioral log). Every field is nil-tolerant (early/late binding): a
// nil source drops its section. Now defaults to time.Now when unset. Note the
// upcoming-calendar section is NOT injected here — it reads the process-wide
// localcal singleton via upcomingCalEvents — so an all-nil Sources can still
// yield a non-empty digest when the operator has upcoming commitments.
type Sources struct {
	Feed     FeedLister
	Wiki     WikiDomainSource
	AgentLog AgentLogSource
	Now      func() time.Time
}

// Digest formats a compact environment summary: recent feed-item titles (active
// work shape) + active wiki counterparty domains (environment breadth). Empty
// sections are omitted; a fully empty digest returns "".
func Digest(s Sources) string {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	var b strings.Builder

	// Explicit failed demand FIRST (RSI code eval M6): this is the strongest
	// demand evidence — the operator already asked in their own words and the
	// agent could not complete it — and the consumer
	// (curriculum.assembleDemandEvidence) truncates the digest to a rune
	// budget. Rendering it last meant a busy feed+calendar silently starved it.
	writeFailedRequestsSection(&b, s.AgentLog, now)
	writeHighEffortSection(&b, s.AgentLog, now)
	writeFeedSection(&b, s.Feed)
	writeWikiDomainsSection(&b, s.Wiki, now)
	writeUpcomingEventsSection(&b, now)

	return strings.TrimSpace(b.String())
}

// writeFailedRequestsSection renders real user requests that errored mid-run,
// with the request text so the 12-rune verbatim-quote grounding gate can bind a
// proposal to the actual ask. Live-test synthetic sessions are excluded at the
// source (agentlog.FailedUserRequests).
func writeFailedRequestsSection(b *strings.Builder, agentLog AgentLogSource, now func() time.Time) {
	if agentLog == nil {
		return
	}
	failed := agentLog.FailedUserRequests(now().Add(-failedWindow).UnixMilli(), failedCap)
	if len(failed) == 0 {
		return
	}
	fmt.Fprintf(b, "최근 실패한 요청(명시적 능력 갭, 최대 %d):\n", failedCap)
	for _, r := range failed {
		line := fmt.Sprintf("- %s: \"%s\"", time.UnixMilli(r.Ts).Format("01-02"), truncRunes(r.Message, 80))
		if e := strings.TrimSpace(r.Error); e != "" {
			line += " — 오류: " + truncRunes(e, 60)
		}
		b.WriteString(line + "\n")
	}
}

// writeHighEffortSection renders completed real-user runs that ground through
// an unusually high tool-call count — the implicit demand signal (the agent
// managed, but the hard way; a recurring shape at this cost is a skill
// candidate). Quoted request text lets the 12-rune verbatim-quote grounding
// gate bind a proposal to the actual ask, same as the failed section.
func writeHighEffortSection(b *strings.Builder, agentLog AgentLogSource, now func() time.Time) {
	if agentLog == nil {
		return
	}
	runs := agentLog.HighEffortUserRuns(now().Add(-grindWindow).UnixMilli(), grindMinToolCalls, grindCap)
	if len(runs) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "고비용 완주 런(도구 %d회 이상 갈아서 해결한 실제 요청 — 유사 요청이 반복되면 스킬 후보, 최대 %d):\n",
		grindMinToolCalls, grindCap)
	for _, r := range runs {
		line := fmt.Sprintf("- %s: \"%s\" — 도구 %d회/%d턴",
			time.UnixMilli(r.Ts).Format("01-02"), truncRunes(r.Message, 80), r.ToolCalls, r.Turns)
		if r.TopTools != "" {
			line += ", 주도구 " + r.TopTools
		}
		if r.UsedSkill {
			line += " (스킬 참조함)"
		}
		b.WriteString(line + "\n")
	}
}

// writeFeedSection renders active work: recent feed items (titles only — the
// producer needs the shape of what's happening, not detail).
func writeFeedSection(b *strings.Builder, feed FeedLister) {
	if feed == nil {
		return
	}
	items, _, err := feed.List(feedCap, false)
	if err != nil || len(items) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "최근 업무 피드(최대 %d):\n", feedCap)
	for _, item := range items {
		if title := strings.TrimSpace(item.Title); title != "" {
			fmt.Fprintf(b, "- %s\n", truncRunes(title, 80))
		}
	}
}

// writeWikiDomainsSection renders environment breadth: active wiki counterparty
// domains (who the operator is engaging with).
func writeWikiDomainsSection(b *strings.Builder, wiki WikiDomainSource, now func() time.Time) {
	if wiki == nil {
		return
	}
	cutoff := now().Add(-domainWindow).Format("2006-01-02")
	domains := wiki.ActiveCounterpartyDomains(cutoff)
	if len(domains) == 0 {
		return
	}
	sorted := make([]string, 0, len(domains))
	for d := range domains {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)
	if len(sorted) > domainCap {
		sorted = sorted[:domainCap]
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "활성 위키 상대 도메인(최대 %d): %s\n", domainCap, strings.Join(sorted, " · "))
}

// writeUpcomingEventsSection renders forward-looking demand: upcoming
// business-calendar commitments (skill-coverage gaps vs the calendar, RSI
// P5-1). The producer infers which capabilities imminent commitments will
// need; the curriculum lane's 12-rune verbatim-quote grounding gate keeps a
// proposal tied to a real event summary, so this is demand grounding, not free
// invention. Untitled holds carry no demand signal and are skipped.
func writeUpcomingEventsSection(b *strings.Builder, now func() time.Time) {
	from := now()
	var events []string
	for _, ev := range upcomingCalEvents(from, from.Add(forwardWindow)) {
		summary := strings.TrimSpace(ev.Summary)
		if summary == "" {
			continue
		}
		events = append(events, fmt.Sprintf("- %s: %s", ev.Start.Format("01-02"), truncRunes(summary, 80)))
		if len(events) >= eventCap {
			break
		}
	}
	if len(events) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	fmt.Fprintf(b, "다가오는 일정(스킬 커버리지 갭 후보, 최대 %d):\n", eventCap)
	b.WriteString(strings.Join(events, "\n"))
	b.WriteString("\n")
}

// truncRunes caps a string to n runes with an ellipsis.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
