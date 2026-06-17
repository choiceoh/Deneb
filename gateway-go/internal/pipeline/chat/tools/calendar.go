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
//
// This file holds the action dispatch and the list/get/create/update/delete
// actions. Shared merge/format helpers and CalendarGlance live in
// calendar_format.go; conflict detection and free_slots in calendar_slots.go.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		case "brief":
			return calActionBrief(ctx, d, p), nil
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: list(일정 조회), get(상세), create(추가), update(수정), delete(삭제), free_slots(빈 시간 찾기), brief(브리핑)", p.Action), nil
		}
	}
}

// --- list ----------------------------------------------------------------

func calActionList(ctx context.Context, d *toolctx.CalendarDeps, p calParams) string {
	from, to, errMsg := calResolveWindow(p.From, p.To, p.HoursAhead)
	if errMsg != "" {
		return errMsg
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

// --- brief ---------------------------------------------------------------

// calActionBrief renders a briefing skeleton for the agent: the window's events
// (default: the rest of today) with their kind + linked origin shown inline and
// any conflicts flagged, then a directive to pull the linked-mail context and
// synthesize a human-readable brief. The link annotations come from the event
// provenance (Source/SourceLabel/Kind) — so a meeting carries which mail it came
// from, and the brief can say *why* it matters, not just *when*.
func calActionBrief(ctx context.Context, d *toolctx.CalendarDeps, p calParams) string {
	from, to := calBriefWindow(p)
	events, warn := calMerged(ctx, d, from, to)
	if len(events) == 0 {
		msg := fmt.Sprintf("%s ~ %s 브리핑할 일정이 없습니다.", calDay(from), calDay(to))
		if warn != "" {
			msg += "\n(" + warn + ")"
		}
		return msg
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 %s ~ %s 일정 %d건:\n", calDay(from), calDay(to), len(events))
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
	sb.WriteString("\n\n이걸 사람이 바로 읽을 브리핑으로 정리해. 연결된 일정([미팅]·「메일 제목」·mail:<id>)은 mail_archive로 원본 맥락을 확인해 왜 지금 중요한지·무엇을 준비해야 하는지 한 줄씩 덧붙이고, 겹치는 일정이 있으면 먼저 경고해. 연결·맥락 없는 일정은 시간·제목만 간단히.")
	return strings.TrimRight(sb.String(), "\n")
}

// calBriefWindow resolves the briefing window: an explicit from/to or hours_ahead
// wins (e.g. "이번 주"); otherwise it defaults to the rest of today (now → next
// local midnight), the natural "오늘 브리핑" scope.
func calBriefWindow(p calParams) (from, to time.Time) {
	if strings.TrimSpace(p.From) != "" || strings.TrimSpace(p.To) != "" || p.HoursAhead > 0 {
		if f, t, errMsg := calResolveWindow(p.From, p.To, p.HoursAhead); errMsg == "" {
			return f, t
		}
	}
	loc := calDisplayLoc()
	now := time.Now().In(loc)
	endToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Add(24 * time.Hour)
	return now, endToday
}

// calResolveWindow resolves the [from, to) read window shared by the text list
// action and the structured (code_action as_json) path: an explicit RFC3339
// [from, to) takes priority, otherwise "now + hoursAhead" (clamped). A non-empty
// errMsg is a user-facing Korean validation message.
func calResolveWindow(fromStr, toStr string, hoursAhead int) (from, to time.Time, errMsg string) {
	if strings.TrimSpace(fromStr) != "" || strings.TrimSpace(toStr) != "" {
		var err error
		from, err = time.Parse(time.RFC3339, strings.TrimSpace(fromStr))
		if err != nil {
			return from, to, "from은 RFC3339 형식이어야 합니다 (예: 2026-06-10T00:00:00+09:00)."
		}
		to, err = time.Parse(time.RFC3339, strings.TrimSpace(toStr))
		if err != nil {
			return from, to, "to는 RFC3339 형식이어야 합니다 (예: 2026-06-17T00:00:00+09:00)."
		}
		if !to.After(from) {
			return from, to, "to는 from보다 뒤여야 합니다."
		}
		return from, to, ""
	}
	hours := hoursAhead
	if hours <= 0 {
		hours = calDefaultHoursAhead
	}
	if hours > calMaxHoursAhead {
		hours = calMaxHoursAhead
	}
	now := time.Now()
	return now, now.Add(time.Duration(hours) * time.Hour), ""
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
	b.WriteString(calLinkBadge(e))
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
	if link := calSourceLine(e); link != "" {
		sb.WriteString(link + "\n")
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
