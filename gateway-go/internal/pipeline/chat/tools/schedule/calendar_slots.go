// calendar_slots.go — scheduling logic for the `calendar` agent tool:
// overlapping-event conflict detection and the free_slots action (working-hours
// gap search over the merged event list). Split from calendar.go (pure move).
package schedule

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// --- conflicts -----------------------------------------------------------

// detectConflicts returns title pairs of overlapping timed events. Input must be
// sorted by start (as calMerged returns), which lets the inner loop break early:
// once a later event starts at/after the current one's end, nothing further
// overlaps it. All-day and zero-start events are ignored.
func detectConflicts(events []tooldeps.CalendarEvent) [][2]string {
	var out [][2]string
	for i := range events {
		a := events[i]
		if a.AllDay || a.Start.IsZero() {
			continue
		}
		aEnd := a.End
		if aEnd.IsZero() || !aEnd.After(a.Start) {
			aEnd = a.Start.Add(time.Hour)
		}
		for j := i + 1; j < len(events); j++ {
			b := events[j]
			if b.AllDay || b.Start.IsZero() {
				continue
			}
			if !b.Start.Before(aEnd) {
				break // sorted: no later event overlaps a
			}
			out = append(out, [2]string{calTitle(a), calTitle(b)})
		}
	}
	return out
}

// --- free slots ----------------------------------------------------------

// interval is a half-open time span [start, end).
type interval struct{ start, end time.Time }

