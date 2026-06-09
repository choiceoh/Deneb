// calendar.go — the `calendar` agent tool: lets the chat agent read and write
// the user's calendar during a conversation. This is the agent's "비서 손" for
// 일정 — the miniapp.calendar.* RPC surface answers the native UI, this answers
// the LLM tool-calling loop.
//
// Hybrid model (identical to handlerminiapp/calendar.go, deliberately reused via
// the same platform layers so the two surfaces can never diverge):
//   - reads MERGE the read-only Google client with the local store, sorted by start
//   - writes ALWAYS land in the local store (create/update/delete), so they work
//     without a Google OAuth write scope; Google events are read-only and the tool
//     refuses to edit/delete them with a clear Korean message.
//
// Local events carry a "local:" ID prefix so get/update/delete route to the store.
// All times are displayed in KST (single-user KST deployment is the product
// contract); the agent supplies RFC3339 instants (it knows "now" + tz from the
// system prompt's baked timestamp).
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

const (
	calDefaultHoursAhead = 48
	calMaxHoursAhead     = 24 * 14 // 2 weeks
	calMaxResults        = 100
	// calKSTFallbackOffset matches calendar_briefing.go: a fixed +09:00 when
	// the Asia/Seoul zoneinfo is missing (stripped container) so the wall clock
	// stays correct instead of silently flipping to UTC.
	calKSTFallbackOffset = 9 * 60 * 60
)

// calLoc is the cached display location (KST). Loaded once; LoadLocation is a
// non-trivial lookup we don't want to repeat on every event format.
var (
	calLocOnce sync.Once
	calLoc     *time.Location
)

func calDisplayLoc() *time.Location {
	calLocOnce.Do(func() {
		loc, err := time.LoadLocation("Asia/Seoul")
		if err != nil {
			loc = time.FixedZone("KST", calKSTFallbackOffset)
		}
		calLoc = loc
	})
	return calLoc
}

