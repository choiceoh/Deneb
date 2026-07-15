package schedule

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

func kstTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, calDisplayLoc())
}

func TestResolveReadWindowContract(t *testing.T) {
	validFrom := "2026-07-11T09:00:00+09:00"
	validTo := "2026-07-11T18:00:00+09:00"
	tests := []struct {
		name       string
		from       string
		to         string
		hours      int
		wantHours  time.Duration
		wantErrSub string
	}{
		{name: "default hours", wantHours: calDefaultHoursAhead * time.Hour},
		{name: "negative hours default", hours: -2, wantHours: calDefaultHoursAhead * time.Hour},
		{name: "one hour", hours: 1, wantHours: time.Hour},
		{name: "max hours", hours: calMaxHoursAhead, wantHours: calMaxHoursAhead * time.Hour},
		{name: "over max clamps", hours: calMaxHoursAhead + 100, wantHours: calMaxHoursAhead * time.Hour},
		{name: "valid explicit", from: validFrom, to: validTo, hours: 1, wantHours: 9 * time.Hour},
		{name: "missing from", from: "", to: validTo, wantErrSub: "from은 RFC3339"},
		{name: "missing to", from: validFrom, to: "", wantErrSub: "to는 RFC3339"},
		{name: "bad from", from: "tomorrow", to: validTo, wantErrSub: "from은 RFC3339"},
		{name: "bad to", from: validFrom, to: "later", wantErrSub: "to는 RFC3339"},
		{name: "equal", from: validFrom, to: validFrom, wantErrSub: "to는 from보다"},
		{name: "inverted", from: validTo, to: validFrom, wantErrSub: "to는 from보다"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, errMsg := calResolveWindow(tt.from, tt.to, tt.hours)
			if tt.wantErrSub != "" {
				if !strings.Contains(errMsg, tt.wantErrSub) {
					t.Fatalf("error = %q, want %q", errMsg, tt.wantErrSub)
				}
				return
			}
			if errMsg != "" {
				t.Fatal(errMsg)
			}
			if got := to.Sub(from); got < tt.wantHours-time.Second || got > tt.wantHours+time.Second {
				t.Fatalf("window = %s, want %s", got, tt.wantHours)
			}
		})
	}

	from, to, errMsg := ResolveReadWindow(validFrom, validTo, 0)
	if errMsg != "" || from.Format(time.RFC3339) != validFrom || to.Format(time.RFC3339) != validTo {
		t.Fatalf("exported facade = %s/%s/%q", from, to, errMsg)
	}
}

func TestFreeSlotsRangeContract(t *testing.T) {
	now := kstTime(2026, time.July, 11, 10, 30)
	validFrom := "2026-07-12T09:00:00+09:00"
	validTo := "2026-07-13T18:00:00+09:00"
	tests := []struct {
		name       string
		params     calParams
		wantFrom   time.Time
		wantTo     time.Time
		wantDur    time.Duration
		wantErrSub string
	}{
		{name: "default seven days", wantFrom: now, wantTo: now.AddDate(0, 0, 7)},
		{name: "hours", params: calParams{HoursAhead: 12}, wantFrom: now, wantDur: 12 * time.Hour},
		{name: "hours clamp", params: calParams{HoursAhead: calMaxHoursAhead + 1}, wantFrom: now, wantDur: calMaxHoursAhead * time.Hour},
		{name: "explicit", params: calParams{From: validFrom, To: validTo}, wantDur: 33 * time.Hour},
		{name: "bad from", params: calParams{From: "bad", To: validTo}, wantErrSub: "from은 RFC3339"},
		{name: "bad to", params: calParams{From: validFrom, To: "bad"}, wantErrSub: "to는 RFC3339"},
		{name: "equal", params: calParams{From: validFrom, To: validFrom}, wantErrSub: "to는 from보다"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, errMsg := freeSlotsRange(tt.params, now)
			if tt.wantErrSub != "" {
				if !strings.Contains(errMsg, tt.wantErrSub) {
					t.Fatalf("error = %q", errMsg)
				}
				return
			}
			if errMsg != "" {
				t.Fatal(errMsg)
			}
			if !tt.wantFrom.IsZero() && !from.Equal(tt.wantFrom) {
				t.Fatalf("from = %s, want %s", from, tt.wantFrom)
			}
			if !tt.wantTo.IsZero() && !to.Equal(tt.wantTo) {
				t.Fatalf("to = %s, want %s", to, tt.wantTo)
			}
			if tt.wantDur > 0 && to.Sub(from) != tt.wantDur {
				t.Fatalf("duration = %s, want %s", to.Sub(from), tt.wantDur)
			}
		})
	}
}

