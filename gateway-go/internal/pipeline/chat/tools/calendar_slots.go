// calendar_slots.go — scheduling logic for the `calendar` agent tool:
// overlapping-event conflict detection and the free_slots action (working-hours
// gap search over the merged event list). Split from calendar.go (pure move).
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

// --- conflicts -----------------------------------------------------------

// detectConflicts returns title pairs of overlapping timed events. Input must be
// sorted by start (as calMerged returns), which lets the inner loop break early:
// once a later event starts at/after the current one's end, nothing further
// overlaps it. All-day and zero-start events are ignored.
func detectConflicts(events []calendar.Event) [][2]string {
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
func calActionFreeSlots(ctx context.Context, d *toolctx.CalendarDeps, p calParams) string {
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

	var sb strings.Builder
	fmt.Fprintf(&sb, "빈 시간 (%02d:00–%02d:00, %d분 이상, %s ~ %s):\n",
		dayStart, dayEnd, int(minDur.Minutes()), calDay(from), calDay(to))
	found := 0
	for day := startOfDay(from, loc); !day.After(to); day = day.AddDate(0, 0, 1) {
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
		slots := freeWithin(winStart, winEnd, busy, minDur)
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
