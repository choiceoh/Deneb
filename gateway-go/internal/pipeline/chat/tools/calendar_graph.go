// calendar_graph.go — work-graph queries over the calendar. Local events carry
// link fields (Source/SourceLabel/Docs, attendees), which makes each event a
// node the agent can join with mail, wiki, and deal data. The `timeline` action
// pulls every event about one entity (client, project, person, place) into a
// single chronological view, then hands the agent the threads to weave the rest
// of the context in — "현대차 관련 회의 전부" / "아산공장 타임라인" in one call.
package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// timelineWindowDays is how far back/forward a work-graph timeline reaches when
// the caller gives no explicit from/to — a project spans past commitments and
// upcoming plans, so look both ways.
const timelineWindowDays = 90

// calActionTimeline returns the events about p.Query (an entity: client,
// project, person, place) as one chronological timeline, then directs the agent
// to join the linked mail/wiki/deal context. Read-only guidance: the agent does
// the cross-domain weave with its mail_archive / wiki / dropbox tools.
func calActionTimeline(ctx context.Context, d *toolctx.CalendarDeps, p calParams) string {
	q := strings.TrimSpace(p.Query)
	if q == "" {
		return "타임라인을 만들 대상(거래처·프로젝트·인물·장소 등)을 query로 지정해줘."
	}
	loc := calDisplayLoc()
	now := time.Now().In(loc)
	from, to := now.AddDate(0, 0, -timelineWindowDays), now.AddDate(0, 0, timelineWindowDays)
	if f, t, ok := timelineExplicitRange(p); ok {
		from, to = f, t
	}

	events, warn := calMerged(ctx, d, from, to)
	ql := strings.ToLower(q)
	matched := make([]calendar.Event, 0, len(events))
	for _, e := range events {
		if eventMatchesEntity(e, ql) {
			matched = append(matched, e)
		}
	}
	if len(matched) == 0 {
		msg := fmt.Sprintf("'%s' 관련 일정을 찾지 못했습니다 (%s ~ %s). mail_archive·wiki로 직접 찾아봐도 돼.", q, calDay(from), calDay(to))
		if warn != "" {
			msg += "\n(" + warn + ")"
		}
		return msg
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "🗂️ '%s' 관련 일정 타임라인 (%s ~ %s, %d건)\n", q, calDay(from), calDay(to), len(matched))
	for i, e := range matched {
		sb.WriteString(calListRow(i+1, e))
	}
	fmt.Fprintf(&sb, "\n위는 '%s' 관련 일정만 모은 거야. 연결된 메일(「제목」·[미팅] 표시는 mail_archive로), 관련 위키(wiki/graphify), 거래 문서(📎는 dropbox)를 엮어 과거 합의 → 예정 일정 → 미결의 전체 흐름을 한 타임라인으로 정리해.", q)
	if warn != "" {
		sb.WriteString("\n(" + warn + ")")
	}
	return sb.String()
}

// timelineExplicitRange honors an explicit from/to (RFC3339) when both parse and
// to is after from; otherwise the caller's wide default window is used.
func timelineExplicitRange(p calParams) (from, to time.Time, ok bool) {
	fs, ts := strings.TrimSpace(p.From), strings.TrimSpace(p.To)
	if fs == "" || ts == "" {
		return time.Time{}, time.Time{}, false
	}
	f, err1 := time.Parse(time.RFC3339, fs)
	t, err2 := time.Parse(time.RFC3339, ts)
	if err1 != nil || err2 != nil || !t.After(f) {
		return time.Time{}, time.Time{}, false
	}
	return f, t, true
}

// eventMatchesEntity reports whether an event is about the entity (lowercased
// ql), matching its title, the linked-mail subject, location, description, and
// attendees (name or email domain) — the fields that carry an entity's identity.
func eventMatchesEntity(e calendar.Event, ql string) bool {
	if strings.Contains(strings.ToLower(calTitle(e)), ql) ||
		strings.Contains(strings.ToLower(e.SourceLabel), ql) ||
		strings.Contains(strings.ToLower(e.Location), ql) ||
		strings.Contains(strings.ToLower(e.Description), ql) {
		return true
	}
	for _, a := range e.Attendees {
		if strings.Contains(strings.ToLower(attendeeLabel(a)), ql) ||
			strings.Contains(strings.ToLower(a.Email), ql) {
			return true
		}
	}
	return false
}