// calActionFreeSlots finds free gaps within working hours across a date range —
// the "어디에 미팅 넣지?" answer. Pure logic over the merged event list.
func calActionFreeSlots(ctx context.Context, d *tooldeps.CalendarDeps, p calParams) string {
	loc := calDisplayLoc()
	now := time.Now().In(loc)

	from, to, errMsg := freeSlotsRange(p, now)
	if errMsg != "" {
		return errMsg
	}
	dayStart, dayEnd := freeSlotsHours(p)
	minDur := time.Duration(p.DurationMin) * time.Minute
	if minDur <= 0 {
		minDur = 30 * time.Minute
	}

	events, warn := calMerged(ctx, d, from, to)
	var busy []interval
	for _, e := range events {
		if e.AllDay || e.Start.IsZero() {
			continue
		}
		end := e.End
		if end.IsZero() || !end.After(e.Start) {
			end = e.Start.Add(time.Hour)
		}
		busy = append(busy, interval{e.Start.In(loc), end.In(loc)})
	}

	// The lunch carve-out only applies when the working window strictly
	// contains 12–13 — keep the header honest for narrow windows.
	lunchCarved := dayStart < 12 && dayEnd > 13
	var excl []string
	if !p.IncludeWeekends {
		excl = append(excl, "주말")
	}
	if lunchCarved {
		excl = append(excl, "점심(12–13시)")
	}
	exclusions := "제외 없음"
	if len(excl) > 0 {
		exclusions = strings.Join(excl, "·") + " 제외"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "빈 시간 (%02d:00–%02d:00, %d분 이상, %s, %s ~ %s):\n",
		dayStart, dayEnd, int(minDur.Minutes()), exclusions, calDay(from), calDay(to))
	found := 0
	for day := startOfDay(from, loc); !day.After(to); day = day.AddDate(0, 0, 1) {
		// Weekends are not bookable business time — skip unless opted in.
		if !p.IncludeWeekends && (day.Weekday() == time.Saturday || day.Weekday() == time.Sunday) {
			continue
		}
		winStart := time.Date(day.Year(), day.Month(), day.Day(), dayStart, 0, 0, 0, loc)
		winEnd := time.Date(day.Year(), day.Month(), day.Day(), dayEnd, 0, 0, 0, loc)
		if winStart.Before(from) {
			winStart = from
		}
		if winStart.Before(now) {
			winStart = now // don't suggest past slots today
		}
		if winEnd.After(to) {
			winEnd = to
		}
		if !winEnd.After(winStart) {
			continue
		}
		// Carve out the lunch hour when the working hours strictly contain
		// it (same condition the header reports) — a deliberately narrow
		// window (e.g. day_start=12, day_end=13) stays untouched so an
		// explicit lunch search still works. freeWithin clips to the window.
		dayBusy := busy
		if lunchCarved {
			lunchStart := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc)
			dayBusy = append(append([]interval(nil), busy...), interval{lunchStart, lunchStart.Add(time.Hour)})
		}
		slots := freeWithin(winStart, winEnd, dayBusy, minDur)
		if len(slots) == 0 {
			continue
		}
		parts := make([]string, 0, len(slots))
		for _, s := range slots {
			parts = append(parts, fmt.Sprintf("%02d:%02d–%02d:%02d",
				s.start.Hour(), s.start.Minute(), s.end.Hour(), s.end.Minute()))
			found++
		}
		fmt.Fprintf(&sb, "%s: %s\n", calDayWeekday(winStart), strings.Join(parts, ", "))
	}
	if found == 0 {
		msg := "해당 기간 근무시간 내 빈 시간이 없습니다."
		if warn != "" {
			msg += "\n(" + warn + ")"
		}
		return msg
	}
	if warn != "" {
		sb.WriteString("(" + warn + ")\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// freeSlotsRange resolves the search window: explicit from/to, else hours_ahead,
// else the next 7 days from now.
func freeSlotsRange(p calParams, now time.Time) (from, to time.Time, errMsg string) {
	if strings.TrimSpace(p.From) != "" || strings.TrimSpace(p.To) != "" {
		var err error
		from, err = time.Parse(time.RFC3339, strings.TrimSpace(p.From))
		if err != nil {
			return time.Time{}, time.Time{}, "from은 RFC3339 형식이어야 합니다 (예: 2026-06-10T00:00:00+09:00)."
		}
		to, err = time.Parse(time.RFC3339, strings.TrimSpace(p.To))
		if err != nil {
			return time.Time{}, time.Time{}, "to는 RFC3339 형식이어야 합니다."
		}
		if !to.After(from) {
			return time.Time{}, time.Time{}, "to는 from보다 뒤여야 합니다."
		}
		return from, to, ""
	}
	if p.HoursAhead > 0 {
		h := p.HoursAhead
		if h > calMaxHoursAhead {
			h = calMaxHoursAhead
		}
		return now, now.Add(time.Duration(h) * time.Hour), ""
	}
	return now, now.AddDate(0, 0, 7), ""
}

// freeSlotsHours returns the working-hours [start, end) hours, applying defaults
// (09:00–18:00). day_start at midnight is treated as "unset" → 9.
func freeSlotsHours(p calParams) (start, end int) {
	ds := p.DayStart
	if ds <= 0 || ds >= 24 {
		ds = 9
	}
	de := p.DayEnd
	if de <= 0 || de > 24 {
		de = 18
	}
	if de <= ds {
		de = ds + 1
	}
	return ds, de
}

// startOfDay truncates t to midnight in loc.
func startOfDay(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// freeWithin returns the gaps in [winStart, winEnd) not covered by busy
// intervals, each at least minDur long. busy may span outside the window; it is
// clipped, sorted, and merged first.
func freeWithin(winStart, winEnd time.Time, busy []interval, minDur time.Duration) []interval {
	var bs []interval
	for _, b := range busy {
		s, e := b.start, b.end
		if e.After(winStart) && s.Before(winEnd) {
			if s.Before(winStart) {
				s = winStart
			}
			if e.After(winEnd) {
				e = winEnd
			}
			bs = append(bs, interval{s, e})
		}
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].start.Before(bs[j].start) })

	var merged []interval
	for _, b := range bs {
		if len(merged) > 0 && !b.start.After(merged[len(merged)-1].end) {
			if b.end.After(merged[len(merged)-1].end) {
				merged[len(merged)-1].end = b.end
			}
			continue
		}
		merged = append(merged, b)
	}

	var gaps []interval
	cur := winStart
	for _, b := range merged {
		if b.start.Sub(cur) >= minDur {
			gaps = append(gaps, interval{cur, b.start})
		}
		if b.end.After(cur) {
			cur = b.end
		}
	}
	if winEnd.Sub(cur) >= minDur {
		gaps = append(gaps, interval{cur, winEnd})
	}
	return gaps
}

// --- schedule audit (time protection) -----------------------------------

const (
	auditBufferGap     = 10 * time.Minute // gap below this between meetings = no buffer (back-to-back)
	auditBackToBackRun = 3                // consecutive back-to-back meetings worth flagging
	auditOverloadCount = 5                // meetings in a day that mark it overloaded
	auditOverloadHours = 5 * time.Hour    // meeting hours in a day that mark it overloaded
	auditFocusMin      = 60 * time.Minute // a usable focus block is at least this long
)

// calActionAudit reviews the schedule for double-bookings, overloaded days, and
// back-to-back runs with no buffer, then points at free blocks to protect as
// focus time — the "time protection" pass. Pure analysis returned as guidance;
// the agent presents it and offers to create the protective blocks (and, for a
// delegating executive, to send a 담당자 to delegable meetings instead). Pull-only,
// so it adds no proactive notification.
func calActionAudit(ctx context.Context, d *tooldeps.CalendarDeps, p calParams) string {
	loc := calDisplayLoc()
	now := time.Now().In(loc)
	from, to, errMsg := freeSlotsRange(p, now)
	if errMsg != "" {
		return errMsg
	}
	dayStart, dayEnd := freeSlotsHours(p)
	events, warn := calMerged(ctx, d, from, to)

	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 일정 점검 (%s ~ %s)\n", calDay(from), calDay(to))

	conflicts := detectConflicts(events)
	if len(conflicts) > 0 {
		sb.WriteString("\n⚠️ 겹침(더블부킹):\n")
		for _, c := range conflicts {
			fmt.Fprintf(&sb, "  • %s ↔ %s\n", c[0], c[1])
		}
	}

	audit := evaluateScheduleAudit(events, from, to, now, loc, dayStart, dayEnd)

	if len(audit.overloads) > 0 {
		sb.WriteString("\n🔴 과부하: " + strings.Join(audit.overloads, " · ") + "\n")
	}
	if len(audit.runs) > 0 {
		sb.WriteString("⏱️ 연속 회의: " + strings.Join(audit.runs, " · ") + "\n")
	}
	if len(audit.noFocus) > 0 {
		sb.WriteString("🚫 포커스 시간 없음: " + strings.Join(audit.noFocus, " · ") + "\n")
	}
	if len(audit.suggestions) > 0 {
		sb.WriteString("\n💡 보호 제안:\n")
		for _, s := range audit.suggestions {
			fmt.Fprintf(&sb, "  • %s\n", s)
		}
	}

	if len(conflicts) == 0 && len(audit.overloads) == 0 && len(audit.runs) == 0 {
		if warn != "" {
			// The primary (Google) calendar fetch had a problem, so we only saw
			// local events — don't certify the whole schedule clean.
			sb.WriteString("\n로컬 일정 기준으로는 더블부킹·과부하가 없지만, " + warn + " — 전체 일정을 확인하지 못했습니다.")
		} else {
			sb.WriteString("\n일정 양호 — 더블부킹·과부하·버퍼 부족 없음.")
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	sb.WriteString("\n위 보호 제안을 calendar(action=\"create\")로 잡아줄지 사용자에게 제안해. 사용자는 위임하는 임원이니, 과부하 날의 회의 중 본인이 꼭 가야 할 것과 담당자·팀에 위임 가능한 것을 구분해 대리 참석도 함께 권해.")
	if warn != "" {
		sb.WriteString("\n(" + warn + ")")
	}
	return strings.TrimRight(sb.String(), "\n")
}

type scheduleAudit struct {
	overloads   []string
	runs        []string
	noFocus     []string
	suggestions []string
}

func evaluateScheduleAudit(
	events []tooldeps.CalendarEvent,
	from, to, now time.Time,
	loc *time.Location,
	dayStart, dayEnd int,
) scheduleAudit {
	var result scheduleAudit
	for day := startOfDay(from, loc); !day.After(to); day = day.AddDate(0, 0, 1) {
		dayResult, ok := evaluateScheduleDay(events, day, from, to, now, loc, dayStart, dayEnd)
		if !ok {
			continue
		}
		if dayResult.overload != "" {
			result.overloads = append(result.overloads, dayResult.overload)
		}
		if dayResult.run != "" {
			result.runs = append(result.runs, dayResult.run)
		}
		if dayResult.noFocus != "" {
			result.noFocus = append(result.noFocus, dayResult.noFocus)
		}
		if dayResult.suggestion != "" {
			result.suggestions = append(result.suggestions, dayResult.suggestion)
		}
	}
	return result
}

type scheduleDayAudit struct {
	overload   string
	run        string
	noFocus    string
	suggestion string
}

func evaluateScheduleDay(
	events []tooldeps.CalendarEvent,
	day, from, to, now time.Time,
	loc *time.Location,
	dayStart, dayEnd int,
) (scheduleDayAudit, bool) {
	dayEvents := timedEventsOn(events, day, loc)
	if len(dayEvents) == 0 {
		return scheduleDayAudit{}, false
	}
	lo, hi := boundedAuditDay(day, from, to, loc)
	inWindow, busy, total := clipAuditEvents(dayEvents, lo, hi, loc)
	if len(inWindow) == 0 {
		return scheduleDayAudit{}, false
	}

	label := calDayWeekday(day)
	overloaded := len(inWindow) >= auditOverloadCount || total >= auditOverloadHours
	result := scheduleDayAudit{}
	if overloaded {
		result.overload = fmt.Sprintf("%s 회의 %d건·%s", label, len(inWindow), shortDur(total))
		result.noFocus, result.suggestion = auditFocusOpportunity(
			day, from, to, now, loc, dayStart, dayEnd, busy, label,
		)
	}
	if run := longestBackToBack(inWindow); run >= auditBackToBackRun {
		result.run = fmt.Sprintf("%s %d연속(버퍼 없음)", label, run)
	}
	return result, true
}

func boundedAuditDay(day, from, to time.Time, loc *time.Location) (time.Time, time.Time) {
	lo := startOfDay(day, loc)
	hi := lo.AddDate(0, 0, 1)
	if from.After(lo) {
		lo = from
	}
	if to.Before(hi) {
		hi = to
	}
	return lo, hi
}

func clipAuditEvents(
	events []tooldeps.CalendarEvent,
	lo, hi time.Time,
	loc *time.Location,
) ([]tooldeps.CalendarEvent, []interval, time.Duration) {
	inWindow := make([]tooldeps.CalendarEvent, 0, len(events))
	busy := make([]interval, 0, len(events))
	var total time.Duration
	for _, event := range events {
		start := event.Start.In(loc)
		if start.Before(lo) {
			start = lo
		}
		end := eventEnd(event).In(loc)
		if end.After(hi) {
			end = hi
		}
		if !end.After(start) {
			continue
		}
		inWindow = append(inWindow, event)
		busy = append(busy, interval{start: start, end: end})
		total += end.Sub(start)
	}
	return inWindow, busy, total
}

func auditFocusOpportunity(
	day, from, to, now time.Time,
	loc *time.Location,
	dayStart, dayEnd int,
	busy []interval,
	label string,
) (noFocus, suggestion string) {
	windowStart := time.Date(day.Year(), day.Month(), day.Day(), dayStart, 0, 0, 0, loc)
	windowEnd := time.Date(day.Year(), day.Month(), day.Day(), dayEnd, 0, 0, 0, loc)
	if windowStart.Before(from) {
		windowStart = from
	}
	if windowEnd.After(to) {
		windowEnd = to
	}
	if windowStart.Before(now) {
		windowStart = now
	}
	if !windowEnd.After(windowStart) {
		return label, ""
	}
	focus := freeWithin(windowStart, windowEnd, busy, auditFocusMin)
	if len(focus) == 0 {
		return label, ""
	}
	first := focus[0]
	return "", fmt.Sprintf("%s %02d:%02d–%02d:%02d 포커스 블록 확보",
		label, first.start.Hour(), first.start.Minute(), first.end.Hour(), first.end.Minute())
}

// eventEnd returns an event's end, defaulting to start+1h when missing/invalid —
// the same convention detectConflicts and free_slots use.
func eventEnd(e tooldeps.CalendarEvent) time.Time {
	end := e.End
	if end.IsZero() || !end.After(e.Start) {
		end = e.Start.Add(time.Hour)
	}
	return end
}

// timedEventsOn returns the timed events that OVERLAP day (local), in the input
// order (calMerged is already start-sorted). Overlap rather than start-date
// equality, so a meeting/travel block running in from the previous day still
// counts toward the day's load and blocks its focus time. All-day markers are
// excluded.
func timedEventsOn(events []tooldeps.CalendarEvent, day time.Time, loc *time.Location) []tooldeps.CalendarEvent {
	dayStart := startOfDay(day, loc)
	dayEnd := dayStart.AddDate(0, 0, 1)
	var out []tooldeps.CalendarEvent
	for _, e := range events {
		if e.AllDay || e.Start.IsZero() {
			continue
		}
		if e.Start.In(loc).Before(dayEnd) && eventEnd(e).In(loc).After(dayStart) {
			out = append(out, e)
		}
	}
	return out
}

// longestBackToBack returns the longest run of consecutive meetings whose
// inter-meeting gap is below the buffer threshold. Input must be start-sorted.
func longestBackToBack(dayTimed []tooldeps.CalendarEvent) int {
	if len(dayTimed) == 0 {
		return 0
	}
	maxRun, run := 1, 1
	for i := 1; i < len(dayTimed); i++ {
		if dayTimed[i].Start.Sub(eventEnd(dayTimed[i-1])) < auditBufferGap {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 1
		}
	}
	return maxRun
}

// shortDur renders a meeting-load duration: "5.5시간" / "3시간" / "90분".
func shortDur(d time.Duration) string {
	if d >= time.Hour {
		s := fmt.Sprintf("%.1f시간", d.Hours())
		return strings.Replace(s, ".0시간", "시간", 1)
	}
	return fmt.Sprintf("%d분", int(d.Minutes()))
}

// --- write-path conflict notice ------------------------------------------

// calEventSpan returns the event's effective [start, end), applying the same
// one-hour default detectConflicts uses for an open-ended event.
func calEventSpan(e tooldeps.CalendarEvent) (time.Time, time.Time) {
	end := e.End
	if end.IsZero() || !end.After(e.Start) {
		end = e.Start.Add(time.Hour)
	}
	return e.Start, end
}

// calConflictNotice reports the events an added or rescheduled event now
// collides with.
//
// list, brief and audit all treat a double-booking as something the operator
// must see — list marks it ⚠️, audit exists to hunt it down. The write path
// said nothing, so booking a meeting straight onto an occupied slot produced a
// clean success line and the model had no signal to mention the clash. This
// does not block the write: the operator asked for the event, and the local
// store already holds it. It only makes the result honest about what happened.
func calConflictNotice(ctx context.Context, d *tooldeps.CalendarDeps, ev tooldeps.CalendarEvent) string {
	if d == nil || ev.AllDay || ev.Start.IsZero() {
		return ""
	}
	start, end := calEventSpan(ev)
	// Widen the query by an hour on each side so a neighbour that starts before
	// this event and runs into it is still in the window.
	events, _ := calMerged(ctx, d, start.Add(-time.Hour), end.Add(time.Hour))
	var clashes []string
	for _, other := range events {
		if other.ID == ev.ID || other.AllDay || other.Start.IsZero() {
			continue
		}
		otherStart, otherEnd := calEventSpan(other)
		if otherStart.Before(end) && start.Before(otherEnd) {
			clashes = append(clashes, fmt.Sprintf("%s (%s)", calTitle(other), calWhen(other)))
		}
	}
	if len(clashes) == 0 {
		return ""
	}
	return "\n⚠️ 이 시간에 이미 있는 일정: " + strings.Join(clashes, ", ") +
		"\n사용자에게 겹침을 알리고 옮길지 물어라 (calendar action=\"free_slots\"로 빈 시간을 제안할 수 있다)."
}
