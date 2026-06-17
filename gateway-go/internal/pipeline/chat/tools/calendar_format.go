// calendar_format.go — shared helpers for the `calendar` agent tool: the
// Google+local merge, KST time/date/attendee rendering, and the ambient
// CalendarGlance summary for the system prompt. Split from calendar.go
// (pure move).
package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// --- shared helpers ------------------------------------------------------

// calMerged returns Google + local events in [from, to) sorted by start. The
// returned warn string is a non-fatal note (e.g. Google fetch failed but local
// answered) so the agent still gets the events it can.
func calMerged(ctx context.Context, d *toolctx.CalendarDeps, from, to time.Time) (merged []calendar.Event, warn string) {
	if d.Client != nil {
		client, err := d.Client()
		if err != nil {
			// Google not configured — silently degrade to local-only (this is
			// the common case before OAuth is set up).
			if d.Local == nil {
				return nil, "구글 캘린더가 연결되지 않았습니다."
			}
		} else {
			events, err := client.ListUpcoming(ctx, from, to, calMaxResults)
			if err != nil {
				warn = "구글 캘린더 조회 실패: " + err.Error()
			} else {
				merged = append(merged, events...)
			}
		}
	}
	if d.Local != nil {
		merged = append(merged, d.Local.ListRange(from, to)...)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Start.Before(merged[j].Start) })
	return merged, warn
}

// calTitle returns a non-empty display title.
func calTitle(e calendar.Event) string {
	if t := strings.TrimSpace(e.Summary); t != "" {
		return t
	}
	return "(제목 없음)"
}

// calWhen renders a compact KST time for a list row: "6/10(화) 15:00–16:00",
// or "6/10(화) 종일" for all-day events.
func calWhen(e calendar.Event) string {
	loc := calDisplayLoc()
	start := e.Start.In(loc)
	if e.AllDay {
		return fmt.Sprintf("%s 종일", calDayWeekday(start))
	}
	s := fmt.Sprintf("%s %02d:%02d", calDayWeekday(start), start.Hour(), start.Minute())
	if !e.End.IsZero() {
		end := e.End.In(loc)
		if sameDay(start, end) {
			s += fmt.Sprintf("–%02d:%02d", end.Hour(), end.Minute())
		}
	}
	return s
}

// calWhenFull renders the full KST time for the detail view.
func calWhenFull(e calendar.Event) string {
	loc := calDisplayLoc()
	start := e.Start.In(loc)
	if e.AllDay {
		return fmt.Sprintf("%s (종일)", calDayWeekday(start))
	}
	s := fmt.Sprintf("%s %02d:%02d", calDayWeekday(start), start.Hour(), start.Minute())
	if !e.End.IsZero() {
		end := e.End.In(loc)
		if sameDay(start, end) {
			s += fmt.Sprintf(" – %02d:%02d", end.Hour(), end.Minute())
		} else {
			s += fmt.Sprintf(" – %s %02d:%02d", calDayWeekday(end), end.Hour(), end.Minute())
		}
	}
	return s
}

// calDay renders a bare KST date "6/10(화)" from an absolute time.
func calDay(t time.Time) string { return calDayWeekday(t.In(calDisplayLoc())) }