func TestFreeSlotsHoursContract(t *testing.T) {
	tests := []struct {
		name      string
		dayStart  int
		dayEnd    int
		wantStart int
		wantEnd   int
	}{
		{name: "defaults", wantStart: 9, wantEnd: 18},
		{name: "negative defaults", dayStart: -1, dayEnd: -1, wantStart: 9, wantEnd: 18},
		{name: "custom", dayStart: 8, dayEnd: 17, wantStart: 8, wantEnd: 17},
		{name: "midnight start means unset", dayStart: 0, dayEnd: 24, wantStart: 9, wantEnd: 24},
		{name: "last hour", dayStart: 23, dayEnd: 24, wantStart: 23, wantEnd: 24},
		{name: "invalid start", dayStart: 24, dayEnd: 20, wantStart: 9, wantEnd: 20},
		{name: "invalid end defaults", dayStart: 8, dayEnd: 25, wantStart: 8, wantEnd: 18},
		{name: "end equal advances one", dayStart: 15, dayEnd: 15, wantStart: 15, wantEnd: 16},
		{name: "end before advances one", dayStart: 15, dayEnd: 10, wantStart: 15, wantEnd: 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := freeSlotsHours(calParams{DayStart: tt.dayStart, DayEnd: tt.dayEnd})
			if start != tt.wantStart || end != tt.wantEnd {
				t.Fatalf("hours = %d-%d, want %d-%d", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestFreeWithinContract(t *testing.T) {
	day := kstTime(2026, time.July, 13, 0, 0)
	at := func(hour, minute int) time.Time {
		return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
	}
	windowStart, windowEnd := at(9, 0), at(18, 0)
	tests := []struct {
		name   string
		busy   []interval
		minDur time.Duration
		want   [][2]string
	}{
		{
			name:   "empty returns whole window",
			minDur: 30 * time.Minute,
			want:   [][2]string{{"09:00", "18:00"}},
		},
		{
			name:   "single middle meeting",
			busy:   []interval{{start: at(11, 0), end: at(12, 0)}},
			minDur: 30 * time.Minute,
			want:   [][2]string{{"09:00", "11:00"}, {"12:00", "18:00"}},
		},
		{
			name: "outside intervals ignored",
			busy: []interval{
				{start: at(7, 0), end: at(8, 0)},
				{start: at(19, 0), end: at(20, 0)},
			},
			minDur: 30 * time.Minute,
			want:   [][2]string{{"09:00", "18:00"}},
		},
		{
			name:   "spanning interval consumes window",
			busy:   []interval{{start: at(7, 0), end: at(20, 0)}},
			minDur: 30 * time.Minute,
			want:   [][2]string{},
		},
		{
			name: "overlapping and touching merge",
			busy: []interval{
				{start: at(12, 0), end: at(13, 0)},
				{start: at(10, 0), end: at(11, 30)},
				{start: at(11, 0), end: at(12, 0)},
				{start: at(13, 0), end: at(14, 0)},
			},
			minDur: 30 * time.Minute,
			want:   [][2]string{{"09:00", "10:00"}, {"14:00", "18:00"}},
		},
		{
			name: "short gaps filtered",
			busy: []interval{
				{start: at(9, 20), end: at(10, 0)},
				{start: at(10, 20), end: at(17, 45)},
			},
			minDur: 30 * time.Minute,
			want:   [][2]string{},
		},
		{
			name: "exact duration retained",
			busy: []interval{
				{start: at(9, 30), end: at(17, 30)},
			},
			minDur: 30 * time.Minute,
			want:   [][2]string{{"09:00", "09:30"}, {"17:30", "18:00"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := freeWithin(windowStart, windowEnd, tt.busy, tt.minDur)
			formatted := make([][2]string, len(got))
			for i, interval := range got {
				formatted[i] = [2]string{interval.start.Format("15:04"), interval.end.Format("15:04")}
			}
			if !reflect.DeepEqual(formatted, tt.want) {
				t.Fatalf("got %v, want %v", formatted, tt.want)
			}
		})
	}
}

func TestConflictDetectionContract(t *testing.T) {
	day := kstTime(2026, time.July, 13, 0, 0)
	event := func(title string, startHour, startMinute, endHour, endMinute int) tooldeps.CalendarEvent {
		return tooldeps.CalendarEvent{
			Summary: title,
			Start:   day.Add(time.Duration(startHour)*time.Hour + time.Duration(startMinute)*time.Minute),
			End:     day.Add(time.Duration(endHour)*time.Hour + time.Duration(endMinute)*time.Minute),
		}
	}
	tests := []struct {
		name   string
		events []tooldeps.CalendarEvent
		want   [][2]string
	}{
		{name: "empty", events: nil, want: nil},
		{name: "touching is not overlap", events: []tooldeps.CalendarEvent{event("A", 9, 0, 10, 0), event("B", 10, 0, 11, 0)}, want: nil},
		{name: "one overlap", events: []tooldeps.CalendarEvent{event("A", 9, 0, 10, 30), event("B", 10, 0, 11, 0)}, want: [][2]string{{"A", "B"}}},
		{name: "nested overlaps", events: []tooldeps.CalendarEvent{event("A", 9, 0, 12, 0), event("B", 9, 30, 10, 0), event("C", 11, 0, 13, 0)}, want: [][2]string{{"A", "B"}, {"A", "C"}}},
		{name: "all day ignored", events: []tooldeps.CalendarEvent{{Summary: "All", AllDay: true, Start: day}, event("B", 9, 0, 10, 0)}, want: nil},
		{name: "zero start ignored", events: []tooldeps.CalendarEvent{{Summary: "Zero"}, event("B", 9, 0, 10, 0)}, want: nil},
		{name: "missing end defaults one hour", events: []tooldeps.CalendarEvent{{Summary: "A", Start: day.Add(9 * time.Hour)}, event("B", 9, 30, 10, 0)}, want: [][2]string{{"A", "B"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectConflicts(tt.events); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalendarFormattingContract(t *testing.T) {
	start := kstTime(2026, time.July, 13, 9, 5)
	end := kstTime(2026, time.July, 13, 10, 45)
	nextDay := kstTime(2026, time.July, 14, 1, 0)
	if got := calTitle(tooldeps.CalendarEvent{}); got != "(제목 없음)" {
		t.Fatalf("empty title = %q", got)
	}
	if got := calTitle(tooldeps.CalendarEvent{Summary: "  Review  "}); got != "Review" {
		t.Fatalf("trimmed title = %q", got)
	}
	if got := calWhen(tooldeps.CalendarEvent{Start: start, End: end}); got != "7/13(월) 09:05–10:45" {
		t.Fatalf("same day compact = %q", got)
	}
	if got := calWhen(tooldeps.CalendarEvent{Start: start, End: nextDay}); got != "7/13(월) 09:05" {
		t.Fatalf("cross day compact = %q", got)
	}
	if got := calWhen(tooldeps.CalendarEvent{Start: start, AllDay: true}); got != "7/13(월) 종일" {
		t.Fatalf("all day compact = %q", got)
	}
	if got := calWhenFull(tooldeps.CalendarEvent{Start: start, End: end}); got != "7/13(월) 09:05 – 10:45" {
		t.Fatalf("same day full = %q", got)
	}
	if got := calWhenFull(tooldeps.CalendarEvent{Start: start, End: nextDay}); got != "7/13(월) 09:05 – 7/14(화) 01:00" {
		t.Fatalf("cross day full = %q", got)
	}
	if got := calWhenFull(tooldeps.CalendarEvent{Start: start, AllDay: true}); got != "7/13(월) (종일)" {
		t.Fatalf("all day full = %q", got)
	}
	if !sameDay(start, end) || sameDay(start, nextDay) {
		t.Fatal("sameDay contract failed")
	}
}

func TestCalendarLabelContract(t *testing.T) {
	weekdays := []struct {
		day  time.Weekday
		want string
	}{
		{day: time.Sunday, want: "일"},
		{day: time.Monday, want: "월"},
		{day: time.Tuesday, want: "화"},
		{day: time.Wednesday, want: "수"},
		{day: time.Thursday, want: "목"},
		{day: time.Friday, want: "금"},
		{day: time.Saturday, want: "토"},
	}
	for _, tt := range weekdays {
		if got := weekdayKorean(tt.day); got != tt.want {
			t.Errorf("weekday %s = %q", tt.day, got)
		}
	}
	rsvps := map[string]string{
		"accepted":    "수락",
		"declined":    "거절",
		"tentative":   "미정",
		"needsAction": "대기",
		"":            "응답 없음",
		"unknown":     "응답 없음",
	}
	for in, want := range rsvps {
		if got := rsvpKorean(in); got != want {
			t.Errorf("rsvp %q = %q, want %q", in, got, want)
		}
	}
	if got := attendeeLabel(tooldeps.CalendarAttendee{DisplayName: "  Jane  ", Email: "jane@example.com"}); got != "Jane" {
		t.Fatalf("display label = %q", got)
	}
	if got := attendeeLabel(tooldeps.CalendarAttendee{Email: " jane@example.com "}); got != "jane@example.com" {
		t.Fatalf("email label = %q", got)
	}
	if got := attendeeLabel(tooldeps.CalendarAttendee{}); got != "" {
		t.Fatalf("empty label = %q", got)
	}
	for in, want := range map[string]string{"meeting": "미팅", "deadline": "기한", " meeting ": "미팅", "other": "", "": ""} {
		if got := calKindLabel(in); got != want {
			t.Errorf("kind %q = %q, want %q", in, got, want)
		}
	}
}

func TestSourceAnnotationContract(t *testing.T) {
	tests := []struct {
		name      string
		event     tooldeps.CalendarEvent
		wantLine  string
		wantBadge string
	}{
		{name: "none", event: tooldeps.CalendarEvent{}, wantLine: "", wantBadge: ""},
		{name: "kind only", event: tooldeps.CalendarEvent{Kind: "meeting"}, wantLine: "연결: 미팅", wantBadge: " · [미팅]"},
		{name: "label only", event: tooldeps.CalendarEvent{SourceLabel: "Proposal"}, wantLine: "연결: 메일 「Proposal」", wantBadge: " · 「Proposal」"},
		{name: "source only", event: tooldeps.CalendarEvent{Source: " mail:id "}, wantLine: "연결: mail:id", wantBadge: ""},
		{name: "kind label", event: tooldeps.CalendarEvent{Kind: "deadline", SourceLabel: "Due date"}, wantLine: "연결: 기한 · 메일 「Due date」", wantBadge: " · [기한] 「Due date」"},
		{name: "all", event: tooldeps.CalendarEvent{Kind: "meeting", SourceLabel: "Invite", Source: "mail:42"}, wantLine: "연결: 미팅 · 메일 「Invite」 · mail:42", wantBadge: " · [미팅] 「Invite」"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calSourceLine(tt.event); got != tt.wantLine {
				t.Errorf("line = %q, want %q", got, tt.wantLine)
			}
			if got := calLinkBadge(tt.event); got != tt.wantBadge {
				t.Errorf("badge = %q, want %q", got, tt.wantBadge)
			}
		})
	}
}

func TestExternalAttendeeCountContract(t *testing.T) {
	attendees := []tooldeps.CalendarAttendee{
		{DisplayName: "Self", Self: true, ResponseStatus: "accepted"},
		{DisplayName: "Declined", ResponseStatus: "declined"},
		{DisplayName: "Accepted", ResponseStatus: "accepted"},
		{Email: "waiting@example.com", ResponseStatus: "needsAction"},
		{DisplayName: "Tentative", ResponseStatus: "tentative"},
		{},
	}
	if got := countExternalAttendees(attendees); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if got := countExternalAttendees(nil); got != 0 {
		t.Fatalf("nil count = %d", got)
	}
}

func TestCalendarDetailDoesNotRenderEmptyAttendeeSection(t *testing.T) {
	e := tooldeps.CalendarEvent{
		ID:      "google-id",
		Summary: "Review",
		Start:   kstTime(2026, time.July, 13, 9, 0),
		End:     kstTime(2026, time.July, 13, 10, 0),
		Attendees: []tooldeps.CalendarAttendee{
			{},
			{DisplayName: "   ", Email: "   "},
		},
	}
	got := calDetail(e)
	if strings.Contains(got, "참석자:") {
		t.Fatalf("empty attendee header leaked:\n%s", got)
	}
	e.Attendees = append(e.Attendees, tooldeps.CalendarAttendee{DisplayName: "Jane", ResponseStatus: "accepted"})
	got = calDetail(e)
	if !strings.Contains(got, "참석자:\n  - Jane (수락)") {
		t.Fatalf("real attendee missing:\n%s", got)
	}
}

func TestCalendarDetailRichFieldsContract(t *testing.T) {
	e := tooldeps.CalendarEvent{
		ID:          "google-id",
		Summary:     "  Executive Review  ",
		Description: "  Bring figures  ",
		Location:    "  Board Room  ",
		Start:       kstTime(2026, time.July, 13, 9, 0),
		End:         kstTime(2026, time.July, 13, 10, 0),
		HTMLLink:    "https://calendar.example/event",
		Organizer:   tooldeps.CalendarAttendee{DisplayName: "Owner"},
		Attendees: []tooldeps.CalendarAttendee{
			{DisplayName: "Jane", ResponseStatus: "accepted"},
			{Email: "john@example.com", ResponseStatus: "tentative"},
		},
		Conference:  &tooldeps.CalendarConference{URI: "https://meet.example/abc"},
		Kind:        "meeting",
		Source:      "mail:42",
		SourceLabel: "Invite",
		Docs:        []string{"proposal.pdf", "budget.xlsx"},
	}
	got := calDetail(e)
	for _, want := range []string{
		"📅 Executive Review",
		"🕒 7/13(월) 09:00 – 10:00",
		"📍 Board Room",
		"🎥 https://meet.example/abc",
		"주최: Owner",
		"Jane (수락)",
		"john@example.com (미정)",
		"메모: Bring figures",
		"연결: 미팅 · 메일 「Invite」 · mail:42",
		"📎 관련 문서: proposal.pdf, budget.xlsx",
		"출처: 구글 캘린더 (읽기 전용) · id=google-id",
		"링크: https://calendar.example/event",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("detail missing %q:\n%s", want, got)
		}
	}
}

func TestCalendarListRowContract(t *testing.T) {
	e := tooldeps.CalendarEvent{
		ID:       "google-id",
		Summary:  "Review",
		Location: "Room",
		Start:    kstTime(2026, time.July, 13, 9, 0),
		End:      kstTime(2026, time.July, 13, 10, 0),
		Conference: &tooldeps.CalendarConference{
			URI: "https://meet.example",
		},
		Attendees: []tooldeps.CalendarAttendee{
			{DisplayName: "A", ResponseStatus: "accepted"},
			{DisplayName: "B", ResponseStatus: "declined"},
		},
		Kind:        "deadline",
		SourceLabel: "Contract",
	}
	got := calListRow(3, e)
	for _, want := range []string{
		"3. [id=google-id]",
		"7/13(월) 09:00–10:00",
		"Review",
		"📍Room",
		"🎥Meet",
		"👤1명",
		"[기한] 「Contract」",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("row missing %q: %s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("row lacks newline: %q", got)
	}
}

func TestCalendarInputParsingContract(t *testing.T) {
	validStart := "2026-07-13T09:00:00+09:00"
	validEnd := "2026-07-13T10:30:00+09:00"
	tests := []struct {
		name        string
		params      calParams
		wantErrSub  string
		wantEndZero bool
	}{
		{name: "missing summary", params: calParams{Start: validStart}, wantErrSub: "summary"},
		{name: "blank summary", params: calParams{Summary: "  ", Start: validStart}, wantErrSub: "summary"},
		{name: "missing start", params: calParams{Summary: "Title"}, wantErrSub: "start"},
		{name: "bad start", params: calParams{Summary: "Title", Start: "tomorrow"}, wantErrSub: "start는 RFC3339"},
		{name: "bad end", params: calParams{Summary: "Title", Start: validStart, End: "later"}, wantErrSub: "end는 RFC3339"},
		{name: "valid no end", params: calParams{Summary: " Title ", Start: validStart}, wantEndZero: true},
		{name: "valid full", params: calParams{Summary: "Title", Start: validStart, End: validEnd, Description: "Desc", Location: "Room", AllDay: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errMsg := calParseInput(tt.params)
			if tt.wantErrSub != "" {
				if !strings.Contains(errMsg, tt.wantErrSub) {
					t.Fatalf("error = %q", errMsg)
				}
				return
			}
			if errMsg != "" {
				t.Fatal(errMsg)
			}
			if got.Start.IsZero() {
				t.Fatal("start was not parsed")
			}
			if tt.wantEndZero && !got.End.IsZero() {
				t.Fatalf("end = %s, want zero", got.End)
			}
			if got.Summary != tt.params.Summary || got.Description != tt.params.Description || got.Location != tt.params.Location || got.AllDay != tt.params.AllDay {
				t.Fatalf("input fields = %+v, want %+v", got, tt.params)
			}
		})
	}
}

func TestTimelineRangeContract(t *testing.T) {
	validFrom := "2026-07-01T00:00:00+09:00"
	validTo := "2026-07-31T00:00:00+09:00"
	tests := []struct {
		name    string
		p       calParams
		wantErr string
	}{
		{name: "missing both", p: calParams{}, wantErr: "함께 지정"},
		{name: "missing from", p: calParams{To: validTo}, wantErr: "함께 지정"},
		{name: "missing to", p: calParams{From: validFrom}, wantErr: "함께 지정"},
		{name: "bad from", p: calParams{From: "bad", To: validTo}, wantErr: "from은 RFC3339"},
		{name: "bad to", p: calParams{From: validFrom, To: "bad"}, wantErr: "to는 RFC3339"},
		{name: "equal", p: calParams{From: validFrom, To: validFrom}, wantErr: "to는 from보다"},
		{name: "inverted", p: calParams{From: validTo, To: validFrom}, wantErr: "to는 from보다"},
		{name: "valid", p: calParams{From: validFrom, To: validTo}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, errMsg := parseTimelineRange(tt.p)
			if tt.wantErr != "" {
				if !strings.Contains(errMsg, tt.wantErr) {
					t.Fatalf("error = %q", errMsg)
				}
				return
			}
			if errMsg != "" || !to.After(from) {
				t.Fatalf("range = %s/%s/%q", from, to, errMsg)
			}
		})
	}
}

func TestEventMatchesEntityContract(t *testing.T) {
	base := tooldeps.CalendarEvent{
		Summary:     "Alpha Review",
		SourceLabel: "Beta Invite",
		Location:    "Gamma Room",
		Description: "Delta project notes",
		Organizer:   tooldeps.CalendarAttendee{DisplayName: "Epsilon Owner", Email: "owner@epsilon.example"},
		Attendees: []tooldeps.CalendarAttendee{
			{DisplayName: "Zeta Person", Email: "zeta@example.com"},
		},
	}
	for _, query := range []string{"alpha", "beta", "gamma", "delta", "epsilon owner", "epsilon.example", "zeta person", "example.com"} {
		if !eventMatchesEntity(base, query) {
			t.Errorf("query %q did not match", query)
		}
	}
	for _, query := range []string{"omega", "unrelated", "not-present"} {
		if eventMatchesEntity(base, query) {
			t.Errorf("query %q unexpectedly matched", query)
		}
	}
}

func TestEventEndAndTimedEventsContract(t *testing.T) {
	day := kstTime(2026, time.July, 13, 0, 0)
	e := tooldeps.CalendarEvent{Start: day.Add(9 * time.Hour), End: day.Add(10 * time.Hour)}
	if got := eventEnd(e); !got.Equal(e.End) {
		t.Fatalf("valid end = %s", got)
	}
	e.End = time.Time{}
	if got := eventEnd(e); !got.Equal(e.Start.Add(time.Hour)) {
		t.Fatalf("missing end = %s", got)
	}
	e.End = e.Start.Add(-time.Minute)
	if got := eventEnd(e); !got.Equal(e.Start.Add(time.Hour)) {
		t.Fatalf("invalid end = %s", got)
	}

	events := []tooldeps.CalendarEvent{
		{Summary: "run-in", Start: day.Add(-time.Hour), End: day.Add(time.Hour)},
		{Summary: "inside", Start: day.Add(9 * time.Hour), End: day.Add(10 * time.Hour)},
		{Summary: "run-out", Start: day.Add(23 * time.Hour), End: day.Add(25 * time.Hour)},
		{Summary: "before", Start: day.Add(-2 * time.Hour), End: day.Add(-time.Hour)},
		{Summary: "after", Start: day.Add(25 * time.Hour), End: day.Add(26 * time.Hour)},
		{Summary: "all-day", Start: day, End: day.Add(24 * time.Hour), AllDay: true},
		{Summary: "zero"},
	}
	got := timedEventsOn(events, day, calDisplayLoc())
	want := []string{"run-in", "inside", "run-out"}
	names := make([]string, len(got))
	for i := range got {
		names[i] = got[i].Summary
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("events = %v, want %v", names, want)
	}
}

func TestLongestBackToBackContract(t *testing.T) {
	day := kstTime(2026, time.July, 13, 0, 0)
	event := func(startMin, durationMin int) tooldeps.CalendarEvent {
		start := day.Add(time.Duration(startMin) * time.Minute)
		return tooldeps.CalendarEvent{Start: start, End: start.Add(time.Duration(durationMin) * time.Minute)}
	}
	tests := []struct {
		name   string
		events []tooldeps.CalendarEvent
		want   int
	}{
		{name: "empty", want: 0},
		{name: "one", events: []tooldeps.CalendarEvent{event(540, 60)}, want: 1},
		{name: "exactly touching three", events: []tooldeps.CalendarEvent{event(540, 60), event(600, 60), event(660, 60)}, want: 3},
		{name: "nine minute gaps count", events: []tooldeps.CalendarEvent{event(540, 60), event(609, 60), event(678, 60)}, want: 3},
		{name: "ten minute gap breaks", events: []tooldeps.CalendarEvent{event(540, 60), event(610, 60), event(680, 60)}, want: 1},
		{name: "longest later run", events: []tooldeps.CalendarEvent{event(540, 60), event(610, 60), event(720, 60), event(780, 60), event(840, 60)}, want: 3},
		{name: "overlap counts no buffer", events: []tooldeps.CalendarEvent{event(540, 120), event(600, 60)}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := longestBackToBack(tt.events); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestShortDurationContract(t *testing.T) {
	for _, tt := range []struct {
		duration time.Duration
		want     string
	}{
		{duration: 0, want: "0분"},
		{duration: 30 * time.Minute, want: "30분"},
		{duration: 59 * time.Minute, want: "59분"},
		{duration: time.Hour, want: "1시간"},
		{duration: 90 * time.Minute, want: "1.5시간"},
		{duration: 5 * time.Hour, want: "5시간"},
	} {
		if got := shortDur(tt.duration); got != tt.want {
			t.Errorf("shortDur(%s) = %q, want %q", tt.duration, got, tt.want)
		}
	}
}

func TestToolCalendarInputAndDispatchContract(t *testing.T) {
	tool := ToolCalendar(&tooldeps.CalendarDeps{})
	if _, err := tool(context.Background(), json.RawMessage(`{"action":`)); err == nil || !strings.Contains(err.Error(), "parse input") {
		t.Fatalf("malformed error = %v", err)
	}
	got, err := tool(context.Background(), json.RawMessage(`{"action":"unknown"}`))
	if err != nil || !strings.Contains(got, "알 수 없는 액션") || !strings.Contains(got, "free_slots") {
		t.Fatalf("unknown action = %q/%v", got, err)
	}
	got, err = tool(context.Background(), json.RawMessage(`{"action":"get"}`))
	if err != nil || !strings.Contains(got, "id는 필수") {
		t.Fatalf("get missing id = %q/%v", got, err)
	}
	got, err = tool(context.Background(), json.RawMessage(`{"action":"timeline"}`))
	if err != nil || !strings.Contains(got, "query") {
		t.Fatalf("timeline missing query = %q/%v", got, err)
	}
}

func TestCalendarGlanceNilAndEmptyContract(t *testing.T) {
	now := kstTime(2026, time.July, 13, 9, 0)
	if got := CalendarGlance(context.Background(), nil, now, 3); got != "" {
		t.Fatalf("nil deps = %q", got)
	}
	if got := CalendarGlance(context.Background(), &tooldeps.CalendarDeps{}, now, 3); got != "" {
		t.Fatalf("empty deps = %q", got)
	}
}