// calParams is the union of all fields across the calendar tool's actions.
type calParams struct {
	Action      string `json:"action"`
	ID          string `json:"id"`
	From        string `json:"from"`
	To          string `json:"to"`
	HoursAhead  int    `json:"hours_ahead"`
	Summary     string `json:"summary"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Location    string `json:"location"`
	Description string `json:"description"`
	AllDay      bool   `json:"all_day"`
	DurationMin int    `json:"duration_min"`
	DayStart    int    `json:"day_start"`
	DayEnd      int    `json:"day_end"`
}

// ToolCalendar returns the calendar tool. A nil deps (neither Google nor local
// wired) is guarded at registration time, so here at least one source exists.
func ToolCalendar(d *toolctx.CalendarDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p calParams
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		switch strings.TrimSpace(p.Action) {
		case "list", "":
			return calActionList(ctx, d, p), nil
		case "get":
			return calActionGet(ctx, d, p), nil
		case "create":
			return calActionCreate(d, p), nil
		case "update":
			return calActionUpdate(d, p), nil
		case "delete":
			return calActionDelete(d, p), nil
		case "free_slots":
			return calActionFreeSlots(ctx, d, p), nil
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: list(일정 조회), get(상세), create(추가), update(수정), delete(삭제), free_slots(빈 시간 찾기)", p.Action), nil
		}
	}
}

// --- list ----------------------------------------------------------------

func calActionList(ctx context.Context, d *toolctx.CalendarDeps, p calParams) string {
	now := time.Now()
	var from, to time.Time

	// Explicit [from, to) window takes priority; otherwise "now + N hours".
	if strings.TrimSpace(p.From) != "" || strings.TrimSpace(p.To) != "" {
		var err error
		from, err = time.Parse(time.RFC3339, strings.TrimSpace(p.From))
		if err != nil {
			return "from은 RFC3339 형식이어야 합니다 (예: 2026-06-10T00:00:00+09:00)."
		}
		to, err = time.Parse(time.RFC3339, strings.TrimSpace(p.To))
		if err != nil {
			return "to는 RFC3339 형식이어야 합니다 (예: 2026-06-17T00:00:00+09:00)."
		}
		if !to.After(from) {
			return "to는 from보다 뒤여야 합니다."
		}
	} else {
		hours := p.HoursAhead
		if hours <= 0 {
			hours = calDefaultHoursAhead
		}
		if hours > calMaxHoursAhead {
			hours = calMaxHoursAhead
		}
		from, to = now, now.Add(time.Duration(hours)*time.Hour)
	}

	events, warn := calMerged(ctx, d, from, to)
	if len(events) == 0 {
		msg := fmt.Sprintf("%s ~ %s 사이에 일정이 없습니다.", calDay(from), calDay(to))
		if warn != "" {
			msg += "\n(" + warn + ")"
		}
		return msg
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "일정 %d건 (%s ~ %s):\n", len(events), calDay(from), calDay(to))
	for i, e := range events {
		sb.WriteString(calListRow(i+1, e))
	}
	if conflicts := detectConflicts(events); len(conflicts) > 0 {
		sb.WriteString("\n⚠️ 겹치는 일정:\n")
		for _, c := range conflicts {
			fmt.Fprintf(&sb, "  - %s ↔ %s\n", c[0], c[1])
		}
	}
	if warn != "" {
		sb.WriteString("\n(" + warn + ")")
	}
	sb.WriteString("\n상세·수정·삭제는 calendar(action=\"get|update|delete\", id=\"...\"). 빈 시간은 free_slots.")
	return strings.TrimRight(sb.String(), "\n")
}

// calListRow renders one compact event line: index, source-tagged ID, KST time
// range, title, and lightweight badges (location, Meet, attendee count).
func calListRow(n int, e calendar.Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d. [id=%s] %s · %s", n, e.ID, calWhen(e), calTitle(e))
	if loc := strings.TrimSpace(e.Location); loc != "" {
		fmt.Fprintf(&b, " · 📍%s", loc)
	}
	if e.Conference != nil && e.Conference.URI != "" {
		b.WriteString(" · 🎥Meet")
	}
	if n := countExternalAttendees(e.Attendees); n > 0 {
		fmt.Fprintf(&b, " · 👤%d명", n)
	}
	if localcal.IsLocalID(e.ID) {
		b.WriteString(" · (로컬)")
	}
	b.WriteString("\n")
	return b.String()
}

// --- get -----------------------------------------------------------------

// calActionGet returns rich detail for one event — the substrate for 미팅 준비:
// time, location, full description, attendees with RSVP state, and the Meet link.
func calActionGet(ctx context.Context, d *toolctx.CalendarDeps, p calParams) string {
	id := strings.TrimSpace(p.ID)
	if id == "" {
		return "id는 필수입니다 (list로 일정 ID를 먼저 확인하세요)."
	}

	var ev *calendar.Event
	if localcal.IsLocalID(id) {
		if d.Local == nil {
			return "로컬 캘린더가 없어 이 일정을 찾을 수 없습니다."
		}
		ev = d.Local.Get(id)
	} else {
		if d.Client == nil {
			return "구글 캘린더가 연결되지 않아 이 일정을 조회할 수 없습니다."
		}
		client, err := d.Client()
		if err != nil {
			return "구글 캘린더 클라이언트를 사용할 수 없습니다: " + err.Error()
		}
		ev, err = client.Get(ctx, id)
		if err != nil {
			return "일정 조회 실패: " + err.Error()
		}
	}
	if ev == nil {
		return fmt.Sprintf("ID '%s'에 해당하는 일정을 찾지 못했습니다.", id)
	}
	return calDetail(*ev)
}

// calDetail formats one event in full. Korean-first, plain text (the native
// client renders the transcript body directly).
func calDetail(e calendar.Event) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "📅 %s\n", calTitle(e))
	fmt.Fprintf(&sb, "🕒 %s\n", calWhenFull(e))
	if loc := strings.TrimSpace(e.Location); loc != "" {
		fmt.Fprintf(&sb, "📍 %s\n", loc)
	}
	if e.Conference != nil && e.Conference.URI != "" {
		fmt.Fprintf(&sb, "🎥 %s\n", e.Conference.URI)
	}
	if org := attendeeLabel(e.Organizer); org != "" {
		fmt.Fprintf(&sb, "주최: %s\n", org)
	}
	if len(e.Attendees) > 0 {
		sb.WriteString("참석자:\n")
		for _, a := range e.Attendees {
			label := attendeeLabel(a)
			if label == "" {
				continue
			}
			fmt.Fprintf(&sb, "  - %s (%s)\n", label, rsvpKorean(a.ResponseStatus))
		}
	}
	if desc := strings.TrimSpace(e.Description); desc != "" {
		fmt.Fprintf(&sb, "메모: %s\n", desc)
	}
	source := "구글 캘린더 (읽기 전용)"
	if localcal.IsLocalID(e.ID) {
		source = "로컬 (수정·삭제 가능)"
	}
	fmt.Fprintf(&sb, "출처: %s · id=%s", source, e.ID)
	if e.HTMLLink != "" {
		fmt.Fprintf(&sb, "\n링크: %s", e.HTMLLink)
	}
	return sb.String()
}

// --- create / update -----------------------------------------------------

func calActionCreate(d *toolctx.CalendarDeps, p calParams) string {
	if d.Local == nil {
		return "로컬 캘린더를 사용할 수 없어 일정을 추가할 수 없습니다."
	}
	in, errMsg := calParseInput(p)
	if errMsg != "" {
		return errMsg
	}
	ev, err := d.Local.Create(in)
	if err != nil {
		return "일정 추가 실패: " + err.Error()
	}
	return "일정을 추가했습니다.\n" + calDetail(ev)
}

func calActionUpdate(d *toolctx.CalendarDeps, p calParams) string {
	id := strings.TrimSpace(p.ID)
	if id == "" {
		return "id는 필수입니다 (수정할 일정의 ID)."
	}
	if !localcal.IsLocalID(id) {
		return "외부 캘린더(구글) 일정은 수정할 수 없습니다 (읽기 전용). 로컬 일정만 수정 가능합니다."
	}
	if d.Local == nil {
		return "로컬 캘린더를 사용할 수 없어 일정을 수정할 수 없습니다."
	}
	in, errMsg := calParseInput(p)
	if errMsg != "" {
		return errMsg
	}
	ev, err := d.Local.Update(id, in)
	if err != nil {
		if errors.Is(err, localcal.ErrNotFound) {
			return fmt.Sprintf("ID '%s'에 해당하는 로컬 일정을 찾지 못했습니다.", id)
		}
		return "일정 수정 실패: " + err.Error()
	}
	return "일정을 수정했습니다.\n" + calDetail(*ev)
}

// calParseInput validates summary+start (required) and parses start/end into a
// localcal.CreateInput, returning a Korean error message on bad input.
func calParseInput(p calParams) (in localcal.CreateInput, errMsg string) {
	if strings.TrimSpace(p.Summary) == "" {
		return localcal.CreateInput{}, "summary(제목)는 필수입니다."
	}
	if strings.TrimSpace(p.Start) == "" {
		return localcal.CreateInput{}, "start(시작 시각)는 필수입니다 (RFC3339, 예: 2026-06-10T15:00:00+09:00)."
	}
	start, err := time.Parse(time.RFC3339, strings.TrimSpace(p.Start))
	if err != nil {
		return localcal.CreateInput{}, "start는 RFC3339 형식이어야 합니다 (예: 2026-06-10T15:00:00+09:00)."
	}
	var end time.Time
	if s := strings.TrimSpace(p.End); s != "" {
		end, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return localcal.CreateInput{}, "end는 RFC3339 형식이어야 합니다 (생략하면 1시간으로 설정)."
		}
	}
	return localcal.CreateInput{
		Summary:     p.Summary,
		Description: p.Description,
		Location:    p.Location,
		Start:       start,
		End:         end,
		AllDay:      p.AllDay,
	}, ""
}

// --- delete --------------------------------------------------------------

func calActionDelete(d *toolctx.CalendarDeps, p calParams) string {
	id := strings.TrimSpace(p.ID)
	if id == "" {
		return "id는 필수입니다 (삭제할 일정의 ID)."
	}
	if !localcal.IsLocalID(id) {
		return "외부 캘린더(구글) 일정은 삭제할 수 없습니다 (읽기 전용). 로컬 일정만 삭제 가능합니다."
	}
	if d.Local == nil {
		return "로컬 캘린더를 사용할 수 없어 일정을 삭제할 수 없습니다."
	}
	if err := d.Local.Delete(id); err != nil {
		if errors.Is(err, localcal.ErrNotFound) {
			return fmt.Sprintf("ID '%s'에 해당하는 로컬 일정을 찾지 못했습니다.", id)
		}
		return "일정 삭제 실패: " + err.Error()
	}
	return "일정을 삭제했습니다."
}

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