func calDayWeekday(t time.Time) string {
	return fmt.Sprintf("%d/%d(%s)", int(t.Month()), t.Day(), weekdayKorean(t.Weekday()))
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func weekdayKorean(w time.Weekday) string {
	switch w {
	case time.Sunday:
		return "일"
	case time.Monday:
		return "월"
	case time.Tuesday:
		return "화"
	case time.Wednesday:
		return "수"
	case time.Thursday:
		return "목"
	case time.Friday:
		return "금"
	default:
		return "토"
	}
}

// rsvpKorean maps a Google responseStatus to a Korean label.
func rsvpKorean(status string) string {
	switch status {
	case "accepted":
		return "수락"
	case "declined":
		return "거절"
	case "tentative":
		return "미정"
	case "needsAction":
		return "대기"
	default:
		return "응답 없음"
	}
}

// attendeeLabel returns a display name (or email) for an attendee, "" if both empty.
func attendeeLabel(a calendar.Attendee) string {
	if n := strings.TrimSpace(a.DisplayName); n != "" {
		return n
	}
	return strings.TrimSpace(a.Email)
}

// calKindLabel maps a Deneb event Kind to a Korean label, "" for none/unknown.
func calKindLabel(kind string) string {
	switch strings.TrimSpace(kind) {
	case "meeting":
		return "미팅"
	case "deadline":
		return "기한"
	default:
		return ""
	}
}

// calSourceLine renders the Deneb origin of a generated event — its kind, the
// mail it came from, and the machine link the agent can follow back (mail:<id>)
// — so the agent can brief and prep over it. "" for a plain manual or Google
// event that carries no Deneb annotation.
func calSourceLine(e calendar.Event) string {
	src := strings.TrimSpace(e.Source)
	if src == "" && calKindLabel(e.Kind) == "" {
		return ""
	}
	var parts []string
	if k := calKindLabel(e.Kind); k != "" {
		parts = append(parts, k)
	}
	if lbl := strings.TrimSpace(e.SourceLabel); lbl != "" {
		parts = append(parts, fmt.Sprintf("메일 「%s」", lbl))
	}
	if src != "" {
		parts = append(parts, src)
	}
	return "연결: " + strings.Join(parts, " · ")
}

// calLinkBadge renders a compact inline annotation of an event's Deneb origin
// (kind + the mail it came from) for list/brief rows, "" when it carries none.
func calLinkBadge(e calendar.Event) string {
	kind := calKindLabel(e.Kind)
	lbl := strings.TrimSpace(e.SourceLabel)
	switch {
	case kind != "" && lbl != "":
		return fmt.Sprintf(" · [%s] 「%s」", kind, lbl)
	case kind != "":
		return " · [" + kind + "]"
	case lbl != "":
		return " · 「" + lbl + "」"
	default:
		return ""
	}
}

// countExternalAttendees counts non-self, non-declined attendees — the people
// the user is actually meeting, for the list badge.
func countExternalAttendees(attendees []calendar.Attendee) int {
	n := 0
	for _, a := range attendees {
		if a.Self || a.ResponseStatus == "declined" {
			continue
		}
		if attendeeLabel(a) == "" {
			continue
		}
		n++
	}
	return n
}

// calGlanceMax bounds how many events the ambient glance lists.
const calGlanceMax = 8

// CalendarGlance renders a compact, ambient upcoming-events summary for the
// system prompt's dynamic block: events in [now, now+days), top calGlanceMax,
// with relative day labels (오늘/내일/요일). Returns "" when there are no events
// or no calendar source — the caller then injects no section. `now` carries the
// display location (KST); callers pass time.Now().In(loc).
func CalendarGlance(ctx context.Context, d *toolctx.CalendarDeps, now time.Time, days int) string {
	if d == nil || (d.Client == nil && d.Local == nil) {
		return ""
	}
	if days <= 0 {
		days = 3
	}
	events, _ := calMerged(ctx, d, now, now.Add(time.Duration(days)*24*time.Hour))
	if len(events) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, e := range events {
		if i >= calGlanceMax {
			fmt.Fprintf(&sb, "- …외 %d건\n", len(events)-calGlanceMax)
			break
		}
		fmt.Fprintf(&sb, "- %s %s", glanceWhen(e, now), calTitle(e))
		if locName := strings.TrimSpace(e.Location); locName != "" {
			fmt.Fprintf(&sb, " · 📍%s", locName)
		}
		if e.Conference != nil && e.Conference.URI != "" {
			sb.WriteString(" · 🎥Meet")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// glanceWhen renders an event's time with a relative day label for the glance.
func glanceWhen(e calendar.Event, now time.Time) string {
	loc := now.Location()
	start := e.Start.In(loc)
	day := relativeDay(start, now)
	if e.AllDay {
		return day + " 종일"
	}
	return fmt.Sprintf("%s %02d:%02d", day, start.Hour(), start.Minute())
}

// relativeDay labels a date relative to now: 오늘/내일 for the same/next calendar
// day, otherwise the weekday-tagged date "6/11(목)".
func relativeDay(t, now time.Time) string {
	switch dayDiff(now, t) {
	case 0:
		return fmt.Sprintf("오늘(%d/%d)", int(t.Month()), t.Day())
	case 1:
		return fmt.Sprintf("내일(%d/%d)", int(t.Month()), t.Day())
	default:
		return calDayWeekday(t)
	}
}

// dayDiff returns the number of calendar days from now to t (both compared in
// now's location). KST has no DST so midnight-truncated subtraction is exact.
func dayDiff(now, t time.Time) int {
	loc := now.Location()
	y1, m1, d1 := now.In(loc).Date()
	y2, m2, d2 := t.In(loc).Date()
	a := time.Date(y1, m1, d1, 0, 0, 0, 0, loc)
	b := time.Date(y2, m2, d2, 0, 0, 0, 0, loc)
	return int(b.Sub(a).Hours()) / 24
}
